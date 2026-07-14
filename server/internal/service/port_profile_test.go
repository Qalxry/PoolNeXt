package service

import (
	"strings"
	"testing"
	"time"

	"poolx/internal/model"
)

func TestCreatePortProfileUsesStrategyGroupNameAsName(t *testing.T) {
	setupServiceTestDB(t)

	node := seedRuntimeTestNode(t, "profile-name-node")
	profile, err := CreatePortProfile(PortProfilePayload{
		Name:       "POOLX-PRIMARY",
		ListenHost: "127.0.0.1",
		MixedPort:  17890,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{node.ID},
	})
	if err != nil {
		t.Fatalf("create port profile: %v", err)
	}

	if profile.Profile.Name != "POOLX-PRIMARY" {
		t.Fatalf("expected profile name to use the submitted name, got %q", profile.Profile.Name)
	}
}

func TestNormalizePortProfilePayloadUsesPoolNeXtDefaultName(t *testing.T) {
	normalized, err := normalizePortProfilePayload(PortProfilePayload{
		MixedPort: 17895,
		NodeIDs:   []int{1},
	})
	if err != nil {
		t.Fatalf("normalize port profile: %v", err)
	}
	if normalized.Name != "PoolNeXt" {
		t.Fatalf("expected PoolNeXt default profile name, got %q", normalized.Name)
	}
	if normalized.ProxySettings.StrategyType != model.PortProfileStrategyFallback {
		t.Fatalf("expected fallback as the default proxy-pool strategy, got %q", normalized.ProxySettings.StrategyType)
	}
}

func TestCreatePortProfileRejectsDuplicateStrategyGroupName(t *testing.T) {
	setupServiceTestDB(t)

	firstNode := seedRuntimeTestNode(t, "duplicate-group-node-1")
	secondNode := seedRuntimeTestNode(t, "duplicate-group-node-2")

	if _, err := CreatePortProfile(PortProfilePayload{
		Name:       "POOLX-DUPLICATE",
		ListenHost: "127.0.0.1",
		MixedPort:  17891,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{firstNode.ID},
	}); err != nil {
		t.Fatalf("create first port profile: %v", err)
	}

	_, err := CreatePortProfile(PortProfilePayload{
		Name:       "poolx-duplicate",
		ListenHost: "127.0.0.1",
		MixedPort:  17892,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{secondNode.ID},
	})
	if err == nil {
		t.Fatal("expected duplicate strategy group name to fail")
	}
	if !strings.Contains(err.Error(), "策略组名称已存在") {
		t.Fatalf("expected duplicate strategy group name error, got %v", err)
	}
}

func TestCreatePortProfileRejectsDuplicateMixedPort(t *testing.T) {
	setupServiceTestDB(t)

	firstNode := seedRuntimeTestNode(t, "duplicate-port-node-1")
	secondNode := seedRuntimeTestNode(t, "duplicate-port-node-2")

	if _, err := CreatePortProfile(PortProfilePayload{
		Name:       "POOLX-ONE",
		ListenHost: "127.0.0.1",
		MixedPort:  17893,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{firstNode.ID},
	}); err != nil {
		t.Fatalf("create first port profile: %v", err)
	}

	_, err := CreatePortProfile(PortProfilePayload{
		Name:       "POOLX-TWO",
		ListenHost: "127.0.0.1",
		MixedPort:  17893,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{secondNode.ID},
	})
	if err == nil {
		t.Fatal("expected duplicate mixed port to fail")
	}
	if !strings.Contains(err.Error(), "Mixed 端口已被其他端口配置占用") {
		t.Fatalf("expected duplicate mixed port error, got %v", err)
	}
}

func TestCreatePortProfileIncludesProxySettingsInJSON(t *testing.T) {
	setupServiceTestDB(t)

	node := seedRuntimeTestNode(t, "json-settings-node")
	profile, err := CreatePortProfile(PortProfilePayload{
		Name:       "POOLX-JSON",
		ListenHost: "127.0.0.1",
		MixedPort:  17894,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
			AuthEnabled:         true,
			AuthUsername:        "username1",
			AuthPassword:        "password1",
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{node.ID},
	})
	if err != nil {
		t.Fatalf("create port profile: %v", err)
	}
	if !profile.Profile.ProxySettings.AuthEnabled || profile.Profile.ProxySettings.AuthUsername != "username1" {
		t.Fatalf("expected proxy settings to round-trip from json, got %+v", profile.Profile.ProxySettings)
	}
}

func TestPortProfileViewPreservesConfiguredNodeOrder(t *testing.T) {
	setupServiceTestDB(t)

	first := seedRuntimeTestNode(t, "ordered-node-1")
	second := seedRuntimeTestNode(t, "ordered-node-2")
	third := seedRuntimeTestNode(t, "ordered-node-3")
	profile, err := CreatePortProfile(PortProfilePayload{
		Name:             "ORDERED",
		ListenHost:       "127.0.0.1",
		MixedPort:        17901,
		ProxySettings:    model.DefaultPortProfileProxySettings(),
		IncludeInRuntime: true,
		NodeIDs:          []int{third.ID, first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("create ordered profile: %v", err)
	}
	want := []int{third.ID, first.ID, second.ID}
	for index, node := range profile.Nodes {
		if node.ID != want[index] {
			t.Fatalf("node order mismatch at %d: got %d want %d", index, node.ID, want[index])
		}
	}
}

func TestSelectRuntimeNodesExcludesDisabledAndQuarantined(t *testing.T) {
	now := time.Now()
	nodes := []*model.ProxyNode{
		{ID: 1, Enabled: true},
		{ID: 2, Enabled: false},
		{ID: 3, Enabled: true, QuarantinedAt: &now},
		{ID: 4, Enabled: true},
	}
	selected := selectRuntimeNodes(nodes)
	if len(selected) != 2 || selected[0].ID != 1 || selected[1].ID != 4 {
		t.Fatalf("unexpected runtime nodes: %+v", selected)
	}

	allQuarantined := selectRuntimeNodes([]*model.ProxyNode{
		{ID: 5, Enabled: true, QuarantinedAt: &now},
		{ID: 6, Enabled: true, QuarantinedAt: &now},
	})
	if len(allQuarantined) != 0 {
		t.Fatalf("quarantined nodes must not remain in the runtime config: %+v", allQuarantined)
	}
}

func TestPreviewPortProfileReportsOnlyRenderedNodes(t *testing.T) {
	setupServiceTestDB(t)

	healthy := seedRuntimeTestNode(t, "preview-healthy-node")
	disabled := seedRuntimeTestNode(t, "preview-disabled-node")
	quarantined := seedRuntimeTestNode(t, "preview-quarantined-node")
	now := time.Now()
	if err := model.DB.Model(&model.ProxyNode{}).
		Where("id = ?", disabled.ID).
		Update("enabled", false).Error; err != nil {
		t.Fatalf("disable preview node: %v", err)
	}
	if err := model.DB.Model(&model.ProxyNode{}).
		Where("id = ?", quarantined.ID).
		Update("quarantined_at", now).Error; err != nil {
		t.Fatalf("quarantine preview node: %v", err)
	}

	preview, err := PreviewPortProfile(PortProfilePayload{
		Name:             "PREVIEW-RUNNABLE",
		ListenHost:       "127.0.0.1",
		MixedPort:        17930,
		ProxySettings:    model.DefaultPortProfileProxySettings(),
		IncludeInRuntime: true,
		NodeIDs:          []int{disabled.ID, healthy.ID, quarantined.ID},
	})
	if err != nil {
		t.Fatalf("preview port profile: %v", err)
	}
	if len(preview.NodeIDs) != 1 || preview.NodeIDs[0] != healthy.ID {
		t.Fatalf("preview node ids must match rendered nodes: %+v", preview.NodeIDs)
	}
	if len(preview.Nodes) != 1 || preview.Nodes[0].ID != healthy.ID {
		t.Fatalf("preview nodes must exclude unavailable nodes: %+v", preview.Nodes)
	}
}
