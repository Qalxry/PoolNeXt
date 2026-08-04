package service

import (
	"fmt"
	"net/netip"
	"poolx/internal/model"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

type RoutingRulePayload struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	RuleType        string `json:"rule_type"`
	Payload         string `json:"payload"`
	TargetProfileID int    `json:"target_profile_id"`
	Enabled         bool   `json:"enabled"`
}

var supportedRoutingRuleTypes = map[string]struct{}{
	model.RoutingRuleDomain:          {},
	model.RoutingRuleDomainSuffix:    {},
	model.RoutingRuleDomainKeyword:   {},
	model.RoutingRuleIPCIDR:          {},
	model.RoutingRuleSourceIPCIDR:    {},
	model.RoutingRuleDestinationPort: {},
	model.RoutingRuleNetwork:         {},
	model.RoutingRuleInboundName:     {},
	model.RoutingRuleMatch:           {},
}

func GetRoutingRules() ([]*model.RoutingRule, error) {
	return model.ListRoutingRules()
}

func ReplaceRoutingRules(payloads []RoutingRulePayload) ([]*model.RoutingRule, error) {
	if len(payloads) > 200 {
		return nil, fmt.Errorf("规则数量不能超过 200 条")
	}

	normalized := make([]*model.RoutingRule, 0, len(payloads))
	seenDefault := false
	for index, payload := range payloads {
		rule, err := normalizeRoutingRule(payload, index)
		if err != nil {
			return nil, err
		}
		if rule.RuleType == model.RoutingRuleMatch && rule.Enabled {
			if seenDefault {
				return nil, fmt.Errorf("只能配置一条启用的默认规则")
			}
			seenDefault = true
			if index != len(payloads)-1 {
				return nil, fmt.Errorf("默认规则必须位于规则列表最后")
			}
		}
		normalized = append(normalized, rule)
	}

	if err := validateRoutingRuleTargets(normalized); err != nil {
		return nil, err
	}

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.RoutingRule{}).Error; err != nil {
			return err
		}
		if len(normalized) == 0 {
			return nil
		}
		return tx.Create(&normalized).Error
	}); err != nil {
		return nil, err
	}
	return model.ListRoutingRules()
}

func normalizeRoutingRule(payload RoutingRulePayload, index int) (*model.RoutingRule, error) {
	ruleType := strings.ToUpper(strings.TrimSpace(payload.RuleType))
	if _, ok := supportedRoutingRuleTypes[ruleType]; !ok {
		return nil, fmt.Errorf("第 %d 条规则类型不受支持", index+1)
	}
	if payload.TargetProfileID <= 0 {
		return nil, fmt.Errorf("第 %d 条规则未选择目标策略组", index+1)
	}

	rulePayload := strings.TrimSpace(payload.Payload)
	if ruleType == model.RoutingRuleMatch {
		rulePayload = ""
	} else if rulePayload == "" {
		return nil, fmt.Errorf("第 %d 条规则的匹配内容不能为空", index+1)
	}
	if strings.ContainsAny(rulePayload, "\r\n,") {
		return nil, fmt.Errorf("第 %d 条规则的匹配内容不能包含逗号或换行", index+1)
	}

	switch ruleType {
	case model.RoutingRuleDomain, model.RoutingRuleDomainSuffix:
		rulePayload = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(rulePayload, ".")))
		if rulePayload == "" || strings.Contains(rulePayload, " ") {
			return nil, fmt.Errorf("第 %d 条规则的域名无效", index+1)
		}
	case model.RoutingRuleDomainKeyword:
		if strings.Contains(rulePayload, " ") {
			return nil, fmt.Errorf("第 %d 条规则的域名关键字不能包含空格", index+1)
		}
	case model.RoutingRuleIPCIDR, model.RoutingRuleSourceIPCIDR:
		prefix, err := netip.ParsePrefix(rulePayload)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条规则的 CIDR 无效", index+1)
		}
		rulePayload = prefix.Masked().String()
	case model.RoutingRuleDestinationPort:
		normalized, err := normalizePortRanges(rulePayload)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条规则的目标端口无效: %v", index+1, err)
		}
		rulePayload = normalized
	case model.RoutingRuleNetwork:
		rulePayload = strings.ToUpper(rulePayload)
		if rulePayload != "TCP" && rulePayload != "UDP" {
			return nil, fmt.Errorf("第 %d 条规则的网络类型必须是 TCP 或 UDP", index+1)
		}
	case model.RoutingRuleInboundName:
		for _, name := range strings.Split(rulePayload, "/") {
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("第 %d 条规则的入口名称无效", index+1)
			}
		}
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		name = fmt.Sprintf("规则 %d", index+1)
	}
	return &model.RoutingRule{
		Name:            name,
		RuleType:        ruleType,
		Payload:         rulePayload,
		TargetProfileID: payload.TargetProfileID,
		Enabled:         payload.Enabled,
		SortOrder:       index,
	}, nil
}

func normalizePortRanges(value string) (string, error) {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' })
	if len(parts) == 0 || len(parts) > 28 {
		return "", fmt.Errorf("端口范围数量必须在 1 到 28 之间")
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		bounds := strings.Split(strings.TrimSpace(part), "-")
		if len(bounds) < 1 || len(bounds) > 2 {
			return "", fmt.Errorf("范围格式错误")
		}
		start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil || start < 1 || start > 65535 {
			return "", fmt.Errorf("端口超出范围")
		}
		end := start
		if len(bounds) == 2 {
			end, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil || end < 1 || end > 65535 {
				return "", fmt.Errorf("端口超出范围")
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

func validateRoutingRuleTargets(rules []*model.RoutingRule) error {
	if len(rules) == 0 {
		return nil
	}
	ids := make([]int, 0, len(rules))
	seen := make(map[int]struct{}, len(rules))
	for _, rule := range rules {
		if _, ok := seen[rule.TargetProfileID]; !ok {
			seen[rule.TargetProfileID] = struct{}{}
			ids = append(ids, rule.TargetProfileID)
		}
	}
	var profiles []*model.PortProfile
	if err := model.DB.Where("id IN ?", ids).Find(&profiles).Error; err != nil {
		return err
	}
	valid := make(map[int]bool, len(profiles))
	for _, profile := range profiles {
		valid[profile.ID] = profile.IncludeInRuntime
	}
	for _, rule := range rules {
		if !valid[rule.TargetProfileID] {
			return fmt.Errorf("规则 %s 的目标策略组不存在或未随运行加载", rule.Name)
		}
	}
	return nil
}

func listEnabledRoutingRulesForRuntime(profileIDs []int) ([]*model.RoutingRule, error) {
	rules, err := model.ListRoutingRules()
	if err != nil {
		return nil, err
	}
	allowed := make(map[int]struct{}, len(profileIDs))
	for _, id := range profileIDs {
		allowed[id] = struct{}{}
	}
	result := make([]*model.RoutingRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if _, ok := allowed[rule.TargetProfileID]; !ok {
			return nil, fmt.Errorf("规则 %s 引用了未加入运行配置的策略组", rule.Name)
		}
		result = append(result, rule)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].SortOrder < result[j].SortOrder })
	return result, nil
}
