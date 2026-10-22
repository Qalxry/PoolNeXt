package service

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"poolx/internal/pkg/common"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	KernelTypeMihomo  = "mihomo"
	KernelTypeXray    = "xray"
	KernelTypeSingbox = "singbox"

	MihomoBinarySourceExisting = "existing"
	MihomoBinarySourceUpload   = "upload"
	MihomoBinarySourceDownload = "download"

	KernelModeExternalImmutable = "external_immutable"
	KernelModeManaged           = "managed"
	KernelModeUnknown           = "unknown"
)

const (
	// maxMihomoReleaseArchiveBytes bounds the compressed release archive that is
	// buffered to a temporary file before the kernel binary is extracted from it.
	maxMihomoReleaseArchiveBytes int64 = 256 << 20
	// maxMihomoKernelBinaryBytes bounds the extracted kernel binary size.
	maxMihomoKernelBinaryBytes int64 = 256 << 20
)

var (
	ErrExternalImmutableKernel     = errors.New("external immutable Mihomo binary cannot be modified")
	ErrKernelOwnershipUnknown      = errors.New("Mihomo binary ownership is unknown")
	ErrKernelProtectionUnavailable = errors.New("Mihomo binary protection state is unavailable")
	ErrUnsafeKernelPath            = errors.New("unsafe Mihomo binary path")
	ErrKernelPathChanged           = errors.New("Mihomo binary path changed during verification")
)

var kernelHTTPClient = &http.Client{
	Timeout: 2 * time.Minute,
}

var execCommandContext = exec.CommandContext

var kernelStateMu sync.Mutex

var beforeKernelReplaceForTest func()

var beforeKernelRenameForTest func()

type InstalledKernelBinary struct {
	KernelType      string    `json:"kernel_type"`
	Mode            string    `json:"mode"`
	Immutable       bool      `json:"immutable"`
	InstallPath     string    `json:"install_path"`
	BinarySource    string    `json:"binary_source"`
	DetectedVersion string    `json:"detected_version"`
	FileName        string    `json:"file_name"`
	ReleaseTag      string    `json:"release_tag,omitempty"`
	InstalledAt     time.Time `json:"installed_at"`
}

func InspectMihomoBinary(ctx context.Context, installPath string) (*InstalledKernelBinary, error) {
	kernelStateMu.Lock()
	defer kernelStateMu.Unlock()
	return inspectMihomoBinaryLocked(ctx, installPath)
}

func inspectMihomoBinaryLocked(ctx context.Context, installPath string) (*InstalledKernelBinary, error) {
	resolvedPath, err := resolveExecutableInstallPath(installPath, false)
	if err != nil {
		return nil, err
	}
	before, err := captureKernelFileIdentity(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("检查 Mihomo 二进制文件失败: %v", err)
	}
	if !before.Exists {
		return nil, fmt.Errorf("未找到 Mihomo 二进制文件: %s", resolvedPath)
	}

	detectedVersion, err := detectMihomoBinaryVersion(ctx, before.CanonicalPath)
	if err != nil {
		return nil, err
	}
	after, err := captureKernelFileIdentity(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("复核 Mihomo 二进制文件失败: %v", err)
	}
	if !sameKernelFileIdentity(before, after) {
		return nil, ErrKernelPathChanged
	}

	return persistInstalledMihomoBinary(after, filepath.Base(resolvedPath), MihomoBinarySourceExisting, detectedVersion, "")
}

func InstallUploadedMihomoBinary(ctx context.Context, fileName string, installPath string, reader io.Reader) (*InstalledKernelBinary, error) {
	kernelStateMu.Lock()
	defer kernelStateMu.Unlock()
	return installUploadedMihomoBinaryLocked(ctx, fileName, installPath, reader)
}

func installUploadedMihomoBinaryLocked(ctx context.Context, fileName string, installPath string, reader io.Reader) (*InstalledKernelBinary, error) {
	resolvedPath, err := resolveExecutableInstallPath(installPath, true)
	if err != nil {
		return nil, err
	}
	if err = ensureManagedMihomoMutationAllowedLocked("upload", resolvedPath); err != nil {
		return nil, err
	}
	if err = ensureKernelTargetParent(filepath.Dir(resolvedPath)); err != nil {
		return nil, err
	}
	tempPath, err := persistExecutableTempFile(filepath.Dir(resolvedPath), "poolx-mihomo-upload-", fileName, reader)
	if err != nil {
		return nil, err
	}
	return installPreparedMihomoBinary(ctx, tempPath, resolvedPath, strings.TrimSpace(fileName), MihomoBinarySourceUpload, "")
}

func DownloadAndInstallMihomoBinary(ctx context.Context, installPath string) (*InstalledKernelBinary, error) {
	kernelStateMu.Lock()
	defer kernelStateMu.Unlock()
	return downloadAndInstallMihomoBinaryLocked(ctx, installPath)
}

func downloadAndInstallMihomoBinaryLocked(ctx context.Context, installPath string) (*InstalledKernelBinary, error) {
	resolvedPath, err := resolveExecutableInstallPath(installPath, true)
	if err != nil {
		return nil, err
	}
	if err = ensureManagedMihomoMutationAllowedLocked("download", resolvedPath); err != nil {
		return nil, err
	}
	if err = ensureKernelTargetParent(filepath.Dir(resolvedPath)); err != nil {
		return nil, err
	}
	release, err := fetchLatestMihomoRelease(ctx)
	if err != nil {
		return nil, err
	}
	asset, err := selectMihomoReleaseAsset(release.Assets)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(defaultContext(ctx), http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Mihomo 下载请求失败: %v", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "PoolNeXt-Server")

	resp, err := kernelHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载 Mihomo 二进制失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 Mihomo 二进制失败: %s", resp.Status)
	}

	tempPath, installedFileName, err := prepareMihomoReleaseBinary(asset.Name, resp.Body, filepath.Dir(resolvedPath))
	if err != nil {
		return nil, err
	}
	return installPreparedMihomoBinary(ctx, tempPath, resolvedPath, installedFileName, MihomoBinarySourceDownload, release.TagName)
}

// prepareMihomoReleaseBinary turns a downloaded release asset body into an executable
// temporary file and reports the name recorded for the installed binary.
//
// Linux/macOS releases publish the raw binary as `.gz`, while Windows releases ship a
// `.zip` archive that contains the kernel executable next to other files.
func prepareMihomoReleaseBinary(assetName string, body io.Reader, tempDir string) (string, string, error) {
	trimmedName := strings.TrimSpace(assetName)
	switch lowerName := strings.ToLower(trimmedName); {
	case strings.HasSuffix(lowerName, ".gz"):
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			return "", "", fmt.Errorf("解压 Mihomo 发行包失败: %v", err)
		}
		defer func() { _ = gzipReader.Close() }()

		tempPath, err := persistExecutableTempFile(tempDir, "poolx-mihomo-download-", trimmedName, gzipReader)
		if err != nil {
			return "", "", err
		}
		return tempPath, trimmedName, nil
	case strings.HasSuffix(lowerName, ".zip"):
		return extractMihomoBinaryFromZip(body, tempDir)
	default:
		return "", "", fmt.Errorf("不支持的 Mihomo 发行包格式: %s", trimmedName)
	}
}

func extractMihomoBinaryFromZip(body io.Reader, tempDir string) (string, string, error) {
	archiveFile, err := os.CreateTemp(tempDir, "poolx-mihomo-download-*.zip")
	if err != nil {
		return "", "", fmt.Errorf("创建 Mihomo 发行包临时文件失败: %v", err)
	}
	archivePath := archiveFile.Name()
	defer func() { _ = os.Remove(archivePath) }()

	written, copyErr := io.Copy(archiveFile, io.LimitReader(body, maxMihomoReleaseArchiveBytes+1))
	closeErr := archiveFile.Close()
	if copyErr != nil {
		return "", "", fmt.Errorf("保存 Mihomo 发行包失败: %v", copyErr)
	}
	if closeErr != nil {
		return "", "", fmt.Errorf("关闭 Mihomo 发行包临时文件失败: %v", closeErr)
	}
	if written > maxMihomoReleaseArchiveBytes {
		return "", "", fmt.Errorf("Mihomo 发行包超过大小上限 %d 字节", maxMihomoReleaseArchiveBytes)
	}

	archiveReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", "", fmt.Errorf("解压 Mihomo 发行包失败: %v", err)
	}
	defer func() { _ = archiveReader.Close() }()

	entry, err := findMihomoExecutableInZip(archiveReader.File)
	if err != nil {
		return "", "", err
	}
	if entry.UncompressedSize64 > uint64(maxMihomoKernelBinaryBytes) {
		return "", "", fmt.Errorf("Mihomo 内核超过大小上限 %d 字节", maxMihomoKernelBinaryBytes)
	}
	entryReader, err := entry.Open()
	if err != nil {
		return "", "", fmt.Errorf("读取 Mihomo 发行包中的内核失败: %v", err)
	}
	defer func() { _ = entryReader.Close() }()

	tempPath, err := persistExecutableTempFile(tempDir, "poolx-mihomo-download-", entry.Name, entryReader)
	if err != nil {
		return "", "", err
	}
	return tempPath, entry.Name, nil
}

func findMihomoExecutableInZip(files []*zip.File) (*zip.File, error) {
	candidates := make([]*zip.File, 0, len(files))
	for _, file := range files {
		if file.FileInfo().IsDir() {
			continue
		}
		baseName := strings.ToLower(filepath.Base(file.Name))
		if baseName == "mihomo" || strings.HasSuffix(baseName, ".exe") {
			candidates = append(candidates, file)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("Mihomo 发行包中未找到内核可执行文件")
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	for _, file := range candidates {
		baseName := strings.ToLower(filepath.Base(file.Name))
		if baseName == "mihomo" || baseName == "mihomo.exe" || strings.HasPrefix(baseName, "mihomo") {
			return file, nil
		}
	}
	return nil, fmt.Errorf("Mihomo 发行包中包含多个可执行文件，无法确定安装目标")
}

func fetchLatestMihomoRelease(ctx context.Context) (*githubReleaseResponse, error) {
	url := fmt.Sprintf(githubReleasesAPIBase+"/latest", common.DefaultMihomoReleaseRepo)
	req, err := http.NewRequestWithContext(defaultContext(ctx), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Mihomo 版本查询请求失败: %v", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PoolNeXt-Server")

	resp, err := kernelHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询 Mihomo 官方版本失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询 Mihomo 官方版本失败: %s", resp.Status)
	}

	var release githubReleaseResponse
	if err = json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析 Mihomo 版本响应失败: %v", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return nil, fmt.Errorf("未获取到有效的 Mihomo 版本标签")
	}
	return &release, nil
}

func selectMihomoReleaseAsset(assets []githubAsset) (*githubAsset, error) {
	return selectMihomoReleaseAssetFor(assets, runtime.GOOS, runtime.GOARCH)
}

func selectMihomoReleaseAssetFor(assets []githubAsset, goos string, goarch string) (*githubAsset, error) {
	if len(assets) == 0 {
		return nil, fmt.Errorf("Mihomo 官方发布未包含可下载资产")
	}

	normalizedAssets := make([]githubAsset, 0, len(assets))
	for _, asset := range assets {
		name := strings.ToLower(strings.TrimSpace(asset.Name))
		if name == "" || strings.TrimSpace(asset.BrowserDownloadURL) == "" {
			continue
		}
		if strings.Contains(name, "sha256") || strings.HasSuffix(name, ".txt") {
			continue
		}
		if !isSupportedMihomoBinaryAsset(name) {
			continue
		}
		normalizedAssets = append(normalizedAssets, asset)
	}

	normalizedGOOS := strings.ToLower(strings.TrimSpace(goos))
	for _, archCandidate := range preferredMihomoArchKeywordsFor(goarch) {
		for _, asset := range normalizedAssets {
			name := strings.ToLower(strings.TrimSpace(asset.Name))
			if !strings.Contains(name, "mihomo-") || !strings.Contains(name, normalizedGOOS) {
				continue
			}
			if strings.Contains(name, archCandidate) {
				candidate := asset
				return &candidate, nil
			}
		}
	}

	return nil, fmt.Errorf("未找到适用于当前平台 %s/%s 的 Mihomo 发行包", normalizedGOOS, strings.ToLower(strings.TrimSpace(goarch)))
}

func isSupportedMihomoBinaryAsset(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(name, "mihomo-") {
		return false
	}
	return strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".zip")
}

func preferredMihomoArchKeywords() []string {
	return preferredMihomoArchKeywordsFor(runtime.GOARCH)
}

func preferredMihomoArchKeywordsFor(goarch string) []string {
	switch strings.ToLower(strings.TrimSpace(goarch)) {
	case "amd64":
		return []string{"amd64-v3", "amd64-v2", "amd64-v1", "amd64-compatible", "amd64"}
	case "arm64":
		return []string{"arm64"}
	case "386":
		return []string{"386"}
	case "arm":
		goarm := strings.TrimSpace(os.Getenv("GOARM"))
		keywords := make([]string, 0, 4)
		if goarm != "" {
			keywords = append(keywords, "armv"+goarm)
		}
		return append(keywords, "armv7", "armv6", "arm")
	default:
		return []string{strings.ToLower(strings.TrimSpace(goarch))}
	}
}

func installPreparedMihomoBinary(ctx context.Context, tempPath string, installPath string, fileName string, source string, releaseTag string) (*InstalledKernelBinary, error) {
	if err := ensureManagedMihomoMutationAllowedLocked("install", installPath); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}

	preparedBefore, err := captureKernelFileIdentity(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("检查待安装 Mihomo 二进制失败: %v", err)
	}
	if !preparedBefore.Exists {
		return nil, fmt.Errorf("待安装 Mihomo 二进制消失: %s", tempPath)
	}
	detectedVersion, err := detectMihomoBinaryVersion(ctx, preparedBefore.CanonicalPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	preparedAfter, err := captureKernelFileIdentity(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("复核待安装 Mihomo 二进制失败: %v", err)
	}
	if !sameKernelFileIdentity(preparedBefore, preparedAfter) {
		_ = os.Remove(tempPath)
		return nil, ErrKernelPathChanged
	}

	if strings.TrimSpace(releaseTag) != "" && !mihomoVersionMatchesRelease(detectedVersion, releaseTag) {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("Mihomo 版本校验失败：release=%s，binary=%s", strings.TrimSpace(releaseTag), strings.TrimSpace(detectedVersion))
	}

	if err = ensureKernelTargetParent(filepath.Dir(installPath)); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	if err = replaceExecutableFileLocked(tempPath, installPath, &preparedAfter); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}

	installedIdentity, err := captureKernelFileIdentity(installPath)
	if err != nil {
		return nil, fmt.Errorf("复核已安装 Mihomo 二进制失败: %v", err)
	}
	if !installedIdentity.Exists {
		return nil, fmt.Errorf("已安装 Mihomo 二进制消失: %s", installPath)
	}
	return persistInstalledMihomoBinary(installedIdentity, fileName, source, detectedVersion, releaseTag)
}

func resolveExecutableInstallPath(installPath string, allowDefault bool) (string, error) {
	resolved := strings.TrimSpace(installPath)
	if resolved == "" {
		if !allowDefault {
			return "", fmt.Errorf("请先填写 Mihomo 二进制文件路径")
		}
		resolved = defaultMihomoInstallPath()
	}
	absolutePath, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("解析 Mihomo 二进制路径失败: %v", err)
	}
	if runtime.GOOS == "windows" && filepath.Ext(absolutePath) == "" {
		absolutePath += ".exe"
	}
	if strings.TrimSpace(filepath.Base(absolutePath)) == "" {
		return "", fmt.Errorf("Mihomo 二进制文件路径无效")
	}
	return absolutePath, nil
}

func defaultMihomoInstallPath() string {
	fileName := "mihomo"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	return filepath.Join(".", fileName)
}

func persistExecutableTempFile(tempDir string, patternPrefix string, fileName string, reader io.Reader) (string, error) {
	suffix := filepath.Ext(strings.TrimSpace(fileName))
	if runtime.GOOS == "windows" && suffix == "" {
		suffix = ".exe"
	}
	if strings.TrimSpace(tempDir) == "" {
		tempDir = os.TempDir()
	}
	tempFile, err := os.CreateTemp(tempDir, patternPrefix+"*"+suffix)
	if err != nil {
		return "", fmt.Errorf("创建临时二进制文件失败: %v", err)
	}
	tempPath := tempFile.Name()
	if _, err = io.Copy(tempFile, reader); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("写入临时二进制文件失败: %v", err)
	}
	if err = tempFile.Chmod(0o755); err != nil && runtime.GOOS != "windows" {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("设置临时二进制文件权限失败: %v", err)
	}
	if err = tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("同步临时二进制文件失败: %v", err)
	}
	if err = tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("关闭临时二进制文件失败: %v", err)
	}
	return tempPath, nil
}

func detectCommandVersion(ctx context.Context, filePath string, args ...string) (string, error) {
	commandCtx := defaultContext(ctx)
	cmd := execCommandContext(commandCtx, filePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed, nil
		}
	}
	return "", fmt.Errorf("命令未返回有效版本号")
}

func detectMihomoBinaryVersion(ctx context.Context, filePath string) (string, error) {
	var lastErr error
	for _, args := range [][]string{{"-v"}, {"version"}, {"--version"}} {
		version, err := detectCommandVersion(ctx, filePath, args...)
		if err != nil {
			lastErr = err
			continue
		}
		if strings.Contains(strings.ToLower(version), "mihomo") {
			return version, nil
		}
		lastErr = fmt.Errorf("上传文件未表现为 Mihomo 二进制，版本输出为: %s", version)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("未能检测到 Mihomo 版本")
	}
	return "", fmt.Errorf("检查 Mihomo 二进制版本失败: %v", lastErr)
}

func persistInstalledMihomoBinary(identity kernelFileIdentity, fileName string, source string, detectedVersion string, releaseTag string) (*InstalledKernelBinary, error) {
	mode := mihomoKernelModeForSource(source)
	if mode == KernelModeUnknown {
		return nil, fmt.Errorf("%w: source=%q", ErrKernelOwnershipUnknown, strings.TrimSpace(source))
	}
	if !identity.Exists {
		return nil, fmt.Errorf("cannot persist missing Mihomo binary identity: %s", identity.RegisteredPath)
	}
	normalizedSource := strings.ToLower(strings.TrimSpace(source))
	options := map[string]string{
		"KernelType":          KernelTypeMihomo,
		"MihomoBinaryPath":    identity.RegisteredPath,
		"MihomoBinaryVersion": detectedVersion,
		"MihomoBinarySource":  normalizedSource,
	}
	if err := persistKernelState(newKernelIdentityRecord(identity, mode, normalizedSource), options); err != nil {
		return nil, fmt.Errorf("持久化 Mihomo 内核状态失败: %w", err)
	}
	updateMihomoOptionSnapshot(identity.RegisteredPath, detectedVersion, normalizedSource)

	return &InstalledKernelBinary{
		KernelType:      KernelTypeMihomo,
		Mode:            mode,
		Immutable:       mode == KernelModeExternalImmutable,
		InstallPath:     identity.RegisteredPath,
		BinarySource:    normalizedSource,
		DetectedVersion: detectedVersion,
		FileName:        strings.TrimSpace(fileName),
		ReleaseTag:      strings.TrimSpace(releaseTag),
		InstalledAt:     time.Now(),
	}, nil
}

func updateMihomoOptionSnapshot(installPath string, detectedVersion string, source string) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.KernelType = KernelTypeMihomo
	common.MihomoBinaryPath = installPath
	common.MihomoBinaryVersion = detectedVersion
	common.MihomoBinarySource = source
	common.OptionMap["KernelType"] = KernelTypeMihomo
	common.OptionMap["MihomoBinaryPath"] = installPath
	common.OptionMap["MihomoBinaryVersion"] = detectedVersion
	common.OptionMap["MihomoBinarySource"] = source
}

func mihomoVersionMatchesRelease(version string, releaseTag string) bool {
	normalizedVersion := strings.ToLower(strings.TrimSpace(version))
	normalizedTag := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(releaseTag, "v")))
	if normalizedVersion == "" || normalizedTag == "" {
		return false
	}
	return strings.Contains(normalizedVersion, normalizedTag)
}

func replaceExecutableFile(tempPath string, targetPath string) error {
	kernelStateMu.Lock()
	defer kernelStateMu.Unlock()
	return replaceExecutableFileLocked(tempPath, targetPath, nil)
}

func replaceExecutableFileLocked(tempPath string, targetPath string, expectedPrepared *kernelFileIdentity) error {
	preparedIdentity, err := captureKernelFileIdentity(tempPath)
	if err != nil {
		return fmt.Errorf("检查待安装 Mihomo 二进制失败: %w", err)
	}
	if !preparedIdentity.Exists {
		return fmt.Errorf("待安装 Mihomo 二进制消失: %s", tempPath)
	}
	if expectedPrepared != nil && !sameKernelFileIdentity(*expectedPrepared, preparedIdentity) {
		return ErrKernelPathChanged
	}
	if err := ensureManagedMihomoMutationAllowedLocked("replace", targetPath); err != nil {
		return err
	}
	if beforeKernelReplaceForTest != nil {
		beforeKernelReplaceForTest()
	}
	if beforeKernelRenameForTest != nil {
		beforeKernelRenameForTest()
	}
	expectedParent, err := captureKernelDirectoryIdentity(filepath.Dir(targetPath))
	if err != nil {
		return err
	}
	expectedTarget, err := authorizeManagedMihomoMutationLocked("replace-final", targetPath)
	if err != nil {
		return err
	}
	finalPreparedIdentity, err := captureKernelFileIdentity(tempPath)
	if err != nil {
		return fmt.Errorf("最终复核待安装 Mihomo 二进制失败: %w", err)
	}
	if !sameKernelFileIdentity(preparedIdentity, finalPreparedIdentity) {
		return ErrKernelPathChanged
	}
	if err := secureReplaceExecutableFile(tempPath, targetPath, finalPreparedIdentity, expectedParent, expectedTarget); err != nil {
		return err
	}
	return nil
}

func mihomoKernelModeForSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case MihomoBinarySourceExisting:
		return KernelModeExternalImmutable
	case MihomoBinarySourceUpload, MihomoBinarySourceDownload:
		return KernelModeManaged
	default:
		return KernelModeUnknown
	}
}

func ensureManagedMihomoMutationAllowed(operation string, targetPath string) error {
	kernelStateMu.Lock()
	defer kernelStateMu.Unlock()
	return ensureManagedMihomoMutationAllowedLocked(operation, targetPath)
}

func ensureManagedMihomoMutationAllowedLocked(operation string, targetPath string) error {
	_, err := authorizeManagedMihomoMutationLocked(operation, targetPath)
	return err
}

func authorizeManagedMihomoMutationLocked(operation string, targetPath string) (kernelFileIdentity, error) {
	targetIdentity, err := captureKernelFileIdentity(targetPath)
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("验证 Mihomo 目标路径失败: %w", err)
	}
	records, err := loadKernelIdentityRecords()
	if err != nil {
		return kernelFileIdentity{}, fmt.Errorf("%w: %v", ErrKernelProtectionUnavailable, err)
	}

	managedPathClaimed := false
	for _, record := range records {
		if record.KernelType != KernelTypeMihomo || record.Mode != KernelModeExternalImmutable {
			continue
		}
		pathMatch := kernelRecordPathMatches(record, targetIdentity)
		objectMatch := kernelRecordObjectMatches(record, targetIdentity)
		if pathMatch || objectMatch {
			return kernelFileIdentity{}, fmt.Errorf("%w: operation=%s path=%s", ErrExternalImmutableKernel, strings.TrimSpace(operation), record.RegisteredPath)
		}
	}
	for _, record := range records {
		if record.KernelType != KernelTypeMihomo || record.Mode != KernelModeManaged {
			continue
		}
		pathMatch := kernelRecordPathMatches(record, targetIdentity)
		managedPathClaimed = managedPathClaimed || pathMatch
		if kernelRecordOwnsManagedTarget(record, targetIdentity) {
			return targetIdentity, nil
		}
	}
	if managedPathClaimed && targetIdentity.Exists {
		return kernelFileIdentity{}, fmt.Errorf("%w: operation=%s path=%s", ErrKernelOwnershipUnknown, strings.TrimSpace(operation), targetIdentity.RegisteredPath)
	}

	common.OptionMapRWMutex.RLock()
	currentPath := strings.TrimSpace(common.MihomoBinaryPath)
	currentSource := strings.TrimSpace(common.MihomoBinarySource)
	common.OptionMapRWMutex.RUnlock()
	if currentPath != "" {
		currentIdentity, identityErr := captureKernelFileIdentity(currentPath)
		if identityErr != nil {
			return kernelFileIdentity{}, fmt.Errorf("%w: validate legacy Mihomo identity: %v", ErrKernelProtectionUnavailable, identityErr)
		}
		if kernelFileIdentitiesReferToSameTarget(currentIdentity, targetIdentity) {
			mode := mihomoKernelModeForSource(currentSource)
			if mode == KernelModeUnknown {
				return kernelFileIdentity{}, fmt.Errorf("%w: operation=%s path=%s source=%q", ErrKernelOwnershipUnknown, strings.TrimSpace(operation), currentIdentity.RegisteredPath, currentSource)
			}
			if err := persistKernelIdentityOnly(newKernelIdentityRecord(currentIdentity, mode, currentSource)); err != nil {
				return kernelFileIdentity{}, fmt.Errorf("%w: persist legacy Mihomo identity: %v", ErrKernelProtectionUnavailable, err)
			}
			if mode == KernelModeExternalImmutable {
				return kernelFileIdentity{}, fmt.Errorf("%w: operation=%s path=%s", ErrExternalImmutableKernel, strings.TrimSpace(operation), currentIdentity.RegisteredPath)
			}
			return targetIdentity, nil
		}
	}
	if targetIdentity.Exists {
		return kernelFileIdentity{}, fmt.Errorf("%w: operation=%s path=%s", ErrKernelOwnershipUnknown, strings.TrimSpace(operation), targetIdentity.RegisteredPath)
	}
	return targetIdentity, nil
}

func kernelFileIdentitiesReferToSameTarget(left kernelFileIdentity, right kernelFileIdentity) bool {
	if pathEqual(left.RegisteredPath, right.RegisteredPath) ||
		pathEqual(left.RegisteredPath, right.CanonicalPath) ||
		pathEqual(left.CanonicalPath, right.RegisteredPath) ||
		pathEqual(left.CanonicalPath, right.CanonicalPath) {
		return true
	}
	if !left.Exists || !right.Exists {
		return false
	}
	if left.DeviceID != 0 && left.Inode != 0 && right.DeviceID != 0 && right.Inode != 0 {
		return left.DeviceID == right.DeviceID && left.Inode == right.Inode
	}
	return left.SHA256 != "" &&
		left.SHA256 == right.SHA256 &&
		left.Size == right.Size &&
		left.FileMode == right.FileMode &&
		left.ModTimeUnixNano == right.ModTimeUnixNano
}

func kernelPathsReferToSameFile(leftPath string, rightPath string) (bool, error) {
	leftAbsolute, err := filepath.Abs(strings.TrimSpace(leftPath))
	if err != nil {
		return false, err
	}
	rightAbsolute, err := filepath.Abs(strings.TrimSpace(rightPath))
	if err != nil {
		return false, err
	}

	leftAbsolute = filepath.Clean(leftAbsolute)
	rightAbsolute = filepath.Clean(rightAbsolute)
	if pathEqual(leftAbsolute, rightAbsolute) {
		return true, nil
	}

	leftCanonical, err := canonicalKernelPath(leftAbsolute)
	if err != nil {
		return false, err
	}
	rightCanonical, err := canonicalKernelPath(rightAbsolute)
	if err != nil {
		return false, err
	}
	if pathEqual(leftCanonical, rightCanonical) {
		return true, nil
	}

	leftInfo, leftErr := os.Stat(leftAbsolute)
	rightInfo, rightErr := os.Stat(rightAbsolute)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo), nil
	}
	if leftErr != nil && !os.IsNotExist(leftErr) {
		return false, leftErr
	}
	if rightErr != nil && !os.IsNotExist(rightErr) {
		return false, rightErr
	}
	return false, nil
}

func canonicalKernelPath(filePath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filePath)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(filePath))
	if parentErr == nil {
		return filepath.Clean(filepath.Join(resolvedParent, filepath.Base(filePath))), nil
	}
	if os.IsNotExist(parentErr) {
		return filepath.Clean(filePath), nil
	}
	return "", parentErr
}

func pathEqual(leftPath string, rightPath string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(leftPath, rightPath)
	}
	return leftPath == rightPath
}

func defaultContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
