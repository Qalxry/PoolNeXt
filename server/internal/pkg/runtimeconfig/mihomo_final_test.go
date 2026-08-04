package runtimeconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"poolx/internal/model"
)

func TestRenderFinalMihomoConfigAggregatesProfiles(t *testing.T) {
	result, err := RenderFinalMihomoConfig(AggregatedMihomoInput{
		AllowLAN:          false,
		Mode:              "rule",
		ControllerAddress: "127.0.0.1:19090",
		ControllerSecret:  "secret",
		Profiles: []*model.PortProfileWithNodes{
			{
				Profile: model.PortProfile{
					ID:         1,
					Name:       "GROUP-A",
					ListenHost: "127.0.0.1",
					MixedPort:  7890,
					ProxySettings: model.PortProfileProxySettings{
						StrategyType: model.PortProfileStrategySelect,
						UDPEnabled:   true,
					},
					IncludeInRuntime: true,
				},
				Nodes: []*model.ProxyNode{
					{
						ID:           11,
						Name:         "hk-1",
						MetadataJSON: `{"type":"ss","server":"1.1.1.1","port":443,"cipher":"aes-128-gcm","password":"secret"}`,
					},
				},
			},
			{
				Profile: model.PortProfile{
					ID:         2,
					Name:       "GROUP-A",
					ListenHost: "127.0.0.1",
					SocksPort:  7891,
					ProxySettings: model.PortProfileProxySettings{
						StrategyType:        model.PortProfileStrategyFallback,
						TestURL:             "https://cp.cloudflare.com/generate_204",
						TestIntervalSeconds: 300,
						UDPEnabled:          true,
					},
					IncludeInRuntime: true,
				},
				Nodes: []*model.ProxyNode{
					{
						ID:           12,
						Name:         "hk-1",
						MetadataJSON: `{"type":"ss","server":"2.2.2.2","port":443,"cipher":"aes-128-gcm","password":"secret-2"}`,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderFinalMihomoConfig returned error: %v", err)
	}
	if result.ListenerCount != 2 {
		t.Fatalf("expected 2 listeners, got %d", result.ListenerCount)
	}
	if !strings.Contains(result.Content, "external-controller: 127.0.0.1:19090") {
		t.Fatalf("expected controller in final config, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "listeners:") {
		t.Fatalf("expected listeners section, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "proxy: GROUP-A-2") {
		t.Fatalf("expected second group name to be uniqued, got %s", result.Content)
	}
}

func TestRenderFinalMihomoConfigSkipsProfilesExcludedFromRuntime(t *testing.T) {
	result, err := RenderFinalMihomoConfig(AggregatedMihomoInput{
		AllowLAN:          false,
		Mode:              "rule",
		ControllerAddress: "127.0.0.1:19090",
		ControllerSecret:  "secret",
		Profiles: []*model.PortProfileWithNodes{
			{
				Profile: model.PortProfile{
					ID:               1,
					Name:             "excluded",
					ListenHost:       "127.0.0.1",
					MixedPort:        7890,
					IncludeInRuntime: false,
				},
				Nodes: []*model.ProxyNode{
					{
						ID:           11,
						Name:         "hk-1",
						MetadataJSON: `{"type":"ss","server":"1.1.1.1","port":443,"cipher":"aes-128-gcm","password":"secret"}`,
					},
				},
			},
			{
				Profile: model.PortProfile{
					ID:               2,
					Name:             "included",
					ListenHost:       "127.0.0.1",
					MixedPort:        7891,
					IncludeInRuntime: true,
				},
				Nodes: []*model.ProxyNode{
					{
						ID:           12,
						Name:         "jp-1",
						MetadataJSON: `{"type":"ss","server":"2.2.2.2","port":443,"cipher":"aes-128-gcm","password":"secret-2"}`,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderFinalMihomoConfig returned error: %v", err)
	}
	if result.ListenerCount != 1 {
		t.Fatalf("expected 1 listener, got %d", result.ListenerCount)
	}
	if strings.Contains(result.Content, "7890") {
		t.Fatalf("expected excluded listener to be omitted, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "7891") {
		t.Fatalf("expected included listener to remain, got %s", result.Content)
	}
}

func TestRenderFinalMihomoConfigUsesConfiguredClashSettings(t *testing.T) {
	result, err := RenderFinalMihomoConfig(AggregatedMihomoInput{
		AllowLAN:          true,
		Mode:              "global",
		ControllerAddress: "0.0.0.0:29090",
		ControllerSecret:  "fixed-secret",
		Profiles: []*model.PortProfileWithNodes{
			{
				Profile: model.PortProfile{
					ID:               2,
					Name:             "included",
					ListenHost:       "127.0.0.1",
					MixedPort:        7891,
					IncludeInRuntime: true,
				},
				Nodes: []*model.ProxyNode{
					{
						ID:           12,
						Name:         "jp-1",
						MetadataJSON: `{"type":"ss","server":"2.2.2.2","port":443,"cipher":"aes-128-gcm","password":"secret-2"}`,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RenderFinalMihomoConfig returned error: %v", err)
	}
	if !strings.Contains(result.Content, "allow-lan: true") {
		t.Fatalf("expected allow-lan in final config, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "mode: global") {
		t.Fatalf("expected mode in final config, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "external-controller: 0.0.0.0:29090") {
		t.Fatalf("expected controller address in final config, got %s", result.Content)
	}
	if !strings.Contains(result.Content, "secret: fixed-secret") {
		t.Fatalf("expected secret in final config, got %s", result.Content)
	}
}

func TestRenderFinalMihomoConfigRoutesRuleEntryWithoutFixedProxy(t *testing.T) {
	result, err := RenderFinalMihomoConfig(AggregatedMihomoInput{
		AllowLAN:          false,
		Mode:              "rule",
		ControllerAddress: "127.0.0.1:19090",
		ControllerSecret:  "secret",
		Profiles: []*model.PortProfileWithNodes{
			{
				Profile: model.PortProfile{
					ID:         10,
					Name:       "RULE-ENTRY",
					ListenHost: "127.0.0.1",
					MixedPort:  17890,
					ProxySettings: model.PortProfileProxySettings{
						StrategyType:            model.PortProfileStrategyLoadBalance,
						EntryMode:               model.PortProfileEntryModeRule,
						TestURL:                 "https://cp.cloudflare.com/generate_204",
						TestIntervalSeconds:     60,
						TestTimeoutMilliseconds: 3000,
						MaxFailedTimes:          3,
						ExpectedStatus:          "200-299",
						LoadBalanceStrategy:     "sticky-sessions",
						LoadBalanceLazy:         true,
						UDPEnabled:              true,
					},
					IncludeInRuntime: true,
				},
				Nodes: []*model.ProxyNode{{
					ID:           1,
					Name:         "node-a",
					MetadataJSON: `{"type":"ss","server":"1.1.1.1","port":443,"cipher":"aes-128-gcm","password":"secret"}`,
				}},
			},
		},
		Rules: []*model.RoutingRule{
			{Name: "api", RuleType: model.RoutingRuleDomainSuffix, Payload: "example.com", TargetProfileID: 10, Enabled: true},
			{Name: "default", RuleType: model.RoutingRuleMatch, TargetProfileID: 10, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("RenderFinalMihomoConfig returned error: %v", err)
	}
	if strings.Contains(result.Content, "proxy: RULE-ENTRY") {
		t.Fatalf("rule-routed listener must not set a fixed proxy: %s", result.Content)
	}
	for _, expected := range []string{
		"DOMAIN-SUFFIX,example.com,RULE-ENTRY",
		"MATCH,RULE-ENTRY",
		"strategy: sticky-sessions",
		"timeout: 3000",
		"max-failed-times: 3",
		"expected-status: 200-299",
		"lazy: true",
	} {
		if !strings.Contains(result.Content, expected) {
			t.Fatalf("expected %q in rendered config: %s", expected, result.Content)
		}
	}
	if len(result.Listeners) != 1 || result.Listeners[0].EntryMode != model.PortProfileEntryModeRule {
		t.Fatalf("unexpected listener view: %+v", result.Listeners)
	}
}

func TestRenderFinalMihomoConfigRequiresDefaultForRuleEntry(t *testing.T) {
	_, err := RenderFinalMihomoConfig(AggregatedMihomoInput{
		ControllerAddress: "127.0.0.1:19090",
		ControllerSecret:  "secret",
		Profiles: []*model.PortProfileWithNodes{{
			Profile: model.PortProfile{
				ID: 1, Name: "rule-entry", ListenHost: "127.0.0.1", MixedPort: 7890,
				ProxySettings:    model.PortProfileProxySettings{EntryMode: model.PortProfileEntryModeRule},
				IncludeInRuntime: true,
			},
			Nodes: []*model.ProxyNode{{ID: 1, Name: "node", MetadataJSON: `{"type":"ss","server":"1.1.1.1","port":443,"cipher":"aes-128-gcm","password":"secret"}`}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "默认规则") {
		t.Fatalf("expected missing default rule error, got %v", err)
	}
}

func TestRenderFinalMihomoConfigRequiresRuleModeForRuleEntry(t *testing.T) {
	_, err := RenderFinalMihomoConfig(AggregatedMihomoInput{
		Mode: "global", ControllerAddress: "127.0.0.1:19090", ControllerSecret: "secret",
		Profiles: []*model.PortProfileWithNodes{{
			Profile: model.PortProfile{
				ID: 1, Name: "rule-entry", ListenHost: "127.0.0.1", MixedPort: 7890,
				ProxySettings:    model.PortProfileProxySettings{EntryMode: model.PortProfileEntryModeRule},
				IncludeInRuntime: true,
			},
			Nodes: []*model.ProxyNode{{ID: 1, Name: "node", MetadataJSON: `{"type":"ss","server":"1.1.1.1","port":443,"cipher":"aes-128-gcm","password":"secret"}`}},
		}},
		Rules: []*model.RoutingRule{{Name: "default", RuleType: model.RoutingRuleMatch, TargetProfileID: 1, Enabled: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "运行模式为 rule") {
		t.Fatalf("expected rule mode error, got %v", err)
	}
}

func TestBuildRoutingRulesRejectsInvalidPersistedRules(t *testing.T) {
	groupNames := map[int]string{1: "GROUP-A"}
	tests := []struct {
		name string
		rule *model.RoutingRule
		want string
	}{
		{
			name: "unsupported type",
			rule: &model.RoutingRule{Name: "corrupt-type", RuleType: "SCRIPT", Payload: "payload", TargetProfileID: 1, Enabled: true},
			want: "不受支持",
		},
		{
			name: "delimiter injection",
			rule: &model.RoutingRule{Name: "corrupt-payload", RuleType: model.RoutingRuleDomain, Payload: "example.com,DIRECT", TargetProfileID: 1, Enabled: true},
			want: "非法分隔符",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildRoutingRules([]*model.RoutingRule{test.rule}, groupNames, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestTargetMihomoAcceptsRuleRoutingConfig(t *testing.T) {
	const binaryPath = "/opt/clash-party/resources/sidecar/mihomo"
	if _, err := os.Stat(binaryPath); err != nil {
		t.Skipf("target Mihomo is unavailable: %v", err)
	}
	result, err := RenderFinalMihomoConfig(AggregatedMihomoInput{
		Mode: "rule", ControllerAddress: "127.0.0.1:19090", ControllerSecret: "secret",
		Profiles: []*model.PortProfileWithNodes{{
			Profile: model.PortProfile{
				ID: 1, Name: "RULE-GROUP", ListenHost: "127.0.0.1", MixedPort: 17899,
				ProxySettings: model.PortProfileProxySettings{
					StrategyType:        model.PortProfileStrategyURLTest,
					EntryMode:           model.PortProfileEntryModeRule,
					TestURL:             "https://cp.cloudflare.com/generate_204",
					TestIntervalSeconds: 300, TestTimeoutMilliseconds: 5000,
					MaxFailedTimes: 5, ExpectedStatus: "204",
					URLTestToleranceMilliseconds: 150, UDPEnabled: true,
				},
				IncludeInRuntime: true,
			},
			Nodes: []*model.ProxyNode{{ID: 1, Name: "ss-node", MetadataJSON: `{"type":"ss","server":"127.0.0.1","port":8388,"cipher":"aes-128-gcm","password":"secret"}`}},
		}},
		Rules: []*model.RoutingRule{
			{Name: "domain", RuleType: model.RoutingRuleDomainSuffix, Payload: "example.com", TargetProfileID: 1, Enabled: true},
			{Name: "default", RuleType: model.RoutingRuleMatch, TargetProfileID: 1, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("render target config: %v", err)
	}
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(result.Content), 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}
	command := exec.Command(binaryPath, "-t", "-d", workDir, "-f", configPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("target Mihomo rejected generated config: %v\n%s\n%s", err, output, result.Content)
	}
}
