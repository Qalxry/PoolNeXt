package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"sync"
	"time"
)

const nodeHealthLockCount = 64

var nodeHealthLocks [nodeHealthLockCount]sync.Mutex

type NodeHealthSummary struct {
	Total        int64      `json:"total"`
	Enabled      int64      `json:"enabled"`
	Disabled     int64      `json:"disabled"`
	Healthy      int64      `json:"healthy"`
	Failed       int64      `json:"failed"`
	Unknown      int64      `json:"unknown"`
	Quarantined  int64      `json:"quarantined"`
	LastTestedAt *time.Time `json:"last_tested_at,omitempty"`
}

type NodeHealthRunResult struct {
	Checked     int                 `json:"checked"`
	Succeeded   int                 `json:"succeeded"`
	Failed      int                 `json:"failed"`
	Quarantined int                 `json:"quarantined"`
	Results     []NodeTestExecution `json:"results"`
}

type nodeHealthOptionSnapshot struct {
	Enabled          bool
	Interval         time.Duration
	Concurrency      int
	FailureThreshold int
}

func GetNodeHealthSummary() (*NodeHealthSummary, error) {
	summary := &NodeHealthSummary{}
	queries := []struct {
		destination *int64
		where       string
		args        []any
	}{
		{destination: &summary.Total},
		{destination: &summary.Enabled, where: "enabled = ?", args: []any{true}},
		{destination: &summary.Disabled, where: "enabled = ?", args: []any{false}},
		{destination: &summary.Healthy, where: "enabled = ? AND last_test_status = ? AND quarantined_at IS NULL", args: []any{true, model.NodeTestStatusSuccess}},
		{destination: &summary.Failed, where: "enabled = ? AND last_test_status = ?", args: []any{true, model.NodeTestStatusFailed}},
		{destination: &summary.Unknown, where: "enabled = ? AND last_test_status = ?", args: []any{true, model.NodeTestStatusUnknown}},
		{destination: &summary.Quarantined, where: "enabled = ? AND quarantined_at IS NOT NULL", args: []any{true}},
	}
	for _, item := range queries {
		query := model.DB.Model(&model.ProxyNode{})
		if item.where != "" {
			query = query.Where(item.where, item.args...)
		}
		if err := query.Count(item.destination).Error; err != nil {
			return nil, err
		}
	}

	var latest model.ProxyNode
	if err := model.DB.Where("last_tested_at IS NOT NULL").Order("last_tested_at desc").Limit(1).Find(&latest).Error; err != nil {
		return nil, err
	}
	summary.LastTestedAt = latest.LastTestedAt
	return summary, nil
}

func RunNodeHealthCheck(ctx context.Context) (*NodeHealthRunResult, error) {
	ids, err := model.ListEnabledProxyNodeIDs()
	if err != nil {
		return nil, err
	}
	result := &NodeHealthRunResult{Results: []NodeTestExecution{}}
	if len(ids) == 0 {
		return result, nil
	}
	options := snapshotNodeHealthOptions()
	executions, err := executeNodeTests(ctx, NodeTestInput{NodeIDs: ids}, options.Concurrency)
	if err != nil {
		return nil, err
	}
	result.Results = executions
	result.Checked = len(executions)
	for _, execution := range executions {
		switch execution.Status {
		case model.NodeTestStatusSuccess:
			result.Succeeded++
		case model.NodeTestStatusFailed:
			result.Failed++
		}
		if execution.Quarantined {
			result.Quarantined++
		}
	}
	return result, nil
}

func RunNodeHealthScheduler(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	firstRun := true
	for {
		options := snapshotNodeHealthOptions()
		delay := options.Interval
		if !options.Enabled {
			delay = 30 * time.Second
		} else if firstRun && delay > 10*time.Second {
			delay = 10 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		firstRun = false
		if !options.Enabled {
			continue
		}

		result, err := RunNodeHealthCheck(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("automatic proxy node health check failed", "error", err)
			_ = AppLog.PushEvent(AppLogEventInput{
				Classification: model.AppLogClassificationSystem,
				Level:          model.AppLogLevelWarn,
				Event:          "node_health.scheduled_run_failed",
				Source:         "scheduler",
				Message:        "自动节点探测失败",
				Details:        map[string]string{"reason": err.Error()},
			})
			continue
		}
		if result.Checked > 0 {
			_ = AppLog.PushEvent(AppLogEventInput{
				Classification: model.AppLogClassificationSystem,
				Level:          model.AppLogLevelInfo,
				Event:          "node_health.scheduled_run_completed",
				Source:         "scheduler",
				Message:        "自动节点探测完成",
				Details: map[string]string{
					"checked":     fmt.Sprint(result.Checked),
					"succeeded":   fmt.Sprint(result.Succeeded),
					"failed":      fmt.Sprint(result.Failed),
					"quarantined": fmt.Sprint(result.Quarantined),
				},
			})
		}
	}
}

func persistNodeTestExecution(nodeID int, execution NodeTestExecution) (NodeTestExecution, error) {
	if execution.Status != model.NodeTestStatusSuccess && execution.Status != model.NodeTestStatusFailed {
		return execution, nil
	}
	lock := &nodeHealthLocks[nodeID%nodeHealthLockCount]
	lock.Lock()
	defer lock.Unlock()

	node, err := model.GetProxyNodeByID(nodeID)
	if err != nil {
		return execution, err
	}
	wasQuarantined := node.QuarantinedAt != nil
	threshold := snapshotNodeHealthOptions().FailureThreshold
	if execution.Status == model.NodeTestStatusSuccess {
		node.ConsecutiveFailures = 0
		node.ConsecutiveSuccesses = incrementHealthStreak(node.ConsecutiveSuccesses)
		node.QuarantinedAt = nil
	} else {
		node.ConsecutiveFailures = incrementHealthStreak(node.ConsecutiveFailures)
		node.ConsecutiveSuccesses = 0
		if node.ConsecutiveFailures >= threshold && node.QuarantinedAt == nil {
			now := execution.FinishedAt
			node.QuarantinedAt = &now
		}
	}
	node.LastTestStatus = execution.Status
	node.LastLatencyMS = execution.LatencyMS
	node.LastTestError = execution.ErrorMessage
	node.LastTestedAt = execution.LastTestedAt
	result := model.DB.Model(&model.ProxyNode{}).Where("id = ?", nodeID).Updates(map[string]any{
		"last_test_status":      node.LastTestStatus,
		"last_latency_ms":       node.LastLatencyMS,
		"last_test_error":       node.LastTestError,
		"last_tested_at":        node.LastTestedAt,
		"consecutive_failures":  node.ConsecutiveFailures,
		"consecutive_successes": node.ConsecutiveSuccesses,
		"quarantined_at":        node.QuarantinedAt,
	})
	if result.Error != nil {
		return execution, result.Error
	}
	if result.RowsAffected == 0 {
		return execution, fmt.Errorf("节点不存在")
	}

	execution.ConsecutiveFailures = node.ConsecutiveFailures
	execution.ConsecutiveSuccesses = node.ConsecutiveSuccesses
	execution.Quarantined = node.QuarantinedAt != nil
	execution.AvailabilityChanged = wasQuarantined != execution.Quarantined
	if execution.AvailabilityChanged {
		event := "node_health.recovered"
		level := model.AppLogLevelInfo
		message := "节点已恢复并重新加入运行配置"
		if execution.Quarantined {
			event = "node_health.quarantined"
			level = model.AppLogLevelWarn
			message = "节点连续探测失败，已移出运行配置"
		}
		details := map[string]string{
			"node_id":              fmt.Sprint(node.ID),
			"node_name":            node.Name,
			"consecutive_failures": fmt.Sprint(node.ConsecutiveFailures),
		}
		if execution.LatencyMS != nil {
			details["latency_ms"] = fmt.Sprint(*execution.LatencyMS)
		}
		if execution.ErrorMessage != "" {
			details["reason"] = execution.ErrorMessage
		}
		_ = AppLog.PushEvent(AppLogEventInput{
			Classification: model.AppLogClassificationSystem,
			Level:          level,
			Event:          event,
			Source:         "backend",
			Message:        message,
			Details:        details,
		})
	}
	return execution, nil
}

func reloadRuntimeAfterNodeHealthChanges(results []NodeTestExecution) {
	changed := runtimeRegistry.shouldResumeAfterNodeHealthRecovery()
	for _, result := range results {
		if result.AvailabilityChanged {
			changed = true
			break
		}
	}
	if !changed {
		return
	}
	if err := reconcileRuntimeAfterNodeChange(
		"节点健康状态变化",
		"node_health.runtime_reload_failed",
		"scheduler",
	); err != nil {
		slog.Warn("reload Mihomo after proxy node health change failed", "error", err)
	}
}

func reloadRuntimeAfterNodeSetChange(reason string) error {
	return reconcileRuntimeAfterNodeChange(
		reason,
		"proxy_nodes.runtime_reload_failed",
		"backend",
	)
}

func reconcileRuntimeAfterNodeChange(reason string, reloadFailureEvent string, source string) error {
	running := runtimeRegistry.activeProcess() != nil
	resumePending := runtimeRegistry.shouldResumeAfterNodeHealthRecovery()
	if !running && !resumePending {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hasRunnableProfile, err := hasRunnableRuntimeProfile()
	if err != nil {
		return fmt.Errorf("%s已保存，但检查可运行端口配置失败: %w", reason, err)
	}
	if !hasRunnableProfile {
		if running {
			if _, err := stopRuntime(ctx, true); err != nil {
				return fmt.Errorf("%s已保存，但停止无可用节点的 Mihomo 失败: %w", reason, err)
			}
			if !runtimeRegistry.shouldResumeAfterNodeHealthRecovery() {
				return nil
			}
			_ = AppLog.PushEvent(AppLogEventInput{
				Classification: model.AppLogClassificationSystem,
				Level:          model.AppLogLevelWarn,
				Event:          "mihomo.stopped_no_runnable_nodes",
				Source:         "mihomo",
				Message:        "Mihomo 已停止：最终配置中没有可运行节点",
				Details:        map[string]string{"change": reason},
			})
		}
		return nil
	}
	if !running {
		status, err := startRuntime(ctx, true, "node_health_recovery")
		if err != nil {
			if errors.Is(err, errRuntimeRecoveryCanceled) {
				return nil
			}
			return fmt.Errorf("节点已恢复，但重新启动 Mihomo 失败: %w", err)
		}
		_ = AppLog.PushEvent(AppLogEventInput{
			Classification: model.AppLogClassificationSystem,
			Level:          model.AppLogLevelInfo,
			Event:          "mihomo.resumed_after_node_recovery",
			Source:         "scheduler",
			Message:        "节点恢复后 Mihomo 已自动重新启动",
			Details:        map[string]string{"listener_count": fmt.Sprint(status.ListenerCount)},
		})
		return nil
	}
	if _, err := ReloadRuntime(ctx); err != nil {
		slog.Warn("reload Mihomo after proxy node set change failed", "reason", reason, "error", err)
		_ = AppLog.PushEvent(AppLogEventInput{
			Classification: model.AppLogClassificationSystem,
			Level:          model.AppLogLevelWarn,
			Event:          reloadFailureEvent,
			Source:         source,
			Message:        "节点集合已保存，但 Mihomo 热重载失败",
			Details: map[string]string{
				"change": reason,
				"reason": err.Error(),
			},
		})
		return fmt.Errorf("%s已保存，但 Mihomo 热重载失败: %w", reason, err)
	}
	return nil
}

func hasRunnableRuntimeProfile() (bool, error) {
	profiles, err := ListPortProfiles()
	if err != nil {
		return false, err
	}
	for _, profile := range profiles {
		if profile.Profile.IncludeInRuntime && len(selectRuntimeNodes(profile.Nodes)) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func snapshotNodeHealthOptions() nodeHealthOptionSnapshot {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	intervalSeconds := common.NodeHealthCheckIntervalSeconds
	if intervalSeconds < 30 {
		intervalSeconds = common.DefaultNodeHealthCheckIntervalSeconds
	}
	concurrency := common.NodeHealthCheckConcurrency
	if concurrency < 1 || concurrency > maxNodeTestParallelism {
		concurrency = common.DefaultNodeHealthCheckConcurrency
	}
	threshold := common.NodeHealthFailureThreshold
	if threshold < 1 || threshold > 10 {
		threshold = common.DefaultNodeHealthFailureThreshold
	}
	return nodeHealthOptionSnapshot{
		Enabled:          common.NodeHealthCheckEnabled,
		Interval:         time.Duration(intervalSeconds) * time.Second,
		Concurrency:      concurrency,
		FailureThreshold: threshold,
	}
}

func incrementHealthStreak(value int) int {
	if value >= 1_000_000 {
		return value
	}
	return value + 1
}
