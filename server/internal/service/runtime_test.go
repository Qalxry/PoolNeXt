package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"poolx/internal/model"
	"poolx/internal/pkg/common"
	kernelpkg "poolx/internal/pkg/kernel"
)

const runtimeProcessHelperEnv = "POOLNEXT_RUNTIME_PROCESS_HELPER"

func TestStartRuntimeRejectsOccupiedListenerPort(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()

	originalKernelType := common.KernelType
	originalBinaryPath := common.MihomoBinaryPath
	originalStarter := startMihomoProcess
	common.KernelType = KernelTypeMihomo
	common.MihomoBinaryPath = "/tmp/fake-mihomo"
	startCalled := false
	startMihomoProcess = func(binaryPath string, workDir string, configPath string, stdout io.Writer, stderr io.Writer) (*exec.Cmd, error) {
		startCalled = true
		return nil, assertiveError("should not start process when listener is occupied")
	}
	t.Cleanup(func() {
		common.KernelType = originalKernelType
		common.MihomoBinaryPath = originalBinaryPath
		startMihomoProcess = originalStarter
		resetRuntimeRegistryForTest()
	})

	node := seedRuntimeTestNode(t, "occupied-port-node")

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy local port: %v", err)
	}
	t.Cleanup(func() {
		_ = occupied.Close()
	})

	port := occupied.Addr().(*net.TCPAddr).Port
	if _, err := CreatePortProfile(PortProfilePayload{
		Name:       "occupied-port-profile",
		ListenHost: "127.0.0.1",
		MixedPort:  port,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{node.ID},
	}); err != nil {
		t.Fatalf("create port profile: %v", err)
	}

	_, err = StartRuntime(context.Background())
	if err == nil {
		t.Fatal("expected occupied listener port to fail")
	}
	if !strings.Contains(err.Error(), "监听端口已被占用") {
		t.Fatalf("expected occupied port error, got %v", err)
	}
	if !strings.Contains(err.Error(), net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))) {
		t.Fatalf("expected occupied address in error, got %v", err)
	}
	if startCalled {
		t.Fatal("expected process start to be skipped when listener port is occupied")
	}
}

func TestStartRuntimeIncludesRecentLogsWhenControllerWaitFails(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()

	originalKernelType := common.KernelType
	originalBinaryPath := common.MihomoBinaryPath
	originalStarter := startMihomoProcess
	originalWaiter := waitForMihomoControllerReady
	common.KernelType = KernelTypeMihomo
	common.MihomoBinaryPath = "/tmp/fake-mihomo"
	startMihomoProcess = func(binaryPath string, workDir string, configPath string, stdout io.Writer, stderr io.Writer) (*exec.Cmd, error) {
		cmd := exec.Command("/bin/sh", "-c", "echo boot failed 1>&2; exit 1")
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd, nil
	}
	waitForMihomoControllerReady = func(ctx context.Context, controllerAddress string, secret string) error {
		time.Sleep(150 * time.Millisecond)
		return context.DeadlineExceeded
	}
	t.Cleanup(func() {
		common.KernelType = originalKernelType
		common.MihomoBinaryPath = originalBinaryPath
		startMihomoProcess = originalStarter
		waitForMihomoControllerReady = originalWaiter
		resetRuntimeRegistryForTest()
	})

	node := seedRuntimeTestNode(t, "controller-wait-node")
	freeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local port: %v", err)
	}
	freePort := freeListener.Addr().(*net.TCPAddr).Port
	_ = freeListener.Close()
	if _, err := CreatePortProfile(PortProfilePayload{
		Name:       "controller-wait-profile",
		ListenHost: "127.0.0.1",
		MixedPort:  freePort,
		SocksPort:  0,
		HTTPPort:   0,
		ProxySettings: model.PortProfileProxySettings{
			StrategyType:        model.PortProfileStrategyFallback,
			TestURL:             "https://cp.cloudflare.com/generate_204",
			TestIntervalSeconds: 300,
			UDPEnabled:          true,
		},
		IncludeInRuntime: true,
		NodeIDs:          []int{node.ID},
	}); err != nil {
		t.Fatalf("create port profile: %v", err)
	}

	_, err = StartRuntime(context.Background())
	if err == nil {
		t.Fatal("expected controller wait failure")
	}
	if !strings.Contains(err.Error(), "等待 Mihomo 控制接口就绪失败") {
		t.Fatalf("expected controller wait error, got %v", err)
	}
	if !strings.Contains(err.Error(), "boot failed") {
		t.Fatalf("expected recent logs in error, got %v", err)
	}
}

func TestRuntimeLogWriterHandlesLongLinesWithoutBlockingWait(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)
	if _, err := ensureKernelInstance(); err != nil {
		t.Fatalf("ensure kernel instance: %v", err)
	}

	process := startRuntimeProcessHelper(t, "long-line")
	select {
	case <-process.settled:
	case <-time.After(3 * time.Second):
		_ = process.cmd.Process.Kill()
		t.Fatal("process Wait blocked while consuming a line larger than 64 KiB")
	}

	logs, err := GetRuntimeLogs(0, runtimeLogLimit)
	if err != nil {
		t.Fatalf("get runtime logs: %v", err)
	}
	if len(logs.Items) == 0 {
		t.Fatal("expected long stdout line to be captured")
	}
	message := logs.Items[0].Message
	if !strings.HasSuffix(message, runtimeLogTruncatedIndicator) {
		t.Fatalf("expected long line to be truncated, got suffix %q", message[max(0, len(message)-32):])
	}
	if len(message) > runtimeLogMessageLimit+len(runtimeLogTruncatedIndicator) {
		t.Fatalf("expected bounded log message, got %d bytes", len(message))
	}
}

func TestRuntimeLogWriterFlushesPartialLineOnProcessExit(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)
	if _, err := ensureKernelInstance(); err != nil {
		t.Fatalf("ensure kernel instance: %v", err)
	}

	process := startRuntimeProcessHelper(t, "partial-line")
	select {
	case <-process.settled:
	case <-time.After(3 * time.Second):
		_ = process.cmd.Process.Kill()
		t.Fatal("process exit handling did not settle")
	}

	logs, err := GetRuntimeLogs(0, runtimeLogLimit)
	if err != nil {
		t.Fatalf("get runtime logs: %v", err)
	}
	for _, entry := range logs.Items {
		if entry.Stream == "stderr" && entry.Message == "unterminated stderr line" {
			return
		}
	}
	t.Fatal("expected unterminated stderr line to be flushed after process exit")
}

func TestRuntimeLogCaptureDoesNotLeakGoroutines(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)
	if _, err := ensureKernelInstance(); err != nil {
		t.Fatalf("ensure kernel instance: %v", err)
	}

	baseline := goruntime.NumGoroutine()
	for index := 0; index < 8; index++ {
		process := startRuntimeProcessHelper(t, "partial-line")
		select {
		case <-process.settled:
		case <-time.After(3 * time.Second):
			_ = process.cmd.Process.Kill()
			t.Fatalf("process %d exit handling did not settle", index)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		goruntime.GC()
		if current := goruntime.NumGoroutine(); current <= baseline+3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("runtime log capture leaked goroutines: baseline=%d current=%d", baseline, goruntime.NumGoroutine())
}

func TestOldRuntimeGenerationCannotOverwriteCurrentState(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)

	originalKernelType := common.KernelType
	common.KernelType = KernelTypeMihomo
	t.Cleanup(func() {
		common.KernelType = originalKernelType
	})

	instance, err := ensureKernelInstance()
	if err != nil {
		t.Fatalf("ensure kernel instance: %v", err)
	}
	currentPID := 4242
	instance.PID = &currentPID
	instance.Status = model.KernelInstanceStatusRunning
	instance.LastError = ""
	if err := model.DB.Save(instance).Error; err != nil {
		t.Fatalf("save current runtime state: %v", err)
	}

	oldProcess := &runtimeProcess{generation: 1}
	currentProcess := &runtimeProcess{generation: 2, done: make(chan struct{})}
	runtimeRegistry.mu.Lock()
	runtimeRegistry.generation = currentProcess.generation
	runtimeRegistry.process = currentProcess
	runtimeRegistry.mu.Unlock()

	runtimeRegistry.settleProcessExit(oldProcess, errors.New("old generation crashed"))

	stored, err := model.GetKernelInstanceByType(common.KernelType)
	if err != nil {
		t.Fatalf("reload kernel instance: %v", err)
	}
	if stored.Status != model.KernelInstanceStatusRunning {
		t.Fatalf("old generation overwrote status: %s", stored.Status)
	}
	if stored.PID == nil || *stored.PID != currentPID {
		t.Fatalf("old generation overwrote current PID: %v", stored.PID)
	}
	if stored.LastError != "" {
		t.Fatalf("old generation overwrote current error: %q", stored.LastError)
	}
	if runtimeRegistry.currentProcess() != currentProcess {
		t.Fatal("old generation detached the current process")
	}
}

func TestStopRuntimeDoesNotSignalUnownedStalePID(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)

	instance, err := ensureKernelInstance()
	if err != nil {
		t.Fatalf("ensure kernel instance: %v", err)
	}
	unowned := exec.Command("sleep", "30")
	if err := unowned.Start(); err != nil {
		t.Fatalf("start unowned process: %v", err)
	}
	t.Cleanup(func() {
		_ = unowned.Process.Kill()
		_ = unowned.Wait()
	})

	pid := unowned.Process.Pid
	instance.PID = &pid
	instance.Status = model.KernelInstanceStatusRunning
	if err := model.DB.Save(instance).Error; err != nil {
		t.Fatalf("save stale pid: %v", err)
	}

	_, err = StopRuntime(context.Background())
	if err == nil || !strings.Contains(err.Error(), "未由当前 PoolNeXt 进程托管") {
		t.Fatalf("expected unowned pid refusal, got %v", err)
	}
	if err := unowned.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unowned process was signaled or exited: %v", err)
	}

	stored, err := model.GetKernelInstanceByType(snapshotRuntimeOptions().KernelType)
	if err != nil {
		t.Fatalf("reload kernel instance: %v", err)
	}
	if stored.PID != nil {
		t.Fatalf("expected stale pid reference to be cleared, got %v", *stored.PID)
	}
	if stored.Status != model.KernelInstanceStatusError {
		t.Fatalf("expected stale pid cleanup to remain visible, got %s", stored.Status)
	}
}

func TestGetRuntimeStatusDoesNotExposeControllerOrNodeSecrets(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)

	const controllerSecret = "controller-secret-must-not-leak"
	const nodeSecret = "node-password-must-not-leak"
	if err := model.UpdateOption("ClashSecret", controllerSecret); err != nil {
		t.Fatalf("set controller secret: %v", err)
	}
	node := seedRuntimeTestNode(t, "status-secret-node")
	node.MetadataJSON = fmt.Sprintf(`{"type":"ss","server":"127.0.0.1","port":443,"cipher":"aes-128-gcm","password":%q}`, nodeSecret)
	if err := model.DB.Save(node).Error; err != nil {
		t.Fatalf("save secret node: %v", err)
	}
	seedIncludedRuntimeProfile(t, node, "status-secret-profile")

	status, err := GetRuntimeStatus(context.Background())
	if err != nil {
		t.Fatalf("get runtime status: %v", err)
	}
	payload, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal runtime status: %v", err)
	}
	for _, secret := range []string{controllerSecret, nodeSecret} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("runtime status leaked secret %q: %s", secret, payload)
		}
	}
	if strings.Contains(string(payload), "rendered_config_preview") {
		t.Fatalf("runtime status unexpectedly exposed a rendered config field: %s", payload)
	}
}

func TestWriteRuntimeConfigUsesPrivatePermissions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), runtimeConfigFileName)
	if err := os.WriteFile(configPath, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed runtime config: %v", err)
	}
	if err := writeRuntimeConfig(configPath, []byte("secret: rotated\n")); err != nil {
		t.Fatalf("write runtime config: %v", err)
	}

	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat runtime config: %v", err)
	}
	if got := info.Mode().Perm(); got != runtimeConfigFileMode {
		t.Fatalf("unexpected config permissions: got %04o want %04o", got, runtimeConfigFileMode)
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	if string(content) != "secret: rotated\n" {
		t.Fatalf("unexpected runtime config content: %q", content)
	}
}

func TestReloadRuntimeUsesActiveConfigPathAndOwnedCredentials(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)

	originalDataRoot := runtimeDataRoot
	originalReload := reloadMihomoConfig
	originalVersion := getMihomoVersion
	runtimeDataRoot = t.TempDir()
	t.Cleanup(func() {
		runtimeDataRoot = originalDataRoot
		reloadMihomoConfig = originalReload
		getMihomoVersion = originalVersion
	})

	const controllerAddress = "127.0.0.1:29090"
	const controllerSecret = "owned-controller-secret"
	if err := model.UpdateOption("ClashExternalController", controllerAddress); err != nil {
		t.Fatalf("set controller address: %v", err)
	}
	if err := model.UpdateOption("ClashSecret", controllerSecret); err != nil {
		t.Fatalf("set controller secret: %v", err)
	}
	node := seedRuntimeTestNode(t, "reload-node")
	seedIncludedRuntimeProfile(t, node, "reload-profile")

	options := snapshotRuntimeOptions()
	instance, err := ensureKernelInstanceFor(options)
	if err != nil {
		t.Fatalf("ensure kernel instance: %v", err)
	}
	pid := 4242
	instance.PID = &pid
	instance.Status = model.KernelInstanceStatusRunning
	instance.ActiveConfigChecksum = "old-checksum"
	if err := model.DB.Save(instance).Error; err != nil {
		t.Fatalf("save running instance: %v", err)
	}

	process := &runtimeProcess{
		generation:        1,
		kernelType:        options.KernelType,
		controllerAddress: controllerAddress,
		controllerSecret:  controllerSecret,
		done:              make(chan struct{}),
		settled:           make(chan struct{}),
	}
	runtimeRegistry.mu.Lock()
	runtimeRegistry.generation = process.generation
	runtimeRegistry.process = process
	runtimeRegistry.mu.Unlock()

	var gotAddress string
	var gotSecret string
	var gotPath string
	reloadMihomoConfig = func(ctx context.Context, address string, secret string, path string) error {
		gotAddress = address
		gotSecret = secret
		gotPath = path
		return nil
	}
	getMihomoVersion = func(ctx context.Context, address string, secret string) (string, error) {
		return "test-version", nil
	}

	if _, err := ReloadRuntime(context.Background()); err != nil {
		t.Fatalf("reload runtime: %v", err)
	}
	if gotPath != "" {
		t.Fatalf("expected Mihomo active config reload path, got %q", gotPath)
	}
	if gotAddress != controllerAddress || gotSecret != controllerSecret {
		t.Fatalf("reload used wrong controller credentials: address=%q secret=%q", gotAddress, gotSecret)
	}
	info, err := os.Stat(filepath.Join(runtimeDataRoot, "runtime", options.KernelType, runtimeConfigFileName))
	if err != nil {
		t.Fatalf("stat reloaded config: %v", err)
	}
	if got := info.Mode().Perm(); got != runtimeConfigFileMode {
		t.Fatalf("unexpected reloaded config permissions: got %04o want %04o", got, runtimeConfigFileMode)
	}
}

func TestStartRuntimePreservesRegistryWhenCleanupFails(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()

	originalDataRoot := runtimeDataRoot
	originalStarter := startMihomoProcess
	originalWaiter := waitForMihomoControllerReady
	originalTerminator := terminateMihomoProcess
	runtimeDataRoot = t.TempDir()
	if err := model.UpdateOption("KernelType", KernelTypeMihomo); err != nil {
		t.Fatalf("set kernel type: %v", err)
	}
	if err := model.UpdateOption("MihomoBinaryPath", "/tmp/fake-mihomo"); err != nil {
		t.Fatalf("set binary path: %v", err)
	}
	node := seedRuntimeTestNode(t, "cleanup-failure-node")
	seedIncludedRuntimeProfile(t, node, "cleanup-failure-profile")

	startMihomoProcess = startLongRunningRuntimeTestProcess
	waitForMihomoControllerReady = func(ctx context.Context, controllerAddress string, secret string) error {
		return context.DeadlineExceeded
	}
	terminateMihomoProcess = func(process *runtimeProcess) error {
		return errors.New("forced termination failure")
	}
	t.Cleanup(func() {
		terminateMihomoProcess = originalTerminator
		startMihomoProcess = originalStarter
		waitForMihomoControllerReady = originalWaiter
		runtimeDataRoot = originalDataRoot
		if process := runtimeRegistry.currentProcess(); process != nil {
			_ = terminateProcess(process)
			select {
			case <-process.settled:
			case <-time.After(3 * time.Second):
			}
		}
		resetRuntimeRegistryForTest()
	})

	_, err := StartRuntime(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forced termination failure") {
		t.Fatalf("expected cleanup failure, got %v", err)
	}
	if runtimeRegistry.activeProcess() == nil {
		t.Fatal("cleanup failure must preserve the owned process in the registry")
	}
	instance, err := model.GetKernelInstanceByType(KernelTypeMihomo)
	if err != nil {
		t.Fatalf("reload kernel instance: %v", err)
	}
	if instance.PID == nil || instance.Status != model.KernelInstanceStatusError {
		t.Fatalf("expected supervised error state with pid, got status=%s pid=%v", instance.Status, instance.PID)
	}
}

func TestStartRuntimeCleansProcessWhenFinalStateSaveFails(t *testing.T) {
	setupServiceTestDB(t)
	resetRuntimeRegistryForTest()
	t.Cleanup(resetRuntimeRegistryForTest)

	originalDataRoot := runtimeDataRoot
	originalStarter := startMihomoProcess
	originalWaiter := waitForMihomoControllerReady
	originalSaver := saveRuntimeInstance
	runtimeDataRoot = t.TempDir()
	if err := model.UpdateOption("KernelType", KernelTypeMihomo); err != nil {
		t.Fatalf("set kernel type: %v", err)
	}
	if err := model.UpdateOption("MihomoBinaryPath", "/tmp/fake-mihomo"); err != nil {
		t.Fatalf("set binary path: %v", err)
	}
	node := seedRuntimeTestNode(t, "final-save-node")
	seedIncludedRuntimeProfile(t, node, "final-save-profile")

	startMihomoProcess = startLongRunningRuntimeTestProcess
	waitForMihomoControllerReady = func(ctx context.Context, controllerAddress string, secret string) error {
		return nil
	}
	saveCalls := 0
	saveRuntimeInstance = func(instance *model.KernelInstance) error {
		saveCalls++
		if saveCalls == 3 {
			return errors.New("forced final state save failure")
		}
		return originalSaver(instance)
	}
	t.Cleanup(func() {
		saveRuntimeInstance = originalSaver
		startMihomoProcess = originalStarter
		waitForMihomoControllerReady = originalWaiter
		runtimeDataRoot = originalDataRoot
	})

	_, err := StartRuntime(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forced final state save failure") {
		t.Fatalf("expected final state save failure, got %v", err)
	}
	if runtimeRegistry.activeProcess() != nil {
		t.Fatal("final state save failure must clean up the started process")
	}
	instance, err := model.GetKernelInstanceByType(KernelTypeMihomo)
	if err != nil {
		t.Fatalf("reload kernel instance: %v", err)
	}
	if instance.PID != nil || instance.Status != model.KernelInstanceStatusError {
		t.Fatalf("expected cleaned error state, got status=%s pid=%v", instance.Status, instance.PID)
	}
}

func TestSnapshotRuntimeOptionsWaitsForCompleteWrite(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalAddress := common.ClashExternalController
	originalSecret := common.ClashSecret
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.ClashExternalController = originalAddress
		common.ClashSecret = originalSecret
		common.OptionMapRWMutex.Unlock()
	})

	common.OptionMapRWMutex.Lock()
	common.ClashExternalController = "127.0.0.1:31000"
	common.ClashSecret = "snapshot-secret"

	started := make(chan struct{})
	result := make(chan runtimeOptionSnapshot, 1)
	go func() {
		close(started)
		result <- snapshotRuntimeOptions()
	}()
	<-started
	select {
	case <-result:
		common.OptionMapRWMutex.Unlock()
		t.Fatal("runtime option snapshot bypassed the option read lock")
	case <-time.After(50 * time.Millisecond):
	}
	common.OptionMapRWMutex.Unlock()

	snapshot := <-result
	if snapshot.ControllerAddress != "127.0.0.1:31000" || snapshot.ControllerSecret != "snapshot-secret" {
		t.Fatalf("unexpected option snapshot: %+v", snapshot)
	}
}

func TestMihomoReloadUsesActiveConfigPathIntegration(t *testing.T) {
	sourceBinary := strings.TrimSpace(os.Getenv("POOLNEXT_TEST_MIHOMO_BINARY"))
	if sourceBinary == "" {
		t.Skip("POOLNEXT_TEST_MIHOMO_BINARY is not set")
	}

	sourceInfoBefore, err := os.Stat(sourceBinary)
	if err != nil {
		t.Fatalf("stat source Mihomo binary: %v", err)
	}
	workDir := t.TempDir()
	temporaryBinary := filepath.Join(workDir, "mihomo-test-copy")
	if err := copyRuntimeTestExecutable(sourceBinary, temporaryBinary); err != nil {
		t.Fatalf("copy Mihomo binary: %v", err)
	}

	controllerPort := reserveRuntimeTestPort(t)
	proxyPort := reserveRuntimeTestPort(t)
	controllerAddress := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", controllerPort))
	const secret = "integration-controller-secret"
	configPath := filepath.Join(workDir, runtimeConfigFileName)
	configForMode := func(mode string) []byte {
		return []byte(fmt.Sprintf("mixed-port: %d\nallow-lan: false\nbind-address: 127.0.0.1\nmode: %s\nlog-level: warning\nexternal-controller: %s\nsecret: %s\nrules:\n  - MATCH,DIRECT\n", proxyPort, mode, controllerAddress, secret))
	}
	if err := writeRuntimeConfig(configPath, configForMode("rule")); err != nil {
		t.Fatalf("write initial Mihomo config: %v", err)
	}

	cmd, err := kernelpkg.StartMihomoProcess(temporaryBinary, workDir, configPath, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("start temporary Mihomo copy: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	readyCtx, cancelReady := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelReady()
	if err := kernelpkg.WaitForControllerReady(readyCtx, controllerAddress, secret); err != nil {
		t.Fatalf("wait for temporary Mihomo controller: %v", err)
	}

	if err := writeRuntimeConfig(configPath, configForMode("global")); err != nil {
		t.Fatalf("write reloaded Mihomo config: %v", err)
	}
	reloadCtx, cancelReload := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancelReload()
	if err := kernelpkg.ReloadMihomoConfig(reloadCtx, controllerAddress, secret, ""); err != nil {
		t.Fatalf("reload Mihomo active config path: %v", err)
	}
	if mode, err := readMihomoMode(controllerAddress, secret); err != nil {
		t.Fatalf("read reloaded Mihomo mode: %v", err)
	} else if !strings.EqualFold(mode, "global") {
		t.Fatalf("Mihomo did not apply the active config file: mode=%q", mode)
	}

	sourceInfoAfter, err := os.Stat(sourceBinary)
	if err != nil {
		t.Fatalf("restat source Mihomo binary: %v", err)
	}
	if !os.SameFile(sourceInfoBefore, sourceInfoAfter) || sourceInfoBefore.Size() != sourceInfoAfter.Size() || !sourceInfoBefore.ModTime().Equal(sourceInfoAfter.ModTime()) || sourceInfoBefore.Mode() != sourceInfoAfter.Mode() {
		t.Fatal("source Mihomo binary metadata changed during read-only integration test")
	}
}

func TestRuntimeProcessHelper(t *testing.T) {
	switch os.Getenv(runtimeProcessHelperEnv) {
	case "long-line":
		_, _ = fmt.Fprintln(os.Stdout, strings.Repeat("x", 128*1024))
		os.Exit(0)
	case "partial-line":
		_, _ = fmt.Fprint(os.Stderr, "unterminated stderr line")
		os.Exit(0)
	}
}

func startRuntimeProcessHelper(t *testing.T, mode string) *runtimeProcess {
	t.Helper()
	stdout := runtimeRegistry.newLogWriter("stdout")
	stderr := runtimeRegistry.newLogWriter("stderr")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuntimeProcessHelper$")
	cmd.Env = append(os.Environ(), runtimeProcessHelperEnv+"="+mode)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start runtime helper process: %v", err)
	}
	process := runtimeRegistry.registerProcess(cmd, stdout, stderr, time.Now(), snapshotRuntimeOptions())
	go runtimeRegistry.waitProcessExit(process)
	return process
}

func seedRuntimeTestNode(t *testing.T, name string) *model.ProxyNode {
	t.Helper()

	node := &model.ProxyNode{
		SourceConfigID:   1,
		SourceConfigName: "seed.yaml",
		Name:             name,
		Type:             "vless",
		Server:           "127.0.0.1",
		Port:             443,
		Fingerprint:      "fingerprint-" + name,
		MetadataJSON:     `{"type":"vless","server":"127.0.0.1","port":443,"uuid":"63a636ac-a5b2-4463-b4f9-e983bf4680bf","tls":true,"servername":"learn.microsoft.com","client-fingerprint":"chrome"}`,
		Enabled:          true,
	}
	if err := model.DB.Create(node).Error; err != nil {
		t.Fatalf("seed proxy node: %v", err)
	}
	return node
}

func seedIncludedRuntimeProfile(t *testing.T, node *model.ProxyNode, name string) *model.PortProfileWithNodes {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve runtime profile port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	profile, err := CreatePortProfile(PortProfilePayload{
		Name:       name,
		ListenHost: "127.0.0.1",
		MixedPort:  port,
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
		t.Fatalf("create runtime profile: %v", err)
	}
	return profile
}

func startLongRunningRuntimeTestProcess(binaryPath string, workDir string, configPath string, stdout io.Writer, stderr io.Writer) (*exec.Cmd, error) {
	cmd := exec.Command("sleep", "30")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func reserveRuntimeTestPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func copyRuntimeTestExecutable(sourcePath string, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		return err
	}
	if err := target.Close(); err != nil {
		return err
	}
	return os.Chmod(targetPath, 0o755)
}

func readMihomoMode(controllerAddress string, secret string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, "http://"+controllerAddress+"/configs", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected controller status: %s", response.Status)
	}
	var payload struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.Mode, nil
}

func resetRuntimeRegistryForTest() {
	runtimeRegistry.operationMu.Lock()
	defer runtimeRegistry.operationMu.Unlock()
	runtimeRegistry.mu.Lock()
	defer runtimeRegistry.mu.Unlock()
	runtimeRegistry.process = nil
	runtimeRegistry.generation = 0
	runtimeRegistry.logSeq = 0
	runtimeRegistry.logs = nil
	runtimeRegistry.startedAt = time.Time{}
}
