package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	kernelpkg "poolx/internal/pkg/kernel"
	"poolx/internal/pkg/runtimeconfig"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	runtimeLogLimit              = 400
	runtimeLogMessageLimit       = 16 * 1024
	runtimeLogTruncatedIndicator = " … [truncated]"
	runtimeConfigFileName        = "config.yaml"
	runtimeConfigFileMode        = 0o600
	runtimeWorkDirMode           = 0o700
)

var (
	errNoRunnableProfiles      = errors.New("当前没有包含可用节点的端口配置，无法启动 Mihomo")
	errRuntimeRecoveryCanceled = errors.New("节点恢复后的自动启动已被手动停止取消")
)

type RuntimeLogEntry struct {
	Seq       int64     `json:"seq"`
	Stream    string    `json:"stream"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type RuntimeLogList struct {
	Items []*RuntimeLogEntry `json:"items"`
}

type RuntimeStatus struct {
	Instance         *model.KernelInstance           `json:"instance"`
	Running          bool                            `json:"running"`
	AutoStartEnabled bool                            `json:"auto_start_enabled"`
	APIHealthy       bool                            `json:"api_healthy"`
	APIVersion       string                          `json:"api_version,omitempty"`
	ProfileCount     int                             `json:"profile_count"`
	ListenerCount    int                             `json:"listener_count"`
	Listeners        []runtimeconfig.RuntimeListener `json:"listeners"`
	ConfigPending    bool                            `json:"config_pending"`
	ConfigValid      bool                            `json:"config_valid"`
	ConfigError      string                          `json:"config_error,omitempty"`
}

type runtimeOptionSnapshot struct {
	KernelType        string
	BinaryPath        string
	AllowLAN          bool
	ControllerAddress string
	ControllerSecret  string
	Mode              string
	AutoStartEnabled  bool
}

type runtimeProcessRegistry struct {
	operationMu                   sync.Mutex
	mu                            sync.Mutex
	process                       *runtimeProcess
	generation                    uint64
	logSeq                        int64
	logs                          []*RuntimeLogEntry
	startedAt                     time.Time
	resumeAfterNodeHealthRecovery bool
}

type runtimeProcess struct {
	cmd               *exec.Cmd
	generation        uint64
	kernelType        string
	controllerAddress string
	controllerSecret  string
	stdout            *runtimeLogWriter
	stderr            *runtimeLogWriter
	done              chan struct{}
	settled           chan struct{}
	waitErr           error
}

type runtimeLogWriter struct {
	mu        sync.Mutex
	registry  *runtimeProcessRegistry
	stream    string
	pending   []byte
	truncated bool
}

var runtimeRegistry = &runtimeProcessRegistry{}
var startMihomoProcess = kernelpkg.StartMihomoProcess
var waitForMihomoControllerReady = kernelpkg.WaitForControllerReady
var reloadMihomoConfig = kernelpkg.ReloadMihomoConfig
var getMihomoVersion = kernelpkg.GetMihomoVersion
var terminateMihomoProcess = terminateProcess
var saveRuntimeInstance = func(instance *model.KernelInstance) error {
	return model.DB.Save(instance).Error
}
var runtimeDataRoot = common.RuntimeDataRoot

func StartRuntime(ctx context.Context) (*RuntimeStatus, error) {
	return startRuntime(ctx, false, "start")
}

func AutoStartRuntime(ctx context.Context) error {
	options := snapshotRuntimeOptions()
	if !options.AutoStartEnabled {
		return nil
	}

	status, err := startRuntime(ctx, true, "auto_start")
	if err != nil {
		if errors.Is(err, errNoRunnableProfiles) {
			runtimeRegistry.setResumeAfterNodeHealthRecovery(true)
		}
		_ = AppLog.PushEvent(AppLogEventInput{
			Classification: model.AppLogClassificationSystem,
			Level:          model.AppLogLevelError,
			Event:          "mihomo.auto_start_failed",
			Source:         "mihomo",
			Message:        "Mihomo 自动启动失败",
			Details:        map[string]string{"reason": err.Error()},
		})
		return err
	}
	_ = AppLog.PushEvent(AppLogEventInput{
		Classification: model.AppLogClassificationSystem,
		Level:          model.AppLogLevelInfo,
		Event:          "mihomo.auto_started",
		Source:         "mihomo",
		Message:        "Mihomo 自动启动完成",
		Details:        map[string]string{"listener_count": fmt.Sprint(status.ListenerCount)},
	})
	return nil
}

func startRuntime(ctx context.Context, allowAlreadyRunning bool, action string) (*RuntimeStatus, error) {
	runtimeRegistry.operationMu.Lock()
	defer runtimeRegistry.operationMu.Unlock()
	if action == "node_health_recovery" && !runtimeRegistry.shouldResumeAfterNodeHealthRecovery() {
		return nil, errRuntimeRecoveryCanceled
	}

	if _, err := model.EnsureClashSecret(); err != nil {
		return nil, fmt.Errorf("初始化 Mihomo 控制密钥失败: %w", err)
	}
	options := snapshotRuntimeOptions()
	if options.KernelType != KernelTypeMihomo {
		return nil, fmt.Errorf("当前仅支持启动 Mihomo")
	}
	if options.BinaryPath == "" {
		return nil, fmt.Errorf("请先在系统设置中完成 Mihomo 二进制安装或路径校验")
	}

	if process := runtimeRegistry.currentProcess(); process != nil {
		if _, exited := process.waitResultIfDone(); !exited {
			if allowAlreadyRunning {
				return GetRuntimeStatus(ctx)
			}
			return nil, fmt.Errorf("Mihomo 已在运行中")
		}
		runtimeRegistry.detachProcess(process)
	}

	instance, err := ensureKernelInstanceFor(options)
	if err != nil {
		return nil, err
	}
	if instance.PID != nil {
		return nil, fmt.Errorf("检测到未由当前 PoolNeXt 进程托管的历史 PID %d；请先执行停止操作以安全清理状态", *instance.PID)
	}

	rendered, workDir, configPath, err := buildFinalRuntimeConfig(options, true)
	if err != nil {
		return nil, recordRuntimeFailure(instance, action, err, nil)
	}
	if err := writeRuntimeConfig(configPath, []byte(rendered.Content)); err != nil {
		return nil, recordRuntimeFailure(instance, action, fmt.Errorf("写入运行配置失败: %w", err), nil)
	}
	if err := ensureRuntimeListenersAvailable(rendered.Listeners); err != nil {
		return nil, recordRuntimeFailure(instance, action, err, nil)
	}

	now := time.Now()
	instance.WorkDir = workDir
	instance.ConfigPath = configPath
	instance.ControllerAddress = options.ControllerAddress
	instance.ControllerSecret = options.ControllerSecret
	instance.ActiveConfigChecksum = rendered.Checksum
	instance.ActiveProfileCount = rendered.ProfileCount
	instance.ActiveListenerCount = rendered.ListenerCount
	instance.LastAction = action
	instance.LastError = ""
	instance.LastStartedAt = &now
	instance.Status = model.KernelInstanceStatusStarting
	if err := saveRuntimeInstance(instance); err != nil {
		return nil, fmt.Errorf("保存 Mihomo 启动状态失败: %w", err)
	}

	stdoutWriter := runtimeRegistry.newLogWriter("stdout")
	stderrWriter := runtimeRegistry.newLogWriter("stderr")
	cmd, err := startMihomoProcess(options.BinaryPath, workDir, configPath, stdoutWriter, stderrWriter)
	if err != nil {
		startErr := fmt.Errorf("启动 Mihomo 失败: %w", err)
		return nil, recordRuntimeFailure(instance, action, startErr, nil)
	}
	pid := cmd.Process.Pid
	instance.PID = &pid
	process := runtimeRegistry.registerProcess(cmd, stdoutWriter, stderrWriter, now, options)
	go runtimeRegistry.waitProcessExit(process)
	if err := saveRuntimeInstance(instance); err != nil {
		return nil, cleanupStartedRuntime(instance, process, fmt.Errorf("保存 Mihomo PID 失败: %w", err))
	}

	readyCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := waitForMihomoControllerReady(readyCtx, options.ControllerAddress, options.ControllerSecret); err != nil {
		waitErr := formatRuntimeStartWaitError(err)
		return nil, cleanupStartedRuntime(instance, process, waitErr)
	}
	if waitErr, exited := process.waitResultIfDone(); exited {
		if waitErr == nil {
			waitErr = errors.New("Mihomo 进程在启动期间提前退出")
		}
		startErr := formatRuntimeStartWaitError(waitErr)
		return nil, cleanupStartedRuntime(instance, process, startErr)
	}

	instance.Status = model.KernelInstanceStatusRunning
	instance.LastError = ""
	if err := saveRuntimeInstance(instance); err != nil {
		return nil, cleanupStartedRuntime(instance, process, fmt.Errorf("保存 Mihomo 运行状态失败: %w", err))
	}
	connectionRegistry.start(process)
	runtimeRegistry.setResumeAfterNodeHealthRecovery(false)
	_ = AppLog.PushEvent(AppLogEventInput{
		Classification: model.AppLogClassificationBusiness,
		Level:          model.AppLogLevelInfo,
		Event:          "mihomo.started",
		Source:         "mihomo",
		Message:        "Mihomo 已启动",
		Details:        map[string]string{"listener_count": fmt.Sprint(rendered.ListenerCount)},
	})
	return GetRuntimeStatus(ctx)
}

func StopRuntime(ctx context.Context) (*RuntimeStatus, error) {
	return stopRuntime(ctx, false)
}

func stopRuntime(ctx context.Context, resumeAfterNodeHealthRecovery bool) (*RuntimeStatus, error) {
	runtimeRegistry.operationMu.Lock()
	defer runtimeRegistry.operationMu.Unlock()
	if !resumeAfterNodeHealthRecovery {
		runtimeRegistry.setResumeAfterNodeHealthRecovery(false)
	}

	options := snapshotRuntimeOptions()
	instance, err := ensureKernelInstanceFor(options)
	if err != nil {
		return nil, err
	}
	process := runtimeRegistry.currentProcess()
	if process != nil {
		if _, exited := process.waitResultIfDone(); exited {
			runtimeRegistry.detachProcess(process)
			process = nil
			instance.PID = nil
		}
	}
	if process == nil && instance.PID != nil {
		stalePID := *instance.PID
		staleErr := fmt.Errorf("拒绝向未由当前 PoolNeXt 进程托管的历史 PID %d 发送信号；仅清理数据库中的陈旧引用", stalePID)
		instance.Status = model.KernelInstanceStatusError
		instance.LastAction = "stop"
		instance.LastError = staleErr.Error()
		instance.PID = nil
		if saveErr := saveRuntimeInstance(instance); saveErr != nil {
			return nil, errors.Join(staleErr, fmt.Errorf("保存陈旧 PID 清理状态失败: %w", saveErr))
		}
		return nil, staleErr
	}

	instance.Status = model.KernelInstanceStatusStopping
	instance.LastAction = "stop"
	instance.LastError = ""
	if err := saveRuntimeInstance(instance); err != nil {
		return nil, fmt.Errorf("保存 Mihomo 停止状态失败: %w", err)
	}

	if process != nil {
		if _, exited := process.waitResultIfDone(); !exited {
			if err := terminateMihomoProcess(process); err != nil {
				instance.Status = model.KernelInstanceStatusError
				instance.LastError = fmt.Sprintf("停止 Mihomo 失败，进程仍由 PoolNeXt 监管: %v", err)
				saveErr := saveRuntimeInstance(instance)
				stopErr := fmt.Errorf("停止 Mihomo 失败: %w", err)
				if saveErr != nil {
					return nil, errors.Join(stopErr, fmt.Errorf("保存停止失败状态失败: %w", saveErr))
				}
				return nil, stopErr
			}
			if resumeAfterNodeHealthRecovery {
				runtimeRegistry.setResumeAfterNodeHealthRecovery(true)
			}
		}
	}
	connectionRegistry.stop()

	now := time.Now()
	instance.Status = model.KernelInstanceStatusStopped
	instance.PID = nil
	instance.LastStoppedAt = &now
	instance.LastError = ""
	if err := saveRuntimeInstance(instance); err != nil {
		if process != nil {
			runtimeRegistry.detachProcess(process)
		}
		return nil, fmt.Errorf("保存 Mihomo 已停止状态失败: %w", err)
	}

	if process != nil {
		runtimeRegistry.detachProcess(process)
	}

	_ = AppLog.PushEvent(AppLogEventInput{
		Classification: model.AppLogClassificationBusiness,
		Level:          model.AppLogLevelInfo,
		Event:          "mihomo.stopped",
		Source:         "mihomo",
		Message:        "Mihomo 已停止",
	})
	return GetRuntimeStatus(ctx)
}

func ReloadRuntime(ctx context.Context) (*RuntimeStatus, error) {
	runtimeRegistry.operationMu.Lock()
	defer runtimeRegistry.operationMu.Unlock()

	options := snapshotRuntimeOptions()
	instance, err := ensureKernelInstanceFor(options)
	if err != nil {
		return nil, err
	}
	process := runtimeRegistry.activeProcess()
	if process == nil {
		if instance.PID != nil {
			return nil, fmt.Errorf("Mihomo PID %d 不属于当前 PoolNeXt 进程，拒绝对未托管进程执行热重载", *instance.PID)
		}
		return nil, fmt.Errorf("Mihomo 当前未由 PoolNeXt 运行，无法热重载")
	}

	rendered, workDir, configPath, err := buildFinalRuntimeConfig(options, true)
	if err != nil {
		return nil, err
	}
	if err := ensureRuntimeWorkDir(workDir); err != nil {
		return nil, fmt.Errorf("准备运行目录失败: %w", err)
	}
	if strings.TrimSpace(instance.ActiveConfigChecksum) == rendered.Checksum {
		if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
			if err := writeRuntimeConfig(configPath, []byte(rendered.Content)); err != nil {
				return nil, fmt.Errorf("恢复缺失的运行配置失败: %w", err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("检查运行配置失败: %w", err)
		} else if err := os.Chmod(configPath, runtimeConfigFileMode); err != nil {
			return nil, fmt.Errorf("收紧运行配置权限失败: %w", err)
		}
		return GetRuntimeStatus(ctx)
	}
	previous, existed, err := readRuntimeConfigBackup(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取现有运行配置失败: %w", err)
	}
	if err := writeRuntimeConfig(configPath, []byte(rendered.Content)); err != nil {
		return nil, fmt.Errorf("写入运行配置失败: %w", err)
	}

	instance.Status = model.KernelInstanceStatusReloading
	instance.LastAction = "reload"
	instance.LastError = ""
	instance.WorkDir = workDir
	instance.ConfigPath = configPath
	instance.ControllerAddress = process.controllerAddress
	instance.ControllerSecret = process.controllerSecret
	if err := saveRuntimeInstance(instance); err != nil {
		restoreErr := restoreRuntimeConfig(configPath, previous, existed)
		return nil, errors.Join(
			fmt.Errorf("保存 Mihomo 热重载状态失败: %w", err),
			wrapOptionalError("恢复原运行配置失败", restoreErr),
		)
	}

	reloadCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// Mihomo treats an empty path as the active -f config path. A non-empty path
	// must be absolute and pass its safe-path policy.
	if err := reloadMihomoConfig(reloadCtx, process.controllerAddress, process.controllerSecret, ""); err != nil {
		restoreErr := restoreRuntimeConfig(configPath, previous, existed)
		instance.Status = model.KernelInstanceStatusError
		instance.LastError = fmt.Sprintf("热重载失败: %v", err)
		saveErr := saveRuntimeInstance(instance)
		return nil, errors.Join(
			fmt.Errorf("热重载失败: %w", err),
			wrapOptionalError("恢复原运行配置失败", restoreErr),
			wrapOptionalError("保存热重载失败状态失败", saveErr),
		)
	}

	now := time.Now()
	instance.Status = model.KernelInstanceStatusRunning
	instance.ActiveConfigChecksum = rendered.Checksum
	instance.ActiveProfileCount = rendered.ProfileCount
	instance.ActiveListenerCount = rendered.ListenerCount
	instance.LastReloadedAt = &now
	instance.LastError = ""
	if err := saveRuntimeInstance(instance); err != nil {
		return nil, fmt.Errorf("Mihomo 已完成热重载但保存最终状态失败，进程仍由 PoolNeXt 监管: %w", err)
	}
	_ = AppLog.PushEvent(AppLogEventInput{
		Classification: model.AppLogClassificationBusiness,
		Level:          model.AppLogLevelInfo,
		Event:          "mihomo.reloaded",
		Source:         "mihomo",
		Message:        "Mihomo 已热重载",
		Details:        map[string]string{"listener_count": fmt.Sprint(rendered.ListenerCount)},
	})
	return GetRuntimeStatus(ctx)
}

func GetRuntimeStatus(ctx context.Context) (*RuntimeStatus, error) {
	options := snapshotRuntimeOptions()
	instance, err := ensureKernelInstanceFor(options)
	if err != nil {
		return nil, err
	}
	rendered, _, _, renderErr := buildFinalRuntimeConfig(options, false)

	apiHealthy := false
	apiVersion := ""
	process := runtimeRegistry.activeProcess()
	if process != nil && process.controllerAddress != "" && process.controllerSecret != "" {
		version, err := getMihomoVersion(ctx, process.controllerAddress, process.controllerSecret)
		if err == nil {
			apiHealthy = true
			apiVersion = version
		}
	}

	status := &RuntimeStatus{
		Instance:         instance,
		Running:          process != nil,
		AutoStartEnabled: options.AutoStartEnabled,
		APIHealthy:       apiHealthy,
		APIVersion:       apiVersion,
		ConfigValid:      renderErr == nil,
	}
	if renderErr == nil {
		status.ProfileCount = rendered.ProfileCount
		status.ListenerCount = rendered.ListenerCount
		status.Listeners = rendered.Listeners
		status.ConfigPending = process != nil && instance.ActiveConfigChecksum != "" && instance.ActiveConfigChecksum != rendered.Checksum
	} else {
		status.ConfigError = renderErr.Error()
	}
	return status, nil
}

func GetRuntimeLogs(afterSeq int64, limit int) (*RuntimeLogList, error) {
	runtimeRegistry.mu.Lock()
	defer runtimeRegistry.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}
	if limit > 300 {
		limit = 300
	}
	items := make([]*RuntimeLogEntry, 0, limit)
	for _, item := range runtimeRegistry.logs {
		if item.Seq <= afterSeq {
			continue
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	return &RuntimeLogList{Items: items}, nil
}

func ensureKernelInstance() (*model.KernelInstance, error) {
	return ensureKernelInstanceFor(snapshotRuntimeOptions())
}

func ensureKernelInstanceFor(options runtimeOptionSnapshot) (*model.KernelInstance, error) {
	instance, err := model.GetKernelInstanceByType(options.KernelType)
	if err == nil {
		return instance, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	workDir := filepath.Join(runtimeDataRoot, "runtime", options.KernelType)
	configPath := filepath.Join(workDir, runtimeConfigFileName)
	instance = &model.KernelInstance{
		KernelType:        options.KernelType,
		Status:            model.KernelInstanceStatusStopped,
		WorkDir:           workDir,
		ConfigPath:        configPath,
		ControllerAddress: options.ControllerAddress,
		ControllerSecret:  options.ControllerSecret,
	}
	if err := model.DB.Create(instance).Error; err != nil {
		return nil, err
	}
	return instance, nil
}

func buildFinalRuntimeConfig(options runtimeOptionSnapshot, persistSnapshots bool) (*runtimeconfig.FinalRenderResult, string, string, error) {
	profiles, err := ListPortProfiles()
	if err != nil {
		return nil, "", "", err
	}
	enabled := make([]*model.PortProfileWithNodes, 0, len(profiles))
	for _, profile := range profiles {
		if profile.Profile.IncludeInRuntime {
			runtimeProfile := *profile
			runtimeProfile.Nodes = selectRuntimeNodes(profile.Nodes)
			if len(runtimeProfile.Nodes) == 0 {
				continue
			}
			runtimeProfile.NodeIDs = make([]int, 0, len(runtimeProfile.Nodes))
			for _, node := range runtimeProfile.Nodes {
				runtimeProfile.NodeIDs = append(runtimeProfile.NodeIDs, node.ID)
			}
			enabled = append(enabled, &runtimeProfile)
		}
	}
	if len(enabled) == 0 {
		return nil, "", "", errNoRunnableProfiles
	}
	if options.ControllerSecret == "" {
		return nil, "", "", fmt.Errorf("Mihomo 控制密钥为空")
	}
	profileIDs := make([]int, 0, len(enabled))
	for _, profile := range enabled {
		profileIDs = append(profileIDs, profile.Profile.ID)
	}
	rules, err := listEnabledRoutingRulesForRuntime(profileIDs)
	if err != nil {
		return nil, "", "", err
	}
	workDir := filepath.Join(runtimeDataRoot, "runtime", options.KernelType)
	configPath := filepath.Join(workDir, runtimeConfigFileName)
	rendered, err := runtimeconfig.RenderFinalMihomoConfig(runtimeconfig.AggregatedMihomoInput{
		Profiles:          enabled,
		Rules:             rules,
		AllowLAN:          options.AllowLAN,
		Mode:              options.Mode,
		ControllerAddress: options.ControllerAddress,
		ControllerSecret:  options.ControllerSecret,
	})
	if err != nil {
		return nil, "", "", err
	}
	if persistSnapshots {
		for _, profile := range enabled {
			if _, err := SaveRuntimePreview(profile.Profile.ID); err != nil {
				return nil, "", "", err
			}
		}
	}
	return rendered, workDir, configPath, nil
}

func selectRuntimeNodes(nodes []*model.ProxyNode) []*model.ProxyNode {
	available := make([]*model.ProxyNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil || !node.Enabled {
			continue
		}
		if node.QuarantinedAt != nil {
			continue
		}
		available = append(available, node)
	}
	return available
}

func snapshotRuntimeOptions() runtimeOptionSnapshot {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	address := strings.TrimSpace(common.ClashExternalController)
	if address == "" {
		address = common.DefaultClashExternalController
	}
	mode := strings.TrimSpace(strings.ToLower(common.ClashMode))
	switch mode {
	case "rule", "global", "direct":
	default:
		mode = common.DefaultClashMode
	}

	return runtimeOptionSnapshot{
		KernelType:        strings.TrimSpace(strings.ToLower(common.KernelType)),
		BinaryPath:        strings.TrimSpace(common.MihomoBinaryPath),
		AllowLAN:          common.ClashAllowLAN,
		ControllerAddress: address,
		ControllerSecret:  strings.TrimSpace(common.ClashSecret),
		Mode:              mode,
		AutoStartEnabled:  common.RuntimeAutoStartEnabled,
	}
}

func persistRuntimeState(instance *model.KernelInstance, status string, action string, lastError string, pid *int, rendered *runtimeconfig.FinalRenderResult) error {
	instance.Status = status
	instance.LastAction = action
	instance.LastError = lastError
	instance.PID = pid
	if rendered != nil {
		instance.ActiveConfigChecksum = rendered.Checksum
		instance.ActiveProfileCount = rendered.ProfileCount
		instance.ActiveListenerCount = rendered.ListenerCount
	}
	return saveRuntimeInstance(instance)
}

func recordRuntimeFailure(instance *model.KernelInstance, action string, failure error, pid *int) error {
	if failure == nil {
		failure = errors.New("未知运行时错误")
	}
	if err := persistRuntimeState(instance, model.KernelInstanceStatusError, action, failure.Error(), pid, nil); err != nil {
		return errors.Join(failure, fmt.Errorf("保存运行失败状态失败: %w", err))
	}
	return failure
}

func cleanupStartedRuntime(instance *model.KernelInstance, process *runtimeProcess, failure error) error {
	cleanupErr := terminateMihomoProcess(process)
	if cleanupErr == nil {
		runtimeRegistry.detachProcess(process)
		instance.PID = nil
	}
	instance.Status = model.KernelInstanceStatusError
	instance.LastAction = "start"
	instance.LastError = failure.Error()
	if cleanupErr != nil {
		instance.LastError += fmt.Sprintf("；清理进程失败，仍保留监管: %v", cleanupErr)
	}

	saveErr := saveRuntimeInstance(instance)
	return errors.Join(
		failure,
		wrapOptionalError("清理 Mihomo 进程失败，进程仍由 PoolNeXt 监管", cleanupErr),
		wrapOptionalError("保存启动失败状态失败", saveErr),
	)
}

func wrapOptionalError(message string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

func ensureRuntimeWorkDir(workDir string) error {
	if err := os.MkdirAll(workDir, runtimeWorkDirMode); err != nil {
		return err
	}
	return os.Chmod(workDir, runtimeWorkDirMode)
}

func writeRuntimeConfig(configPath string, content []byte) error {
	if err := ensureRuntimeWorkDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(configPath), ".config-*.yaml")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(runtimeConfigFileMode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, configPath); err != nil {
		return err
	}
	return os.Chmod(configPath, runtimeConfigFileMode)
}

func readRuntimeConfigBackup(configPath string) ([]byte, bool, error) {
	content, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func restoreRuntimeConfig(configPath string, content []byte, existed bool) error {
	if !existed {
		if err := os.Remove(configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeRuntimeConfig(configPath, content)
}

func (registry *runtimeProcessRegistry) newLogWriter(stream string) *runtimeLogWriter {
	return &runtimeLogWriter{
		registry: registry,
		stream:   strings.TrimSpace(stream),
		pending:  make([]byte, 0, 1024),
	}
}

func (writer *runtimeLogWriter) Write(payload []byte) (int, error) {
	written := len(payload)
	writer.mu.Lock()
	defer writer.mu.Unlock()

	for len(payload) > 0 {
		newline := bytes.IndexByte(payload, '\n')
		if newline < 0 {
			writer.appendFragmentLocked(payload)
			break
		}
		writer.appendFragmentLocked(payload[:newline])
		writer.emitLocked()
		payload = payload[newline+1:]
	}
	return written, nil
}

func (writer *runtimeLogWriter) Flush() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.pending) == 0 && !writer.truncated {
		return
	}
	writer.emitLocked()
}

func (writer *runtimeLogWriter) appendFragmentLocked(fragment []byte) {
	remaining := runtimeLogMessageLimit - len(writer.pending)
	if remaining > 0 {
		if len(fragment) < remaining {
			remaining = len(fragment)
		}
		writer.pending = append(writer.pending, fragment[:remaining]...)
	}
	if len(fragment) > remaining {
		writer.truncated = true
	}
}

func (writer *runtimeLogWriter) emitLocked() {
	message := strings.TrimSpace(string(writer.pending))
	if writer.truncated {
		message += runtimeLogTruncatedIndicator
	}
	writer.pending = writer.pending[:0]
	writer.truncated = false
	if message == "" {
		return
	}

	level := model.AppLogLevelInfo
	lower := strings.ToLower(message)
	if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") {
		level = model.AppLogLevelError
	} else if strings.Contains(lower, "warn") {
		level = model.AppLogLevelWarn
	}
	writer.registry.appendLog(writer.stream, level, message)
}

func (registry *runtimeProcessRegistry) appendLog(stream string, level string, message string) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.logSeq++
	entry := &RuntimeLogEntry{
		Seq:       registry.logSeq,
		Stream:    stream,
		Level:     level,
		Message:   message,
		CreatedAt: time.Now(),
	}
	registry.logs = append(registry.logs, entry)
	if len(registry.logs) > runtimeLogLimit {
		registry.logs = registry.logs[len(registry.logs)-runtimeLogLimit:]
	}
}

func (registry *runtimeProcessRegistry) registerProcess(cmd *exec.Cmd, stdout *runtimeLogWriter, stderr *runtimeLogWriter, startedAt time.Time, options runtimeOptionSnapshot) *runtimeProcess {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.generation++
	process := &runtimeProcess{
		cmd:               cmd,
		generation:        registry.generation,
		kernelType:        options.KernelType,
		controllerAddress: options.ControllerAddress,
		controllerSecret:  options.ControllerSecret,
		stdout:            stdout,
		stderr:            stderr,
		done:              make(chan struct{}),
		settled:           make(chan struct{}),
	}
	registry.process = process
	registry.startedAt = startedAt
	return process
}

func (registry *runtimeProcessRegistry) currentProcess() *runtimeProcess {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.process
}

func (registry *runtimeProcessRegistry) activeProcess() *runtimeProcess {
	process := registry.currentProcess()
	if process == nil {
		return nil
	}
	select {
	case <-process.done:
		return nil
	default:
		return process
	}
}

func (registry *runtimeProcessRegistry) setResumeAfterNodeHealthRecovery(enabled bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.resumeAfterNodeHealthRecovery = enabled
}

func (registry *runtimeProcessRegistry) shouldResumeAfterNodeHealthRecovery() bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.resumeAfterNodeHealthRecovery
}

func (registry *runtimeProcessRegistry) detachProcess(process *runtimeProcess) bool {
	if process == nil {
		return false
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.process != process || registry.generation != process.generation {
		return false
	}
	registry.process = nil
	registry.startedAt = time.Time{}
	return true
}

func (registry *runtimeProcessRegistry) waitProcessExit(process *runtimeProcess) {
	err := process.cmd.Wait()
	process.stdout.Flush()
	process.stderr.Flush()
	process.waitErr = err
	close(process.done)
	defer close(process.settled)
	registry.settleProcessExit(process, err)
}

func (registry *runtimeProcessRegistry) settleProcessExit(process *runtimeProcess, err error) {
	registry.operationMu.Lock()
	defer registry.operationMu.Unlock()
	if !registry.detachProcess(process) {
		return
	}
	connectionRegistry.stop()
	if err != nil {
		registry.appendLog("system", model.AppLogLevelError, fmt.Sprintf("Mihomo 进程退出: %v", err))
	}
	kernelType := process.kernelType
	if kernelType == "" {
		kernelType = snapshotRuntimeOptions().KernelType
	}
	instance, getErr := model.GetKernelInstanceByType(kernelType)
	if getErr != nil {
		return
	}
	now := time.Now()
	instance.PID = nil
	instance.LastStoppedAt = &now
	if err != nil {
		instance.Status = model.KernelInstanceStatusError
		instance.LastError = err.Error()
	} else {
		instance.Status = model.KernelInstanceStatusStopped
		instance.LastError = ""
	}
	if saveErr := saveRuntimeInstance(instance); saveErr != nil {
		registry.appendLog("system", model.AppLogLevelError, fmt.Sprintf("保存 Mihomo 退出状态失败: %v", saveErr))
	}
}

func (process *runtimeProcess) waitResultIfDone() (error, bool) {
	if process == nil {
		return nil, true
	}
	select {
	case <-process.done:
		return process.waitErr, true
	default:
		return nil, false
	}
}

func terminateProcess(process *runtimeProcess) error {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return nil
	}
	_ = process.cmd.Process.Signal(os.Interrupt)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-process.done:
		return nil
	case <-timer.C:
	}
	if err := process.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	killTimer := time.NewTimer(2 * time.Second)
	defer killTimer.Stop()
	select {
	case <-process.done:
		return nil
	case <-killTimer.C:
		return fmt.Errorf("等待 Mihomo 进程退出超时")
	}
}

func ensureRuntimeListenersAvailable(listeners []runtimeconfig.RuntimeListener) error {
	for _, listener := range listeners {
		address := net.JoinHostPort(listener.Listen, fmt.Sprintf("%d", listener.Port))
		probe, err := net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("监听端口已被占用，无法启动 Mihomo: %s (%s)", address, listener.Name)
		}
		_ = probe.Close()
	}
	return nil
}

func formatRuntimeStartWaitError(waitErr error) error {
	message := fmt.Sprintf("等待 Mihomo 控制接口就绪失败: %v", waitErr)
	logs := runtimeRegistry.recentLogSummary(8)
	if logs == "" {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s | 最近日志: %s", message, logs)
}

func (registry *runtimeProcessRegistry) recentLogSummary(limit int) string {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if limit <= 0 || len(registry.logs) == 0 {
		return ""
	}
	start := len(registry.logs) - limit
	if start < 0 {
		start = 0
	}

	messages := make([]string, 0, len(registry.logs)-start)
	for _, entry := range registry.logs[start:] {
		messages = append(messages, fmt.Sprintf("[%s] %s", entry.Stream, entry.Message))
	}
	return strings.Join(messages, " || ")
}
