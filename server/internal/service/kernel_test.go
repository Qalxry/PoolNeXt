package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"poolx/internal/model"
	"runtime"
	"strings"
	"sync"
	"testing"

	"poolx/internal/pkg/common"
)

func TestValidateKernelTypeRejectsReservedKernel(t *testing.T) {
	err := validateKernelType(KernelTypeXray)
	if err == nil {
		t.Fatal("expected xray to be rejected for now")
	}
}

func TestSelectMihomoReleaseAssetPrefersCurrentPlatform(t *testing.T) {
	assets := []githubAsset{
		{Name: "mihomo-linux-arm64-v1.0.0.gz", BrowserDownloadURL: "https://example.com/arm64"},
		{Name: fmt.Sprintf("mihomo-%s-%s-v1.0.0.gz", runtime.GOOS, preferredMihomoArchKeywords()[0]), BrowserDownloadURL: "https://example.com/current"},
	}

	asset, err := selectMihomoReleaseAsset(assets)
	if err != nil {
		t.Fatalf("expected asset selection to succeed: %v", err)
	}
	if asset.BrowserDownloadURL != "https://example.com/current" {
		t.Fatalf("unexpected asset selected: %s", asset.BrowserDownloadURL)
	}
}

func TestSelectMihomoReleaseAssetSkipsPackageFormats(t *testing.T) {
	assets := []githubAsset{
		{Name: fmt.Sprintf("mihomo-%s-%s-v1.0.0.deb", runtime.GOOS, preferredMihomoArchKeywords()[0]), BrowserDownloadURL: "https://example.com/package"},
		{Name: fmt.Sprintf("mihomo-%s-%s-v1.0.0.gz", runtime.GOOS, preferredMihomoArchKeywords()[0]), BrowserDownloadURL: "https://example.com/binary"},
	}

	asset, err := selectMihomoReleaseAsset(assets)
	if err != nil {
		t.Fatalf("expected asset selection to succeed: %v", err)
	}
	if asset.BrowserDownloadURL != "https://example.com/binary" {
		t.Fatalf("expected gzip binary asset, got %s", asset.BrowserDownloadURL)
	}
}

func TestInstallUploadedMihomoBinary(t *testing.T) {
	setupServiceTestDB(t)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "mihomo")
	fileName, content := testExecutableFile("Mihomo Meta v1.19.21")

	originalKernelType := common.KernelType
	originalPath := common.MihomoBinaryPath
	originalVersion := common.MihomoBinaryVersion
	originalSource := common.MihomoBinarySource
	t.Cleanup(func() {
		common.KernelType = originalKernelType
		common.MihomoBinaryPath = originalPath
		common.MihomoBinaryVersion = originalVersion
		common.MihomoBinarySource = originalSource
	})

	result, err := InstallUploadedMihomoBinary(context.Background(), fileName, targetPath, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("expected Mihomo install to succeed: %v", err)
	}
	if result.KernelType != KernelTypeMihomo {
		t.Fatalf("unexpected kernel type: %s", result.KernelType)
	}
	if result.Mode != KernelModeManaged || result.Immutable {
		t.Fatalf("unexpected managed kernel mode: mode=%s immutable=%v", result.Mode, result.Immutable)
	}
	if !strings.Contains(result.DetectedVersion, "Mihomo") {
		t.Fatalf("unexpected version: %s", result.DetectedVersion)
	}
	if common.MihomoBinaryPath == "" || common.MihomoBinarySource != MihomoBinarySourceUpload {
		t.Fatalf("expected Mihomo options to be persisted")
	}
	if _, err := os.Stat(result.InstallPath); err != nil {
		t.Fatalf("expected installed binary at %s: %v", result.InstallPath, err)
	}
}

func TestInspectMihomoBinary(t *testing.T) {
	setupServiceTestDB(t)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "mihomo-existing")
	fileName, content := testExecutableFile("Mihomo Meta v1.19.22")
	if err := os.WriteFile(targetPath, content, 0o755); err != nil {
		t.Fatalf("write existing binary: %v", err)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(targetPath, ".exe") {
		renamedPath := targetPath + ".exe"
		if err := os.Rename(targetPath, renamedPath); err != nil {
			t.Fatalf("rename existing binary: %v", err)
		}
		targetPath = renamedPath
	}
	fileName = filepath.Base(targetPath)
	before := snapshotKernelFile(t, targetPath)

	result, err := InspectMihomoBinary(context.Background(), targetPath)
	if err != nil {
		t.Fatalf("expected inspect to succeed: %v", err)
	}
	if result.BinarySource != MihomoBinarySourceExisting {
		t.Fatalf("unexpected source: %s", result.BinarySource)
	}
	if result.FileName != fileName {
		t.Fatalf("unexpected file name: %s", result.FileName)
	}
	if result.Mode != KernelModeExternalImmutable || !result.Immutable {
		t.Fatalf("unexpected external kernel mode: mode=%s immutable=%v", result.Mode, result.Immutable)
	}
	assertKernelFileUnchanged(t, targetPath, before)

	var record kernelBinaryIdentityRecord
	if err := model.DB.Where("kernel_type = ? AND registered_path = ?", KernelTypeMihomo, result.InstallPath).First(&record).Error; err != nil {
		t.Fatalf("load persisted external identity: %v", err)
	}
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(content))
	if record.Mode != KernelModeExternalImmutable || record.SHA256 != expectedHash {
		t.Fatalf("unexpected persisted external identity: mode=%s sha256=%s device=%d inode=%d", record.Mode, record.SHA256, record.DeviceID, record.Inode)
	}
	if runtime.GOOS == "linux" && (record.DeviceID == 0 || record.Inode == 0) {
		t.Fatalf("Linux external identity is missing device/inode: device=%d inode=%d", record.DeviceID, record.Inode)
	}
}

func TestExternalImmutableMihomoRejectsUploadAndPreservesBinary(t *testing.T) {
	setupServiceTestDB(t)

	targetPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	registerExternalMihomo(t, targetPath)
	before := snapshotKernelFile(t, targetPath)

	managedPath := filepath.Join(t.TempDir(), "managed-mihomo")
	if err := model.UpdateOption("MihomoBinaryPath", managedPath); err != nil {
		t.Fatalf("mutate current Mihomo path option: %v", err)
	}
	if err := model.UpdateOption("MihomoBinarySource", MihomoBinarySourceUpload); err != nil {
		t.Fatalf("mutate current Mihomo source option: %v", err)
	}
	if err := model.CloseDB(); err != nil {
		t.Fatalf("close database before restart simulation: %v", err)
	}
	model.DB = nil
	if err := model.InitDB(); err != nil {
		t.Fatalf("reopen database for restart simulation: %v", err)
	}
	model.InitOptionMap()

	fileName, content := testExecutableFile("Mihomo Meta v1.20.0")
	_, err := InstallUploadedMihomoBinary(context.Background(), fileName, targetPath, bytes.NewReader(content))
	if !errors.Is(err, ErrExternalImmutableKernel) {
		t.Fatalf("expected immutable kernel rejection, got %v", err)
	}
	assertKernelFileUnchanged(t, targetPath, before)
}

func TestInspectMihomoBinaryRejectsIdentityChangeDuringVersionRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic executable replacement fixture requires Unix semantics")
	}
	setupServiceTestDB(t)

	targetPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	fileName, replacementContent := testExecutableFile("Mihomo Meta v1.20.0")
	replacementPath := filepath.Join(filepath.Dir(targetPath), "replacement-"+fileName)
	if err := os.WriteFile(replacementPath, replacementContent, 0o755); err != nil {
		t.Fatalf("write replacement Mihomo fixture: %v", err)
	}
	originalExec := execCommandContext
	var replaceOnce sync.Once
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		replaceOnce.Do(func() {
			if err := os.Rename(replacementPath, targetPath); err != nil {
				t.Fatalf("replace Mihomo during version read: %v", err)
			}
		})
		return originalExec(ctx, name, args...)
	}
	t.Cleanup(func() {
		execCommandContext = originalExec
	})

	_, err := InspectMihomoBinary(context.Background(), targetPath)
	if !errors.Is(err, ErrKernelPathChanged) {
		t.Fatalf("expected identity-change rejection, got %v", err)
	}
	if err := ensureKernelIdentitySchema(); err != nil {
		t.Fatalf("initialize identity schema for count: %v", err)
	}
	var count int64
	if err := model.DB.Model(&kernelBinaryIdentityRecord{}).Count(&count).Error; err != nil {
		t.Fatalf("count external identities: %v", err)
	}
	if count != 0 {
		t.Fatalf("changed binary must not be registered, got %d identities", count)
	}
}

func TestExternalImmutableMihomoRejectsDownloadBeforeNetwork(t *testing.T) {
	setupServiceTestDB(t)

	targetPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	registerExternalMihomo(t, targetPath)

	originalClient := kernelHTTPClient
	requestCount := 0
	kernelHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requestCount++
		return nil, fmt.Errorf("network must not be reached")
	})}
	t.Cleanup(func() {
		kernelHTTPClient = originalClient
	})

	_, err := DownloadAndInstallMihomoBinary(context.Background(), targetPath)
	if !errors.Is(err, ErrExternalImmutableKernel) {
		t.Fatalf("expected immutable kernel rejection, got %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected rejection before release download, got %d requests", requestCount)
	}
}

func TestInstallUploadedMihomoFailsClosedBeforeCreatingDirectories(t *testing.T) {
	setupServiceTestDB(t)

	root := t.TempDir()
	targetParent := filepath.Join(root, "must-not-exist", "nested")
	targetPath := filepath.Join(targetParent, "mihomo")
	fileName, content := testExecutableFile("Mihomo Meta v1.20.0")

	database := model.DB
	model.DB = nil
	t.Cleanup(func() {
		model.DB = database
	})

	_, err := InstallUploadedMihomoBinary(context.Background(), fileName, targetPath, bytes.NewReader(content))
	if !errors.Is(err, ErrKernelProtectionUnavailable) {
		t.Fatalf("expected protection-store rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "must-not-exist")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("fail-closed rejection must not create target directories, stat=%v", statErr)
	}
}

func TestManagedMihomoInstallUsesSeparatePathWithoutTouchingExternalBinary(t *testing.T) {
	setupServiceTestDB(t)

	externalPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	registerExternalMihomo(t, externalPath)
	externalBefore := snapshotKernelFile(t, externalPath)
	managedPath := filepath.Join(t.TempDir(), "mihomo-managed")
	fileName, content := testExecutableFile("Mihomo Meta v1.20.0")

	result, err := InstallUploadedMihomoBinary(context.Background(), fileName, managedPath, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("expected separate managed installation to succeed: %v", err)
	}
	if result.Mode != KernelModeManaged || result.Immutable {
		t.Fatalf("unexpected managed kernel mode: mode=%s immutable=%v", result.Mode, result.Immutable)
	}
	assertKernelFileUnchanged(t, externalPath, externalBefore)
}

func TestExternalImmutableMihomoRejectsMutationKinds(t *testing.T) {
	setupServiceTestDB(t)

	targetPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	registerExternalMihomo(t, targetPath)

	for _, operation := range []string{"upgrade", "overwrite", "delete", "chmod", "move"} {
		t.Run(operation, func(t *testing.T) {
			err := ensureManagedMihomoMutationAllowed(operation, targetPath)
			if !errors.Is(err, ErrExternalImmutableKernel) {
				t.Fatalf("expected %s to be rejected, got %v", operation, err)
			}
		})
	}
}

func TestReplaceExecutableFileRejectsExternalKernelAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink and hard-link identity checks use Unix test fixtures")
	}

	for _, aliasType := range []string{"symlink", "hardlink"} {
		t.Run(aliasType, func(t *testing.T) {
			setupServiceTestDB(t)

			targetPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
			aliasPath := filepath.Join(t.TempDir(), "mihomo-alias")
			var err error
			if aliasType == "symlink" {
				err = os.Symlink(targetPath, aliasPath)
			} else {
				err = os.Link(targetPath, aliasPath)
			}
			if err != nil {
				t.Fatalf("create %s: %v", aliasType, err)
			}

			registerExternalMihomo(t, targetPath)
			before := snapshotKernelFile(t, targetPath)
			tempPath := writeTestMihomoBinary(t, "Mihomo Meta v1.20.0")

			err = replaceExecutableFile(tempPath, aliasPath)
			if !errors.Is(err, ErrExternalImmutableKernel) {
				t.Fatalf("expected alias replacement rejection, got %v", err)
			}
			assertKernelFileUnchanged(t, targetPath, before)
			if _, statErr := os.Lstat(aliasPath); statErr != nil {
				t.Fatalf("expected alias to remain in place: %v", statErr)
			}
			if _, statErr := os.Stat(tempPath); statErr != nil {
				t.Fatalf("expected prepared managed binary to remain untouched: %v", statErr)
			}
		})
	}
}

func TestExternalAliasProtectionOverridesEarlierManagedIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard-link identity fixture requires Unix semantics")
	}
	setupServiceTestDB(t)

	managedPath := filepath.Join(t.TempDir(), "mihomo-managed")
	fileName, managedContent := testExecutableFile("Mihomo Meta v1.20.0")
	if _, err := InstallUploadedMihomoBinary(context.Background(), fileName, managedPath, bytes.NewReader(managedContent)); err != nil {
		t.Fatalf("install managed Mihomo fixture: %v", err)
	}
	externalAlias := filepath.Join(t.TempDir(), "mihomo-external-alias")
	if err := os.Link(managedPath, externalAlias); err != nil {
		t.Fatalf("create external hard-link alias: %v", err)
	}
	registerExternalMihomo(t, externalAlias)
	before := snapshotKernelFile(t, managedPath)

	upgradeName, upgradeContent := testExecutableFile("Mihomo Meta v1.21.0")
	_, err := InstallUploadedMihomoBinary(context.Background(), upgradeName, managedPath, bytes.NewReader(upgradeContent))
	if !errors.Is(err, ErrExternalImmutableKernel) {
		t.Fatalf("expected external alias to override managed identity, got %v", err)
	}
	assertKernelFileUnchanged(t, managedPath, before)
}

func TestUnknownMihomoSourceDoesNotDefaultToManaged(t *testing.T) {
	setupServiceTestDB(t)

	targetPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	setMihomoBinaryStateForTest(t, targetPath, "")
	before := snapshotKernelFile(t, targetPath)
	fileName, content := testExecutableFile("Mihomo Meta v1.20.0")

	_, err := InstallUploadedMihomoBinary(context.Background(), fileName, targetPath, bytes.NewReader(content))
	if !errors.Is(err, ErrKernelOwnershipUnknown) {
		t.Fatalf("expected unknown ownership rejection, got %v", err)
	}
	if mode := mihomoKernelModeForSource(""); mode != KernelModeUnknown {
		t.Fatalf("empty source must remain unknown, got %s", mode)
	}
	if mode := mihomoKernelModeForSource("future-source"); mode != KernelModeUnknown {
		t.Fatalf("unrecognized source must remain unknown, got %s", mode)
	}
	assertKernelFileUnchanged(t, targetPath, before)
}

func TestLegacyManagedMihomoIdentityIsAdoptedBeforeReplacement(t *testing.T) {
	setupServiceTestDB(t)

	targetPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	setMihomoBinaryStateForTest(t, targetPath, MihomoBinarySourceUpload)
	fileName, content := testExecutableFile("Mihomo Meta v1.20.0")

	result, err := InstallUploadedMihomoBinary(context.Background(), fileName, targetPath, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("replace legacy managed Mihomo: %v", err)
	}
	if result.Mode != KernelModeManaged || result.DetectedVersion != "Mihomo Meta v1.20.0" {
		t.Fatalf("unexpected managed result: %+v", result)
	}
	var record kernelBinaryIdentityRecord
	if err := model.DB.Where("registered_path = ?", result.InstallPath).First(&record).Error; err != nil {
		t.Fatalf("load adopted managed identity: %v", err)
	}
	if record.Mode != KernelModeManaged || record.Source != MihomoBinarySourceUpload {
		t.Fatalf("unexpected adopted identity: mode=%s source=%s", record.Mode, record.Source)
	}
}

func TestReplaceExecutableFileRechecksFinalHardLinkIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard-link replacement fixture requires Unix semantics")
	}
	setupServiceTestDB(t)

	externalPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	registerExternalMihomo(t, externalPath)
	externalBefore := snapshotKernelFile(t, externalPath)

	managedDir := t.TempDir()
	managedPath := filepath.Join(managedDir, "mihomo")
	fileName, managedContent := testExecutableFile("Mihomo Meta v1.20.0")
	if _, err := InstallUploadedMihomoBinary(context.Background(), fileName, managedPath, bytes.NewReader(managedContent)); err != nil {
		t.Fatalf("install managed fixture: %v", err)
	}
	tempPath, err := persistExecutableTempFile(managedDir, "poolnext-final-check-", fileName, bytes.NewReader([]byte(strings.ReplaceAll(string(managedContent), "v1.20.0", "v1.21.0"))))
	if err != nil {
		t.Fatalf("prepare replacement: %v", err)
	}

	beforeKernelReplaceForTest = func() {
		if removeErr := os.Remove(managedPath); removeErr != nil {
			t.Fatalf("remove managed target in replacement hook: %v", removeErr)
		}
		if linkErr := os.Link(externalPath, managedPath); linkErr != nil {
			t.Fatalf("replace managed target with external hard link: %v", linkErr)
		}
	}
	t.Cleanup(func() {
		beforeKernelReplaceForTest = nil
	})

	err = replaceExecutableFile(tempPath, managedPath)
	if !errors.Is(err, ErrExternalImmutableKernel) {
		t.Fatalf("expected final hard-link identity rejection, got %v", err)
	}
	assertKernelFileUnchanged(t, externalPath, externalBefore)
	if _, statErr := os.Stat(tempPath); statErr != nil {
		t.Fatalf("prepared replacement must remain after rejection: %v", statErr)
	}
}

func TestReplaceExecutableFileDoesNotClobberLateUnknownTarget(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RENAME_NOREPLACE final-window protection is implemented for Linux")
	}
	setupServiceTestDB(t)

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "mihomo")
	fileName, preparedContent := testExecutableFile("Mihomo Meta v1.20.0")
	tempPath, err := persistExecutableTempFile(targetDir, "poolnext-noreplace-", fileName, bytes.NewReader(preparedContent))
	if err != nil {
		t.Fatalf("prepare managed replacement: %v", err)
	}
	unknownContent := []byte("must remain untouched")
	beforeKernelRenameForTest = func() {
		if writeErr := os.WriteFile(targetPath, unknownContent, 0o600); writeErr != nil {
			t.Fatalf("create late unknown target: %v", writeErr)
		}
	}
	t.Cleanup(func() {
		beforeKernelRenameForTest = nil
	})

	err = replaceExecutableFile(tempPath, targetPath)
	if err == nil {
		t.Fatal("expected late target creation to abort no-replace install")
	}
	after, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		t.Fatalf("read late unknown target: %v", readErr)
	}
	if !bytes.Equal(after, unknownContent) {
		t.Fatalf("late unknown target was clobbered: %q", string(after))
	}
	if _, statErr := os.Stat(tempPath); statErr != nil {
		t.Fatalf("prepared binary must remain after no-replace rejection: %v", statErr)
	}
}

func TestReplaceExecutableFileRejectsLateExternalHardLinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard-link replacement fixture requires Unix semantics")
	}
	setupServiceTestDB(t)

	externalPath := writeTestMihomoBinary(t, "Mihomo Meta v1.19.24")
	registerExternalMihomo(t, externalPath)
	externalBefore := snapshotKernelFile(t, externalPath)

	managedDir := t.TempDir()
	managedPath := filepath.Join(managedDir, "mihomo")
	fileName, managedContent := testExecutableFile("Mihomo Meta v1.20.0")
	if _, err := InstallUploadedMihomoBinary(context.Background(), fileName, managedPath, bytes.NewReader(managedContent)); err != nil {
		t.Fatalf("install managed fixture: %v", err)
	}
	tempPath, err := persistExecutableTempFile(managedDir, "poolnext-late-external-", fileName, bytes.NewReader([]byte(strings.ReplaceAll(string(managedContent), "v1.20.0", "v1.21.0"))))
	if err != nil {
		t.Fatalf("prepare replacement: %v", err)
	}

	beforeKernelRenameForTest = func() {
		if removeErr := os.Remove(managedPath); removeErr != nil {
			t.Fatalf("remove managed target in final-window hook: %v", removeErr)
		}
		if linkErr := os.Link(externalPath, managedPath); linkErr != nil {
			t.Fatalf("replace managed target with external hard link in final-window hook: %v", linkErr)
		}
	}
	t.Cleanup(func() {
		beforeKernelRenameForTest = nil
	})

	err = replaceExecutableFile(tempPath, managedPath)
	if !errors.Is(err, ErrExternalImmutableKernel) {
		t.Fatalf("expected final-window external hard-link rejection, got %v", err)
	}
	assertKernelFileUnchanged(t, externalPath, externalBefore)
	managedInfo, statErr := os.Stat(managedPath)
	if statErr != nil {
		t.Fatalf("stat late external hard-link target: %v", statErr)
	}
	externalInfo, statErr := os.Stat(externalPath)
	if statErr != nil {
		t.Fatalf("stat external Mihomo fixture: %v", statErr)
	}
	if !os.SameFile(managedInfo, externalInfo) {
		t.Fatal("late external hard link must remain in place after rejection")
	}
	if _, statErr := os.Stat(tempPath); statErr != nil {
		t.Fatalf("prepared replacement must remain after rejection: %v", statErr)
	}
}

func TestInstallUploadedMihomoRejectsSymlinkedTargetParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("no-symlink directory traversal is implemented for Linux")
	}
	setupServiceTestDB(t)

	realParent := t.TempDir()
	symlinkParent := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(realParent, symlinkParent); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}
	fileName, content := testExecutableFile("Mihomo Meta v1.20.0")

	_, err := InstallUploadedMihomoBinary(context.Background(), fileName, filepath.Join(symlinkParent, "must-not-be-created", "mihomo"), bytes.NewReader(content))
	if !errors.Is(err, ErrUnsafeKernelPath) {
		t.Fatalf("expected unsafe parent rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(realParent, "must-not-be-created")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("directories must not be created through symlink parent, stat=%v", statErr)
	}
}

func TestConcurrentManagedInstallsKeepIdentityAndOptionsConsistent(t *testing.T) {
	setupServiceTestDB(t)

	targetPath := filepath.Join(t.TempDir(), "mihomo")
	versions := []string{
		"Mihomo Meta v1.20.1",
		"Mihomo Meta v1.20.2",
		"Mihomo Meta v1.20.3",
		"Mihomo Meta v1.20.4",
		"Mihomo Meta v1.20.5",
		"Mihomo Meta v1.20.6",
	}
	start := make(chan struct{})
	errorsByInstall := make(chan error, len(versions))
	var waitGroup sync.WaitGroup
	for _, version := range versions {
		version := version
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			fileName, content := testExecutableFile(version)
			_, err := InstallUploadedMihomoBinary(context.Background(), fileName, targetPath, bytes.NewReader(content))
			errorsByInstall <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errorsByInstall)
	for err := range errorsByInstall {
		if err != nil {
			t.Fatalf("concurrent managed install failed: %v", err)
		}
	}

	identity, err := captureKernelFileIdentity(targetPath)
	if err != nil {
		t.Fatalf("capture final managed identity: %v", err)
	}
	version, err := detectMihomoBinaryVersion(context.Background(), targetPath)
	if err != nil {
		t.Fatalf("detect final managed version: %v", err)
	}
	var record kernelBinaryIdentityRecord
	if err := model.DB.Where("registered_path = ?", identity.RegisteredPath).First(&record).Error; err != nil {
		t.Fatalf("load final managed identity: %v", err)
	}
	if record.Mode != KernelModeManaged || record.SHA256 != identity.SHA256 || record.Inode != identity.Inode {
		t.Fatalf("managed identity does not match final file: record=%+v identity=%+v", record, identity)
	}
	common.OptionMapRWMutex.RLock()
	optionPath := common.MihomoBinaryPath
	optionVersion := common.MihomoBinaryVersion
	optionSource := common.MihomoBinarySource
	common.OptionMapRWMutex.RUnlock()
	if optionPath != identity.RegisteredPath || optionVersion != version || optionSource != MihomoBinarySourceUpload {
		t.Fatalf("option snapshot is torn: path=%s version=%s source=%s final_version=%s", optionPath, optionVersion, optionSource, version)
	}
	var persistedOptions []model.Option
	if err := model.DB.Where("key IN ?", []string{"KernelType", "MihomoBinaryPath", "MihomoBinaryVersion", "MihomoBinarySource"}).Find(&persistedOptions).Error; err != nil {
		t.Fatalf("load persisted kernel options: %v", err)
	}
	persisted := make(map[string]string, len(persistedOptions))
	for _, option := range persistedOptions {
		persisted[option.Key] = option.Value
	}
	if persisted["KernelType"] != KernelTypeMihomo ||
		persisted["MihomoBinaryPath"] != identity.RegisteredPath ||
		persisted["MihomoBinaryVersion"] != version ||
		persisted["MihomoBinarySource"] != MihomoBinarySourceUpload {
		t.Fatalf("persisted kernel options are torn: %+v final_version=%s", persisted, version)
	}
}

func TestResolveExecutableInstallPathUsesWorkingDirectoryDefault(t *testing.T) {
	resolved, err := resolveExecutableInstallPath("", true)
	if err != nil {
		t.Fatalf("resolve executable install path: %v", err)
	}

	expectedPath, err := filepath.Abs(filepath.Join(".", "mihomo"))
	if err != nil {
		t.Fatalf("resolve expected path: %v", err)
	}
	if runtime.GOOS == "windows" {
		expectedPath += ".exe"
	}
	if resolved != expectedPath {
		t.Fatalf("unexpected default install path: got %s want %s", resolved, expectedPath)
	}
}

func testExecutableFile(version string) (string, []byte) {
	if runtime.GOOS == "windows" {
		return "mihomo-test.cmd", []byte("@echo off\r\necho " + version + "\r\n")
	}
	return "mihomo-test.sh", []byte("#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then\n  echo " + version + "\n  exit 0\nfi\nif [ \"$1\" = \"version\" ]; then\n  echo " + version + "\n  exit 0\nfi\necho " + version + "\n")
}

type kernelFileSnapshot struct {
	info    os.FileInfo
	content []byte
}

func snapshotKernelFile(t *testing.T, filePath string) kernelFileSnapshot {
	t.Helper()
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat kernel file: %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read kernel file: %v", err)
	}
	return kernelFileSnapshot{info: info, content: content}
}

func assertKernelFileUnchanged(t *testing.T, filePath string, before kernelFileSnapshot) {
	t.Helper()
	after := snapshotKernelFile(t, filePath)
	if !os.SameFile(before.info, after.info) {
		t.Fatal("kernel inode identity changed")
	}
	if before.info.Mode() != after.info.Mode() {
		t.Fatalf("kernel mode changed: before=%v after=%v", before.info.Mode(), after.info.Mode())
	}
	if before.info.Size() != after.info.Size() {
		t.Fatalf("kernel size changed: before=%d after=%d", before.info.Size(), after.info.Size())
	}
	if !before.info.ModTime().Equal(after.info.ModTime()) {
		t.Fatalf("kernel mtime changed: before=%v after=%v", before.info.ModTime(), after.info.ModTime())
	}
	if !bytes.Equal(before.content, after.content) {
		t.Fatal("kernel content changed")
	}
}

func writeTestMihomoBinary(t *testing.T, version string) string {
	t.Helper()
	fileName, content := testExecutableFile(version)
	filePath := filepath.Join(t.TempDir(), fileName)
	if err := os.WriteFile(filePath, content, 0o755); err != nil {
		t.Fatalf("write test Mihomo binary: %v", err)
	}
	return filePath
}

func registerExternalMihomo(t *testing.T, binaryPath string) *InstalledKernelBinary {
	t.Helper()
	result, err := InspectMihomoBinary(context.Background(), binaryPath)
	if err != nil {
		t.Fatalf("register external Mihomo binary: %v", err)
	}
	return result
}

func setMihomoBinaryStateForTest(t *testing.T, binaryPath string, source string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	originalPath := common.MihomoBinaryPath
	originalSource := common.MihomoBinarySource
	common.MihomoBinaryPath = binaryPath
	common.MihomoBinarySource = source
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap["MihomoBinaryPath"] = binaryPath
	common.OptionMap["MihomoBinarySource"] = source
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.MihomoBinaryPath = originalPath
		common.MihomoBinarySource = originalSource
		common.OptionMap["MihomoBinaryPath"] = originalPath
		common.OptionMap["MihomoBinarySource"] = originalSource
		common.OptionMapRWMutex.Unlock()
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
