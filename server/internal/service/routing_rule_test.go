package service

import (
	"strings"
	"testing"

	"poolx/internal/model"
)

func TestReplaceRoutingRulesNormalizesAndOrdersRules(t *testing.T) {
	setupServiceTestDB(t)
	node := seedRuntimeTestNode(t, "routing-node")
	profile, err := CreatePortProfile(PortProfilePayload{
		Name: "ROUTING", ListenHost: "127.0.0.1", MixedPort: 18990,
		ProxySettings: model.DefaultPortProfileProxySettings(), IncludeInRuntime: true,
		NodeIDs: []int{node.ID},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	rules, err := ReplaceRoutingRules([]RoutingRulePayload{
		{Name: "ports", RuleType: model.RoutingRuleDestinationPort, Payload: "9000-8000/443", TargetProfileID: profile.Profile.ID, Enabled: true},
		{Name: "default", RuleType: model.RoutingRuleMatch, TargetProfileID: profile.Profile.ID, Enabled: true},
	})
	if err != nil {
		t.Fatalf("replace rules: %v", err)
	}
	if len(rules) != 2 || rules[0].Payload != "8000-9000/443" || rules[1].SortOrder != 1 {
		t.Fatalf("unexpected normalized rules: %+v", rules)
	}
}

func TestReplaceRoutingRulesRejectsUnavailableTargetAndMisplacedDefault(t *testing.T) {
	setupServiceTestDB(t)
	_, err := ReplaceRoutingRules([]RoutingRulePayload{{
		RuleType: model.RoutingRuleMatch, TargetProfileID: 999, Enabled: true,
	}})
	if err == nil || !strings.Contains(err.Error(), "不存在或未随运行加载") {
		t.Fatalf("expected unavailable target error, got %v", err)
	}
}

func TestDeletePortProfileRejectsRoutingReferences(t *testing.T) {
	setupServiceTestDB(t)
	node := seedRuntimeTestNode(t, "routing-delete-node")
	profile, err := CreatePortProfile(PortProfilePayload{
		Name: "ROUTING-DELETE", ListenHost: "127.0.0.1", MixedPort: 18991,
		ProxySettings: model.DefaultPortProfileProxySettings(), IncludeInRuntime: true,
		NodeIDs: []int{node.ID},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if _, err := ReplaceRoutingRules([]RoutingRulePayload{{
		Name: "default", RuleType: model.RoutingRuleMatch,
		TargetProfileID: profile.Profile.ID, Enabled: true,
	}}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	err = DeletePortProfile(profile.Profile.ID)
	if err == nil || !strings.Contains(err.Error(), "分流规则引用") {
		t.Fatalf("expected referenced profile delete error, got %v", err)
	}
	payload := toPortProfilePayloadForTest(profile)
	payload.IncludeInRuntime = false
	if _, err := UpdatePortProfile(profile.Profile.ID, payload); err == nil || !strings.Contains(err.Error(), "关闭随运行加载") {
		t.Fatalf("expected referenced profile exclusion error, got %v", err)
	}
}

func toPortProfilePayloadForTest(profile *model.PortProfileWithNodes) PortProfilePayload {
	return PortProfilePayload{
		Name:             profile.Profile.Name,
		ListenHost:       profile.Profile.ListenHost,
		MixedPort:        profile.Profile.MixedPort,
		SocksPort:        profile.Profile.SocksPort,
		HTTPPort:         profile.Profile.HTTPPort,
		ProxySettings:    profile.Profile.ProxySettings,
		IncludeInRuntime: profile.Profile.IncludeInRuntime,
		NodeIDs:          profile.NodeIDs,
	}
}
