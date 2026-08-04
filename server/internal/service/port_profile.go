package service

import (
	"errors"
	"fmt"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/runtimeconfig"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const defaultPortProfileGroupName = "PoolNeXt"

type PortProfilePayload struct {
	Name             string                         `json:"name"`
	ListenHost       string                         `json:"listen_host"`
	MixedPort        int                            `json:"mixed_port"`
	SocksPort        int                            `json:"socks_port"`
	HTTPPort         int                            `json:"http_port"`
	ProxySettings    model.PortProfileProxySettings `json:"proxy_settings"`
	IncludeInRuntime bool                           `json:"include_in_runtime"`
	NodeIDs          []int                          `json:"node_ids"`
}

type PortProfilePreview struct {
	Profile    model.PortProfile  `json:"profile"`
	NodeIDs    []int              `json:"node_ids"`
	Nodes      []*model.ProxyNode `json:"nodes"`
	KernelType string             `json:"kernel_type"`
	Checksum   string             `json:"checksum"`
	Content    string             `json:"content"`
}

func ListPortProfiles() ([]*model.PortProfileWithNodes, error) {
	profiles, err := model.ListPortProfiles()
	if err != nil {
		return nil, err
	}

	result := make([]*model.PortProfileWithNodes, 0, len(profiles))
	for _, profile := range profiles {
		item, err := buildPortProfileView(profile)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func GetPortProfile(id int) (*model.PortProfileWithNodes, error) {
	profile, err := model.GetPortProfileByID(id)
	if err != nil {
		return nil, fmt.Errorf("端口配置不存在")
	}
	return buildPortProfileView(profile)
}

func CreatePortProfile(payload PortProfilePayload) (*model.PortProfileWithNodes, error) {
	normalized, err := normalizePortProfilePayload(payload)
	if err != nil {
		return nil, err
	}
	if err := validatePortProfileUniqueness(normalized, 0); err != nil {
		return nil, err
	}

	profile := &model.PortProfile{
		Name:              normalized.Name,
		ListenHost:        normalized.ListenHost,
		MixedPort:         normalized.MixedPort,
		SocksPort:         normalized.SocksPort,
		HTTPPort:          normalized.HTTPPort,
		ProxySettingsJSON: mustEncodePortProfileProxySettings(normalized.ProxySettings),
		ProxySettings:     normalized.ProxySettings,
		IncludeInRuntime:  normalized.IncludeInRuntime,
		KernelType:        common.KernelType,
	}

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(profile).Error; err != nil {
			return err
		}
		return replacePortProfileNodes(tx, profile.ID, normalized.NodeIDs)
	}); err != nil {
		return nil, err
	}

	return GetPortProfile(profile.ID)
}

func UpdatePortProfile(id int, payload PortProfilePayload) (*model.PortProfileWithNodes, error) {
	normalized, err := normalizePortProfilePayload(payload)
	if err != nil {
		return nil, err
	}
	if err := validatePortProfileUniqueness(normalized, id); err != nil {
		return nil, err
	}
	profile, err := model.GetPortProfileByID(id)
	if err != nil {
		return nil, fmt.Errorf("端口配置不存在")
	}
	if profile.IncludeInRuntime && !normalized.IncludeInRuntime {
		var routingReferenceCount int64
		if err := model.DB.Model(&model.RoutingRule{}).Where("target_profile_id = ?", id).Count(&routingReferenceCount).Error; err != nil {
			return nil, err
		}
		if routingReferenceCount > 0 {
			return nil, fmt.Errorf("该策略组仍被 %d 条分流规则引用，请先调整规则再关闭随运行加载", routingReferenceCount)
		}
	}

	profile.Name = normalized.Name
	profile.ListenHost = normalized.ListenHost
	profile.MixedPort = normalized.MixedPort
	profile.SocksPort = normalized.SocksPort
	profile.HTTPPort = normalized.HTTPPort
	profile.ProxySettingsJSON = mustEncodePortProfileProxySettings(normalized.ProxySettings)
	profile.ProxySettings = normalized.ProxySettings
	profile.IncludeInRuntime = normalized.IncludeInRuntime
	profile.KernelType = common.KernelType

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(profile).Error; err != nil {
			return err
		}
		if err := replacePortProfileNodes(tx, profile.ID, normalized.NodeIDs); err != nil {
			return err
		}
		return tx.Where("port_profile_id = ?", profile.ID).Delete(&model.RuntimeConfig{}).Error
	}); err != nil {
		return nil, err
	}

	return GetPortProfile(profile.ID)
}

func DeletePortProfile(id int) error {
	if id <= 0 {
		return fmt.Errorf("无效的端口配置 ID")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var routingReferenceCount int64
		if err := tx.Model(&model.RoutingRule{}).Where("target_profile_id = ?", id).Count(&routingReferenceCount).Error; err != nil {
			return err
		}
		if routingReferenceCount > 0 {
			return fmt.Errorf("该策略组仍被 %d 条分流规则引用，请先调整规则", routingReferenceCount)
		}
		if err := tx.Where("port_profile_id = ?", id).Delete(&model.PortProfileNode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("port_profile_id = ?", id).Delete(&model.RuntimeConfig{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.PortProfile{}, "id = ?", id).Error
	})
}

func PreviewPortProfile(payload PortProfilePayload) (*PortProfilePreview, error) {
	normalized, err := normalizePortProfilePayload(payload)
	if err != nil {
		return nil, err
	}
	nodes, err := resolveSelectedNodes(normalized.NodeIDs)
	if err != nil {
		return nil, err
	}
	runtimeNodes := selectRuntimeNodes(nodes)

	profile := model.PortProfile{
		Name:              normalized.Name,
		ListenHost:        normalized.ListenHost,
		MixedPort:         normalized.MixedPort,
		SocksPort:         normalized.SocksPort,
		HTTPPort:          normalized.HTTPPort,
		ProxySettingsJSON: mustEncodePortProfileProxySettings(normalized.ProxySettings),
		ProxySettings:     normalized.ProxySettings,
		IncludeInRuntime:  normalized.IncludeInRuntime,
		KernelType:        common.KernelType,
	}

	rendered, err := runtimeconfig.RenderMihomoConfig(runtimeconfig.MihomoRenderInput{
		Profile: profile,
		Nodes:   runtimeNodes,
	})
	if err != nil {
		return nil, err
	}

	runtimeNodeIDs := make([]int, 0, len(runtimeNodes))
	for _, node := range runtimeNodes {
		runtimeNodeIDs = append(runtimeNodeIDs, node.ID)
	}

	return &PortProfilePreview{
		Profile:    profile,
		NodeIDs:    runtimeNodeIDs,
		Nodes:      runtimeNodes,
		KernelType: rendered.KernelType,
		Checksum:   rendered.Checksum,
		Content:    rendered.Content,
	}, nil
}

func PreviewSavedPortProfile(id int) (*PortProfilePreview, error) {
	view, err := GetPortProfile(id)
	if err != nil {
		return nil, err
	}
	return PreviewPortProfile(PortProfilePayload{
		Name:             view.Profile.Name,
		ListenHost:       view.Profile.ListenHost,
		MixedPort:        view.Profile.MixedPort,
		SocksPort:        view.Profile.SocksPort,
		HTTPPort:         view.Profile.HTTPPort,
		ProxySettings:    view.Profile.ProxySettings,
		IncludeInRuntime: view.Profile.IncludeInRuntime,
		NodeIDs:          view.NodeIDs,
	})
}

func SaveRuntimePreview(id int) (*model.RuntimeConfig, error) {
	preview, err := PreviewSavedPortProfile(id)
	if err != nil {
		return nil, err
	}

	runtimeConfig := &model.RuntimeConfig{
		PortProfileID:  id,
		KernelType:     preview.KernelType,
		Checksum:       preview.Checksum,
		RenderedConfig: preview.Content,
	}
	if err := model.DB.Where("port_profile_id = ?", id).
		Assign(runtimeConfig).
		FirstOrCreate(runtimeConfig).Error; err != nil {
		return nil, err
	}
	return runtimeConfig, nil
}

func ListProxyNodeOptions(keyword string) ([]*model.ProxyNode, error) {
	filter := model.ProxyNodeListFilter{
		Keyword: strings.TrimSpace(keyword),
	}
	items, err := model.ListProxyNodes(0, 1000, filter)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(left int, right int) bool {
		return strings.ToLower(items[left].Name) < strings.ToLower(items[right].Name)
	})
	return items, nil
}

func buildPortProfileView(profile *model.PortProfile) (*model.PortProfileWithNodes, error) {
	if err := profile.HydrateProxySettings(); err != nil {
		return nil, fmt.Errorf("解析端口配置代理设置失败: %v", err)
	}
	relations, err := model.ListPortProfileNodes(profile.ID)
	if err != nil {
		return nil, err
	}
	nodeIDs := make([]int, 0, len(relations))
	for _, item := range relations {
		nodeIDs = append(nodeIDs, item.ProxyNodeID)
	}

	nodes, err := model.FindProxyNodesByIDs(nodeIDs)
	if err != nil {
		return nil, err
	}
	nodes = orderProxyNodesByIDs(nodeIDs, nodes)

	var runtimeConfig *model.RuntimeConfig
	if existing, runtimeErr := model.GetRuntimeConfigByPortProfileID(profile.ID); runtimeErr == nil {
		runtimeConfig = existing
	} else if !errors.Is(runtimeErr, gorm.ErrRecordNotFound) {
		return nil, runtimeErr
	}

	return &model.PortProfileWithNodes{
		Profile: *profile,
		NodeIDs: nodeIDs,
		Nodes:   nodes,
		Runtime: runtimeConfig,
	}, nil
}

func normalizePortProfilePayload(payload PortProfilePayload) (*PortProfilePayload, error) {
	groupName := fallbackPortProfileString(strings.TrimSpace(payload.Name), defaultPortProfileGroupName)
	if groupName == "" {
		return nil, fmt.Errorf("策略组名称不能为空")
	}
	if payload.MixedPort < 0 || payload.SocksPort < 0 || payload.HTTPPort < 0 {
		return nil, fmt.Errorf("端口不能为负数")
	}
	if len(payload.NodeIDs) == 0 {
		return nil, fmt.Errorf("请先选择至少一个节点")
	}

	proxySettings := normalizePortProfileProxySettings(payload.ProxySettings)
	expectedStatus, err := normalizeExpectedStatus(proxySettings.ExpectedStatus)
	if err != nil {
		return nil, fmt.Errorf("期望状态码无效: %v", err)
	}
	proxySettings.ExpectedStatus = expectedStatus
	if proxySettings.AuthEnabled && (proxySettings.AuthUsername == "" || proxySettings.AuthPassword == "") {
		return nil, fmt.Errorf("开启鉴权后，用户名和密码不能为空")
	}

	mixedPort := payload.MixedPort
	socksPort := payload.SocksPort
	httpPort := payload.HTTPPort
	if mixedPort > 0 {
		socksPort = 0
		httpPort = 0
	} else if socksPort <= 0 && httpPort <= 0 {
		return nil, fmt.Errorf("关闭 Mixed 后，至少需要填写一个 SOCKS 或 HTTP 端口")
	}

	normalized := &PortProfilePayload{
		Name:             groupName,
		ListenHost:       fallbackPortProfileString(strings.TrimSpace(payload.ListenHost), "127.0.0.1"),
		MixedPort:        mixedPort,
		SocksPort:        socksPort,
		HTTPPort:         httpPort,
		ProxySettings:    proxySettings,
		IncludeInRuntime: payload.IncludeInRuntime,
		NodeIDs:          deduplicateNodeIDs(payload.NodeIDs),
	}
	if len(normalized.NodeIDs) == 0 {
		return nil, fmt.Errorf("请先选择至少一个节点")
	}
	if err := validatePortProfilePorts(normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func validatePortProfileUniqueness(payload *PortProfilePayload, currentID int) error {
	var groupNameCount int64
	groupNameQuery := model.DB.Model(&model.PortProfile{}).
		Where("lower(name) = ?", strings.ToLower(payload.Name))
	if currentID > 0 {
		groupNameQuery = groupNameQuery.Where("id <> ?", currentID)
	}
	if err := groupNameQuery.Count(&groupNameCount).Error; err != nil {
		return err
	}
	if groupNameCount > 0 {
		return fmt.Errorf("策略组名称已存在，请使用其他名称")
	}

	if payload.MixedPort > 0 {
		var mixedPortCount int64
		mixedPortQuery := model.DB.Model(&model.PortProfile{}).Where("mixed_port = ?", payload.MixedPort)
		if currentID > 0 {
			mixedPortQuery = mixedPortQuery.Where("id <> ?", currentID)
		}
		if err := mixedPortQuery.Count(&mixedPortCount).Error; err != nil {
			return err
		}
		if mixedPortCount > 0 {
			return fmt.Errorf("Mixed 端口已被其他端口配置占用")
		}
	}

	return nil
}

func normalizeLoadBalanceStrategy(value string) string {
	switch strings.TrimSpace(value) {
	case "round-robin", "sticky-sessions":
		return strings.TrimSpace(value)
	default:
		return "consistent-hashing"
	}
}

func validatePortProfilePorts(payload *PortProfilePayload) error {
	ports := make(map[int]string)
	addPort := func(port int, label string) error {
		if port == 0 {
			return nil
		}
		if port < 0 || port > 65535 {
			return fmt.Errorf("%s 端口无效", label)
		}
		if existing, ok := ports[port]; ok {
			return fmt.Errorf("%s 与 %s 端口冲突", label, existing)
		}
		ports[port] = label
		return nil
	}
	if err := addPort(payload.MixedPort, "Mixed"); err != nil {
		return err
	}
	if err := addPort(payload.SocksPort, "SOCKS"); err != nil {
		return err
	}
	if err := addPort(payload.HTTPPort, "HTTP"); err != nil {
		return err
	}
	return nil
}

func resolveSelectedNodes(nodeIDs []int) ([]*model.ProxyNode, error) {
	nodes, err := model.FindProxyNodesByIDs(nodeIDs)
	if err != nil {
		return nil, err
	}
	if len(nodes) != len(nodeIDs) {
		return nil, fmt.Errorf("部分节点不存在，请重新选择")
	}
	nodeMap := make(map[int]*model.ProxyNode, len(nodes))
	for _, node := range nodes {
		nodeMap[node.ID] = node
	}
	ordered := make([]*model.ProxyNode, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		ordered = append(ordered, nodeMap[id])
	}
	return ordered, nil
}

func orderProxyNodesByIDs(nodeIDs []int, nodes []*model.ProxyNode) []*model.ProxyNode {
	nodeMap := make(map[int]*model.ProxyNode, len(nodes))
	for _, node := range nodes {
		if node != nil {
			nodeMap[node.ID] = node
		}
	}
	ordered := make([]*model.ProxyNode, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if node := nodeMap[id]; node != nil {
			ordered = append(ordered, node)
		}
	}
	return ordered
}

func replacePortProfileNodes(tx *gorm.DB, profileID int, nodeIDs []int) error {
	if err := tx.Where("port_profile_id = ?", profileID).Delete(&model.PortProfileNode{}).Error; err != nil {
		return err
	}
	items := make([]model.PortProfileNode, 0, len(nodeIDs))
	for index, nodeID := range nodeIDs {
		items = append(items, model.PortProfileNode{
			PortProfileID: profileID,
			ProxyNodeID:   nodeID,
			SortOrder:     index,
		})
	}
	if len(items) == 0 {
		return nil
	}
	return tx.Create(&items).Error
}

func deduplicateNodeIDs(nodeIDs []int) []int {
	seen := make(map[int]struct{}, len(nodeIDs))
	result := make([]int, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func normalizePortProfileProxySettings(settings model.PortProfileProxySettings) model.PortProfileProxySettings {
	result := model.DefaultPortProfileProxySettings()
	if settings != (model.PortProfileProxySettings{}) {
		result.StrategyType = settings.StrategyType
		result.EntryMode = settings.EntryMode
		result.TestURL = settings.TestURL
		result.TestIntervalSeconds = settings.TestIntervalSeconds
		result.TestTimeoutMilliseconds = settings.TestTimeoutMilliseconds
		result.MaxFailedTimes = settings.MaxFailedTimes
		result.ExpectedStatus = settings.ExpectedStatus
		result.URLTestToleranceMilliseconds = settings.URLTestToleranceMilliseconds
		result.LoadBalanceStrategy = settings.LoadBalanceStrategy
		result.LoadBalanceLazy = settings.LoadBalanceLazy
		result.LoadBalanceDisableUDP = settings.LoadBalanceDisableUDP
		result.UDPEnabled = settings.UDPEnabled
	}
	switch strings.TrimSpace(result.StrategyType) {
	case model.PortProfileStrategyURLTest, model.PortProfileStrategyFallback, model.PortProfileStrategyLoadBalance:
		result.StrategyType = strings.TrimSpace(result.StrategyType)
	default:
		result.StrategyType = model.PortProfileStrategySelect
	}
	switch strings.TrimSpace(result.EntryMode) {
	case model.PortProfileEntryModeRule:
		result.EntryMode = model.PortProfileEntryModeRule
	default:
		result.EntryMode = model.PortProfileEntryModeFixed
	}
	defaults := model.DefaultPortProfileProxySettings()
	result.TestURL = fallbackPortProfileString(strings.TrimSpace(settings.TestURL), defaults.TestURL)
	result.TestIntervalSeconds = normalizePortProfilePositive(settings.TestIntervalSeconds, defaults.TestIntervalSeconds)
	result.TestTimeoutMilliseconds = normalizePortProfilePositive(settings.TestTimeoutMilliseconds, defaults.TestTimeoutMilliseconds)
	if result.TestTimeoutMilliseconds > 60000 {
		result.TestTimeoutMilliseconds = 60000
	}
	result.MaxFailedTimes = normalizePortProfilePositive(settings.MaxFailedTimes, defaults.MaxFailedTimes)
	if result.MaxFailedTimes > 100 {
		result.MaxFailedTimes = 100
	}
	result.ExpectedStatus = fallbackPortProfileString(strings.TrimSpace(settings.ExpectedStatus), defaults.ExpectedStatus)
	result.URLTestToleranceMilliseconds = settings.URLTestToleranceMilliseconds
	if result.URLTestToleranceMilliseconds < 0 {
		result.URLTestToleranceMilliseconds = 0
	}
	if result.URLTestToleranceMilliseconds > 65535 {
		result.URLTestToleranceMilliseconds = 65535
	}
	result.LoadBalanceStrategy = normalizeLoadBalanceStrategy(settings.LoadBalanceStrategy)
	result.AuthEnabled = settings.AuthEnabled
	result.AuthUsername = strings.TrimSpace(settings.AuthUsername)
	result.AuthPassword = strings.TrimSpace(settings.AuthPassword)
	if !result.AuthEnabled {
		result.AuthUsername = ""
		result.AuthPassword = ""
	}
	return result
}

func mustEncodePortProfileProxySettings(settings model.PortProfileProxySettings) string {
	encoded, err := model.EncodePortProfileProxySettings(settings)
	if err != nil {
		return "{}"
	}
	return encoded
}

func fallbackPortProfileString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func normalizePortProfilePositive(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func normalizeExpectedStatus(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "*" {
		return "*", nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == ',' })
	if len(parts) == 0 || len(parts) > 28 {
		return "", fmt.Errorf("范围数量必须在 1 到 28 之间")
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		bounds := strings.Split(strings.TrimSpace(part), "-")
		if len(bounds) < 1 || len(bounds) > 2 {
			return "", fmt.Errorf("范围格式错误")
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil || start < 100 || start > 599 {
			return "", fmt.Errorf("状态码必须在 100 到 599 之间")
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil || end < 100 || end > 599 {
				return "", fmt.Errorf("状态码必须在 100 到 599 之间")
			}
		}
		if start > end {
			start, end = end, start
		}
		if start == end {
			result = append(result, strconv.Itoa(start))
		} else {
			result = append(result, fmt.Sprintf("%d-%d", start, end))
		}
	}
	return strings.Join(result, "/"), nil
}
