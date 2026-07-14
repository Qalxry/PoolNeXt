package service

import (
	"context"
	"fmt"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	kernelpkg "poolx/internal/pkg/kernel"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const maxNodeTestParallelism = 4

var runNodeKernelTest = kernelpkg.TestNodeWithMihomo
var nodeProbeSlots = make(chan struct{}, maxNodeTestParallelism)

type ProxyNodeListInput struct {
	Page           int
	Offset         int
	Limit          int
	Keyword        string
	SourceConfigID int
	Enabled        *bool
	TestStatus     string
	Quarantined    *bool
}

type NodeTestInput struct {
	NodeIDs          []int    `json:"node_ids"`
	NodeFingerprints []string `json:"node_fingerprints"`
	TimeoutMS        int      `json:"timeout_ms"`
	TestURL          string   `json:"test_url"`
}

type ProxyNodeDeleteInput struct {
	NodeIDs []int `json:"node_ids"`
}

type ProxyNodeTagsInput struct {
	NodeIDs []int  `json:"node_ids"`
	Tags    string `json:"tags"`
}

type NodeTestExecution struct {
	NodeID               int        `json:"node_id"`
	NodeName             string     `json:"node_name"`
	Status               string     `json:"status"`
	LatencyMS            *int       `json:"latency_ms,omitempty"`
	ErrorMessage         string     `json:"error_message,omitempty"`
	TestURL              string     `json:"test_url,omitempty"`
	DialAddress          string     `json:"dial_address"`
	Cached               bool       `json:"cached"`
	StartedAt            time.Time  `json:"started_at"`
	FinishedAt           time.Time  `json:"finished_at"`
	LastTestedAt         *time.Time `json:"last_tested_at,omitempty"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	ConsecutiveSuccesses int        `json:"consecutive_successes"`
	Quarantined          bool       `json:"quarantined"`
	AvailabilityChanged  bool       `json:"availability_changed"`
}

func ListProxyNodes(input ProxyNodeListInput) ([]*model.ProxyNode, error) {
	page := input.Page
	if page < 0 {
		page = 0
	}
	limit := input.Limit
	if limit <= 0 {
		limit = common.ItemsPerPage
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	if input.Offset == 0 && page > 0 {
		offset = page * limit
	}
	return model.ListProxyNodes(offset, limit, model.ProxyNodeListFilter{
		Keyword:        strings.TrimSpace(input.Keyword),
		SourceConfigID: input.SourceConfigID,
		Enabled:        input.Enabled,
		TestStatus:     normalizeNodeTestStatusFilter(input.TestStatus),
		Quarantined:    input.Quarantined,
	})
}

func SetProxyNodeEnabled(id int, enabled bool) error {
	var node model.ProxyNode
	if err := model.DB.Select("id", "enabled").First(&node, "id = ?", id).Error; err != nil {
		return fmt.Errorf("节点不存在")
	}
	if node.Enabled == enabled {
		return nil
	}
	result := model.DB.Model(&model.ProxyNode{}).Where("id = ?", id).Update("enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("节点不存在")
	}
	return reloadRuntimeAfterNodeSetChange("节点启用状态变化")
}

func DeleteProxyNode(id int) error {
	if id <= 0 {
		return fmt.Errorf("无效的节点 ID")
	}
	if _, err := model.GetProxyNodeByID(id); err != nil {
		return fmt.Errorf("节点不存在")
	}
	if _, err := deleteProxyNodesWithRelations([]int{id}); err != nil {
		return err
	}
	return reloadRuntimeAfterNodeSetChange("节点删除")
}

func DeleteProxyNodes(ids []int) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("请先选择要删除的节点")
	}

	seen := make(map[int]struct{}, len(ids))
	validIDs := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		validIDs = append(validIDs, id)
	}
	if len(validIDs) == 0 {
		return 0, fmt.Errorf("请先选择要删除的节点")
	}
	deleted, err := deleteProxyNodesWithRelations(validIDs)
	if err != nil {
		return 0, err
	}
	if err := reloadRuntimeAfterNodeSetChange("节点批量删除"); err != nil {
		return deleted, err
	}
	return deleted, nil
}

func deleteProxyNodesWithRelations(ids []int) (int, error) {
	deleted := 0
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var profileIDs []int
		if err := tx.Model(&model.PortProfileNode{}).
			Where("proxy_node_id IN ?", ids).
			Distinct("port_profile_id").
			Pluck("port_profile_id", &profileIDs).Error; err != nil {
			return err
		}
		if err := tx.Where("proxy_node_id IN ?", ids).Delete(&model.PortProfileNode{}).Error; err != nil {
			return err
		}
		if len(profileIDs) > 0 {
			for _, profileID := range profileIDs {
				var remaining int64
				if err := tx.Model(&model.PortProfileNode{}).
					Where("port_profile_id = ?", profileID).
					Count(&remaining).Error; err != nil {
					return err
				}
				if remaining == 0 {
					if err := tx.Model(&model.PortProfile{}).
						Where("id = ?", profileID).
						Update("include_in_runtime", false).Error; err != nil {
						return err
					}
				}
			}
			if err := tx.Where("port_profile_id IN ?", profileIDs).Delete(&model.RuntimeConfig{}).Error; err != nil {
				return err
			}
		}

		result := tx.Delete(&model.ProxyNode{}, "id IN ?", ids)
		if result.Error != nil {
			return result.Error
		}
		deleted = int(result.RowsAffected)
		if deleted == 0 {
			return fmt.Errorf("未找到可删除的节点")
		}
		return nil
	})
	return deleted, err
}

func UpdateProxyNodeTags(ids []int, tags string) (int, error) {
	if len(ids) == 0 {
		return 0, fmt.Errorf("请先选择至少一个节点")
	}
	normalized := normalizeProxyNodeTags(tags)
	result := model.DB.Model(&model.ProxyNode{}).Where("id IN ?", ids).Update("tags", normalized)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, fmt.Errorf("未找到可更新的节点")
	}
	return int(result.RowsAffected), nil
}

func ExecuteNodeTests(ctx context.Context, input NodeTestInput) ([]NodeTestExecution, error) {
	return executeNodeTests(ctx, input, maxNodeTestParallelism)
}

func executeNodeTests(ctx context.Context, input NodeTestInput, parallelism int) ([]NodeTestExecution, error) {
	if len(input.NodeIDs) == 0 {
		return nil, fmt.Errorf("请先选择要测试的节点")
	}
	if strings.TrimSpace(common.MihomoBinaryPath) == "" {
		return nil, fmt.Errorf("请先在系统设置中完成 Mihomo 二进制安装或路径校验")
	}
	timeout := normalizeNodeTestTimeout(input.TimeoutMS)
	testURL := normalizeNodeTestURL(input.TestURL)

	nodes, err := model.FindProxyNodesByIDs(input.NodeIDs)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("未找到可测试的节点")
	}

	type indexedExecution struct {
		index int
		item  NodeTestExecution
	}
	jobs := make(chan int)
	resultsCh := make(chan indexedExecution, len(nodes))
	workerCount := minNodeTestWorkerCount(len(nodes), parallelism)
	if workerCount <= 0 {
		workerCount = 1
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				node := nodes[index]
				execution := buildNodeTestExecutionWithSlot(ctx, node, timeout, testURL)
				resultsCh <- indexedExecution{index: index, item: execution}
			}
		}()
	}
	for index := range nodes {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	close(resultsCh)

	results := make([]NodeTestExecution, 0, len(nodes))
	ordered := make([]indexedExecution, 0, len(nodes))
	for item := range resultsCh {
		ordered = append(ordered, item)
	}
	sort.Slice(ordered, func(left int, right int) bool {
		return ordered[left].index < ordered[right].index
	})
	for _, item := range ordered {
		execution, err := persistNodeTestExecution(item.item.NodeID, item.item)
		if err != nil {
			return nil, err
		}
		results = append(results, execution)
	}
	reloadRuntimeAfterNodeHealthChanges(results)

	return results, nil
}

type metadataNodeTestInput struct {
	NodeID       int
	Name         string
	Server       string
	Port         int
	MetadataJSON string
	Timeout      time.Duration
	TestURL      string
}

func executeMetadataNodeTest(ctx context.Context, input metadataNodeTestInput) NodeTestExecution {
	startedAt := time.Now()
	dialAddress := fmt.Sprintf("%s:%d", input.Server, input.Port)

	execution := NodeTestExecution{
		NodeID:      input.NodeID,
		NodeName:    input.Name,
		TestURL:     input.TestURL,
		DialAddress: dialAddress,
		StartedAt:   startedAt,
	}

	result, err := runNodeKernelTest(ctx, kernelpkg.MihomoNodeTestInput{
		BinaryPath:   common.MihomoBinaryPath,
		ProxyName:    input.Name,
		MetadataJSON: input.MetadataJSON,
		TestURL:      input.TestURL,
		Timeout:      input.Timeout,
	})
	finishedAt := time.Now()
	execution.FinishedAt = finishedAt

	if err != nil {
		execution.Status = model.NodeTestStatusFailed
		execution.ErrorMessage = err.Error()
		execution.LastTestedAt = &finishedAt
		return execution
	}

	latency := result.LatencyMS
	execution.Status = model.NodeTestStatusSuccess
	execution.LatencyMS = &latency
	execution.LastTestedAt = &finishedAt
	return execution
}

func buildNodeTestExecution(ctx context.Context, node *model.ProxyNode, timeout time.Duration, testURL string) NodeTestExecution {
	return executeMetadataNodeTest(ctx, metadataNodeTestInput{
		NodeID:       node.ID,
		Name:         node.Name,
		Server:       node.Server,
		Port:         node.Port,
		MetadataJSON: node.MetadataJSON,
		Timeout:      timeout,
		TestURL:      testURL,
	})
}

func buildNodeTestExecutionWithSlot(ctx context.Context, node *model.ProxyNode, timeout time.Duration, testURL string) NodeTestExecution {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case nodeProbeSlots <- struct{}{}:
		defer func() { <-nodeProbeSlots }()
	case <-ctx.Done():
		now := time.Now()
		return NodeTestExecution{
			NodeID:       node.ID,
			NodeName:     node.Name,
			Status:       model.NodeTestStatusUnknown,
			ErrorMessage: ctx.Err().Error(),
			TestURL:      testURL,
			DialAddress:  fmt.Sprintf("%s:%d", node.Server, node.Port),
			StartedAt:    now,
			FinishedAt:   now,
		}
	}
	return buildNodeTestExecution(ctx, node, timeout, testURL)
}

func normalizeNodeTestTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		timeoutMS = common.NodeTestDefaultTimeoutMS
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if timeout > 60*time.Second {
		timeout = 60 * time.Second
	}
	return timeout
}

func normalizeNodeTestURL(testURL string) string {
	value := strings.TrimSpace(testURL)
	if value == "" {
		value = strings.TrimSpace(common.NodeTestDefaultURL)
	}
	if value == "" {
		return common.DefaultNodeTestURL
	}
	return value
}

func normalizeProxyNodeTags(tags string) string {
	if strings.TrimSpace(tags) == "" {
		return ""
	}
	parts := strings.FieldsFunc(tags, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n'
	})
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return strings.Join(result, ", ")
}

func normalizeNodeTestStatusFilter(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case model.NodeTestStatusUnknown, model.NodeTestStatusSuccess, model.NodeTestStatusFailed:
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func minNodeTestWorkerCount(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
