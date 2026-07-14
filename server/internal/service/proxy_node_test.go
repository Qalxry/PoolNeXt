package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"poolx/internal/model"
	"poolx/internal/pkg/common"
	kernelpkg "poolx/internal/pkg/kernel"

	"gorm.io/gorm"
)

func TestExecuteNodeTestsPersistsResult(t *testing.T) {
	setupServiceTestDB(t)

	originalRunner := runNodeKernelTest
	runNodeKernelTest = func(ctx context.Context, input kernelpkg.MihomoNodeTestInput) (*kernelpkg.MihomoNodeTestResult, error) {
		return &kernelpkg.MihomoNodeTestResult{LatencyMS: 321}, nil
	}
	t.Cleanup(func() {
		runNodeKernelTest = originalRunner
	})

	originalBinaryPath := common.MihomoBinaryPath
	common.MihomoBinaryPath = "/tmp/fake-mihomo"
	t.Cleanup(func() {
		common.MihomoBinaryPath = originalBinaryPath
	})

	node := &model.ProxyNode{
		SourceConfigID:   1,
		SourceConfigName: "seed.yaml",
		Name:             "local-node",
		Type:             "ss",
		Server:           "127.0.0.1",
		Port:             1,
		Fingerprint:      "fingerprint-local-node",
		MetadataJSON:     `{"name":"local-node"}`,
		Enabled:          true,
		LastTestStatus:   model.NodeTestStatusUnknown,
	}
	if err := model.DB.Create(node).Error; err != nil {
		t.Fatalf("seed proxy node: %v", err)
	}

	results, err := ExecuteNodeTests(context.Background(), NodeTestInput{
		NodeIDs:   []int{node.ID},
		TimeoutMS: 1000,
	})
	if err != nil {
		t.Fatalf("ExecuteNodeTests returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one test result, got %d", len(results))
	}
	if results[0].Status != model.NodeTestStatusSuccess {
		t.Fatalf("unexpected test status: %+v", results[0])
	}

	var refreshed model.ProxyNode
	if err := model.DB.First(&refreshed, "id = ?", node.ID).Error; err != nil {
		t.Fatalf("reload proxy node: %v", err)
	}
	if refreshed.LastTestStatus != model.NodeTestStatusSuccess {
		t.Fatalf("expected node status to be updated, got %s", refreshed.LastTestStatus)
	}
	if refreshed.LastLatencyMS == nil || *refreshed.LastLatencyMS != 321 {
		t.Fatalf("expected node latency to be updated, got %+v", refreshed.LastLatencyMS)
	}
}

func TestProxyNodeJSONDoesNotExposeMetadata(t *testing.T) {
	node := &model.ProxyNode{
		ID:           1,
		Name:         "private-node",
		MetadataJSON: `{"password":"do-not-expose"}`,
	}
	payload, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal proxy node: %v", err)
	}
	if strings.Contains(string(payload), "metadata_json") || strings.Contains(string(payload), "do-not-expose") {
		t.Fatalf("proxy node response exposed metadata: %s", payload)
	}
}

func TestProxyNodeListDoesNotLoadMetadata(t *testing.T) {
	setupServiceTestDB(t)
	node := seedRuntimeTestNode(t, "list-without-metadata")
	if node.MetadataJSON == "" {
		t.Fatal("test fixture must contain node metadata")
	}

	items, err := ListProxyNodes(ProxyNodeListInput{Limit: 10})
	if err != nil {
		t.Fatalf("list proxy nodes: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected proxy node count: %d", len(items))
	}
	if items[0].MetadataJSON != "" {
		t.Fatal("proxy node list loaded metadata that the compact UI does not consume")
	}
}

func TestExecuteNodeTestsPersistsFailureResult(t *testing.T) {
	setupServiceTestDB(t)

	originalRunner := runNodeKernelTest
	runNodeKernelTest = func(ctx context.Context, input kernelpkg.MihomoNodeTestInput) (*kernelpkg.MihomoNodeTestResult, error) {
		return nil, assertiveError("boom")
	}
	t.Cleanup(func() {
		runNodeKernelTest = originalRunner
	})

	originalBinaryPath := common.MihomoBinaryPath
	common.MihomoBinaryPath = "/tmp/fake-mihomo"
	t.Cleanup(func() {
		common.MihomoBinaryPath = originalBinaryPath
	})

	node := &model.ProxyNode{
		SourceConfigID:   1,
		SourceConfigName: "seed.yaml",
		Name:             "local-node",
		Type:             "ss",
		Server:           "127.0.0.1",
		Port:             1,
		Fingerprint:      "fingerprint-local-node-failed",
		MetadataJSON:     `{"name":"local-node"}`,
		Enabled:          true,
		LastTestStatus:   model.NodeTestStatusUnknown,
	}
	if err := model.DB.Create(node).Error; err != nil {
		t.Fatalf("seed proxy node: %v", err)
	}

	results, err := ExecuteNodeTests(context.Background(), NodeTestInput{
		NodeIDs: []int{node.ID},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeTests returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one test result, got %d", len(results))
	}
	if results[0].Status != model.NodeTestStatusFailed {
		t.Fatalf("unexpected test status: %+v", results[0])
	}

	var refreshed model.ProxyNode
	if err := model.DB.First(&refreshed, "id = ?", node.ID).Error; err != nil {
		t.Fatalf("reload proxy node: %v", err)
	}
	if refreshed.LastTestStatus != model.NodeTestStatusFailed {
		t.Fatalf("expected node status to be updated, got %s", refreshed.LastTestStatus)
	}
	if refreshed.LastTestError == "" {
		t.Fatal("expected node test error to be updated")
	}
}

func TestExecuteNodeTestsAlwaysRunsKernelProbe(t *testing.T) {
	setupServiceTestDB(t)

	originalRunner := runNodeKernelTest
	callCount := 0
	runNodeKernelTest = func(ctx context.Context, input kernelpkg.MihomoNodeTestInput) (*kernelpkg.MihomoNodeTestResult, error) {
		callCount++
		return &kernelpkg.MihomoNodeTestResult{LatencyMS: 999}, nil
	}
	t.Cleanup(func() {
		runNodeKernelTest = originalRunner
	})

	originalBinaryPath := common.MihomoBinaryPath
	common.MihomoBinaryPath = "/tmp/fake-mihomo"
	t.Cleanup(func() {
		common.MihomoBinaryPath = originalBinaryPath
	})

	latency := 123
	node := &model.ProxyNode{
		SourceConfigID:   1,
		SourceConfigName: "seed.yaml",
		Name:             "probe-node",
		Type:             "ss",
		Server:           "127.0.0.1",
		Port:             1,
		Fingerprint:      "fingerprint-probe-node",
		MetadataJSON:     `{"name":"probe-node"}`,
		Enabled:          true,
		LastTestStatus:   model.NodeTestStatusSuccess,
		LastLatencyMS:    &latency,
	}
	if err := model.DB.Create(node).Error; err != nil {
		t.Fatalf("seed proxy node: %v", err)
	}

	results, err := ExecuteNodeTests(context.Background(), NodeTestInput{
		NodeIDs: []int{node.ID},
	})
	if err != nil {
		t.Fatalf("ExecuteNodeTests returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one test result, got %d", len(results))
	}
	if results[0].Cached {
		t.Fatalf("expected fresh result, got %+v", results[0])
	}
	if callCount != 1 {
		t.Fatalf("expected kernel runner to execute once, got %d", callCount)
	}
}

func TestNormalizeProxyNodeTags(t *testing.T) {
	result := normalizeProxyNodeTags(" hk, premium, hk，low-latency ; premium ")
	if result != "hk, premium, low-latency" {
		t.Fatalf("unexpected normalized tags: %s", result)
	}
}

func TestNodeHealthQuarantinesAfterThresholdAndRecoversOnSuccess(t *testing.T) {
	setupServiceTestDB(t)

	originalRunner := runNodeKernelTest
	originalBinaryPath := common.MihomoBinaryPath
	originalThreshold := common.NodeHealthFailureThreshold
	common.MihomoBinaryPath = "/tmp/fake-mihomo"
	common.NodeHealthFailureThreshold = 2
	probeShouldFail := true
	runNodeKernelTest = func(ctx context.Context, input kernelpkg.MihomoNodeTestInput) (*kernelpkg.MihomoNodeTestResult, error) {
		if probeShouldFail {
			return nil, fmt.Errorf("unreachable")
		}
		return &kernelpkg.MihomoNodeTestResult{LatencyMS: 42}, nil
	}
	t.Cleanup(func() {
		runNodeKernelTest = originalRunner
		common.MihomoBinaryPath = originalBinaryPath
		common.NodeHealthFailureThreshold = originalThreshold
	})

	node := seedRuntimeTestNode(t, "health-transition-node")
	first, err := ExecuteNodeTests(context.Background(), NodeTestInput{NodeIDs: []int{node.ID}})
	if err != nil {
		t.Fatalf("first probe: %v", err)
	}
	if first[0].Quarantined || first[0].ConsecutiveFailures != 1 {
		t.Fatalf("first failure should not quarantine: %+v", first[0])
	}

	second, err := ExecuteNodeTests(context.Background(), NodeTestInput{NodeIDs: []int{node.ID}})
	if err != nil {
		t.Fatalf("second probe: %v", err)
	}
	if !second[0].Quarantined || !second[0].AvailabilityChanged || second[0].ConsecutiveFailures != 2 {
		t.Fatalf("second failure should quarantine: %+v", second[0])
	}

	probeShouldFail = false
	recovered, err := ExecuteNodeTests(context.Background(), NodeTestInput{NodeIDs: []int{node.ID}})
	if err != nil {
		t.Fatalf("recovery probe: %v", err)
	}
	if recovered[0].Quarantined || !recovered[0].AvailabilityChanged || recovered[0].ConsecutiveFailures != 0 {
		t.Fatalf("successful probe should recover node: %+v", recovered[0])
	}

	refreshed, err := model.GetProxyNodeByID(node.ID)
	if err != nil {
		t.Fatalf("reload recovered node: %v", err)
	}
	if refreshed.QuarantinedAt != nil || refreshed.ConsecutiveSuccesses != 1 {
		t.Fatalf("unexpected persisted recovery state: %+v", refreshed)
	}
	logs, err := ListAppLogs(10, 0, "system", "", "backend", "node_health.")
	if err != nil {
		t.Fatalf("list node-health transition logs: %v", err)
	}
	if len(logs.Items) != 2 || logs.Items[0].Event != "node_health.quarantined" || logs.Items[1].Event != "node_health.recovered" {
		t.Fatalf("unexpected node-health transition logs: %+v", logs.Items)
	}
}

func TestConcurrentNodeTestsRespectGlobalProbeLimit(t *testing.T) {
	setupServiceTestDB(t)

	originalRunner := runNodeKernelTest
	originalBinaryPath := common.MihomoBinaryPath
	common.MihomoBinaryPath = "/tmp/fake-mihomo"
	var active atomic.Int32
	var peak atomic.Int32
	runNodeKernelTest = func(ctx context.Context, input kernelpkg.MihomoNodeTestInput) (*kernelpkg.MihomoNodeTestResult, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := peak.Load()
			if current <= previous || peak.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		return &kernelpkg.MihomoNodeTestResult{LatencyMS: 25}, nil
	}
	t.Cleanup(func() {
		runNodeKernelTest = originalRunner
		common.MihomoBinaryPath = originalBinaryPath
	})

	groups := make([][]int, 2)
	for index := 0; index < 8; index++ {
		node := seedRuntimeTestNode(t, fmt.Sprintf("bounded-probe-%d", index))
		groups[index%2] = append(groups[index%2], node.ID)
	}

	var wg sync.WaitGroup
	errorsCh := make(chan error, len(groups))
	for _, ids := range groups {
		ids := ids
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ExecuteNodeTests(context.Background(), NodeTestInput{NodeIDs: ids})
			errorsCh <- err
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("concurrent probe failed: %v", err)
		}
	}
	if got := peak.Load(); got > maxNodeTestParallelism {
		t.Fatalf("global probe concurrency exceeded limit: got %d want <= %d", got, maxNodeTestParallelism)
	}
	if got := peak.Load(); got < 2 {
		t.Fatalf("expected probes to run concurrently, peak=%d", got)
	}
}

func TestPersistNodeTestExecutionPreservesConcurrentNodeEdits(t *testing.T) {
	setupServiceTestDB(t)

	node := seedRuntimeTestNode(t, "health-preserves-concurrent-edits")
	callbackName := "test:edit-node-after-health-read"
	var once sync.Once
	if err := model.DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "proxy_nodes" {
			return
		}
		once.Do(func() {
			if err := model.DB.Model(&model.ProxyNode{}).
				Where("id = ?", node.ID).
				Updates(map[string]any{"enabled": false, "tags": "edited-concurrently"}).Error; err != nil {
				t.Errorf("apply concurrent node edit: %v", err)
			}
		})
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = model.DB.Callback().Query().Remove(callbackName)
	})

	finishedAt := time.Now()
	latency := 31
	if _, err := persistNodeTestExecution(node.ID, NodeTestExecution{
		NodeID:       node.ID,
		NodeName:     node.Name,
		Status:       model.NodeTestStatusSuccess,
		LatencyMS:    &latency,
		FinishedAt:   finishedAt,
		LastTestedAt: &finishedAt,
	}); err != nil {
		t.Fatalf("persist node health result: %v", err)
	}

	refreshed, err := model.GetProxyNodeByID(node.ID)
	if err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if refreshed.Enabled || refreshed.Tags != "edited-concurrently" {
		t.Fatalf("health persistence overwrote concurrent node edits: %+v", refreshed)
	}
	if refreshed.LastTestStatus != model.NodeTestStatusSuccess || refreshed.LastLatencyMS == nil || *refreshed.LastLatencyMS != latency {
		t.Fatalf("health fields were not persisted: %+v", refreshed)
	}
}

func TestPersistNodeTestExecutionDoesNotResurrectConcurrentlyDeletedNode(t *testing.T) {
	setupServiceTestDB(t)

	node := seedRuntimeTestNode(t, "health-does-not-resurrect-node")
	callbackName := "test:delete-node-after-health-read"
	var once sync.Once
	if err := model.DB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "proxy_nodes" {
			return
		}
		once.Do(func() {
			if err := model.DB.Delete(&model.ProxyNode{}, "id = ?", node.ID).Error; err != nil {
				t.Errorf("delete node concurrently: %v", err)
			}
		})
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = model.DB.Callback().Query().Remove(callbackName)
	})

	finishedAt := time.Now()
	_, err := persistNodeTestExecution(node.ID, NodeTestExecution{
		NodeID:       node.ID,
		NodeName:     node.Name,
		Status:       model.NodeTestStatusFailed,
		ErrorMessage: "unreachable",
		FinishedAt:   finishedAt,
		LastTestedAt: &finishedAt,
	})
	if err == nil {
		t.Fatal("expected persistence to report the concurrently deleted node")
	}

	var count int64
	if err := model.DB.Model(&model.ProxyNode{}).Where("id = ?", node.ID).Count(&count).Error; err != nil {
		t.Fatalf("count deleted node: %v", err)
	}
	if count != 0 {
		t.Fatalf("health persistence resurrected deleted node %d", node.ID)
	}
}

func TestNodeHealthSummaryCountsOnlyEnabledPoolHealth(t *testing.T) {
	setupServiceTestDB(t)

	enabledHealthy := seedRuntimeTestNode(t, "summary-enabled-healthy")
	disabledHealthy := seedRuntimeTestNode(t, "summary-disabled-healthy")
	enabledQuarantined := seedRuntimeTestNode(t, "summary-enabled-quarantined")
	now := time.Now()

	if err := model.DB.Model(&model.ProxyNode{}).
		Where("id = ?", enabledHealthy.ID).
		Updates(map[string]any{
			"last_test_status": model.NodeTestStatusSuccess,
			"last_tested_at":   now,
		}).Error; err != nil {
		t.Fatalf("mark enabled node healthy: %v", err)
	}
	if err := model.DB.Model(&model.ProxyNode{}).
		Where("id = ?", disabledHealthy.ID).
		Updates(map[string]any{
			"enabled":          false,
			"last_test_status": model.NodeTestStatusSuccess,
			"last_tested_at":   now,
		}).Error; err != nil {
		t.Fatalf("mark disabled node healthy: %v", err)
	}
	if err := model.DB.Model(&model.ProxyNode{}).
		Where("id = ?", enabledQuarantined.ID).
		Updates(map[string]any{
			"last_test_status": model.NodeTestStatusFailed,
			"last_tested_at":   now,
			"quarantined_at":   now,
		}).Error; err != nil {
		t.Fatalf("mark enabled node quarantined: %v", err)
	}

	summary, err := GetNodeHealthSummary()
	if err != nil {
		t.Fatalf("get node health summary: %v", err)
	}
	if summary.Total != 3 || summary.Enabled != 2 || summary.Disabled != 1 {
		t.Fatalf("unexpected pool totals: %+v", summary)
	}
	if summary.Healthy != 1 || summary.Failed != 1 || summary.Unknown != 0 || summary.Quarantined != 1 {
		t.Fatalf("disabled nodes must not contribute to enabled-pool health: %+v", summary)
	}
}

func TestDeleteProxyNodeCleansProfileRelationsAndRuntimeSnapshot(t *testing.T) {
	setupServiceTestDB(t)

	node := seedRuntimeTestNode(t, "delete-related-node")
	profile, err := CreatePortProfile(PortProfilePayload{
		Name:             "DELETE-RELATED",
		ListenHost:       "127.0.0.1",
		MixedPort:        17920,
		ProxySettings:    model.DefaultPortProfileProxySettings(),
		IncludeInRuntime: true,
		NodeIDs:          []int{node.ID},
	})
	if err != nil {
		t.Fatalf("create related profile: %v", err)
	}
	if _, err := SaveRuntimePreview(profile.Profile.ID); err != nil {
		t.Fatalf("save runtime snapshot: %v", err)
	}

	if err := DeleteProxyNode(node.ID); err != nil {
		t.Fatalf("delete proxy node: %v", err)
	}

	var relationCount int64
	if err := model.DB.Model(&model.PortProfileNode{}).
		Where("proxy_node_id = ?", node.ID).
		Count(&relationCount).Error; err != nil {
		t.Fatalf("count remaining relations: %v", err)
	}
	if relationCount != 0 {
		t.Fatalf("expected node relations to be deleted, got %d", relationCount)
	}

	var snapshotCount int64
	if err := model.DB.Model(&model.RuntimeConfig{}).
		Where("port_profile_id = ?", profile.Profile.ID).
		Count(&snapshotCount).Error; err != nil {
		t.Fatalf("count remaining runtime snapshots: %v", err)
	}
	if snapshotCount != 0 {
		t.Fatalf("expected stale runtime snapshots to be deleted, got %d", snapshotCount)
	}

	refreshedProfile, err := model.GetPortProfileByID(profile.Profile.ID)
	if err != nil {
		t.Fatalf("reload related profile: %v", err)
	}
	if refreshedProfile.IncludeInRuntime {
		t.Fatal("expected an empty profile to be removed from the final runtime configuration")
	}
}

type assertiveError string

func (e assertiveError) Error() string {
	return string(e)
}
