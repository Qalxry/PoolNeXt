package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSessionSecretFile(t *testing.T) {
	secret := strings.Repeat("a1B2c3D4", 4)
	path := filepath.Join(t.TempDir(), "poolnext-secret")
	if err := os.WriteFile(path, []byte("  "+secret+"\r\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	loaded, err := LoadSessionSecretFile(path)
	if err != nil {
		t.Fatalf("expected secret file to load: %v", err)
	}
	if loaded != secret {
		t.Fatalf("unexpected secret value: %q", loaded)
	}
}

func TestLoadSessionSecretFileRejectsInvalidFiles(t *testing.T) {
	emptyPath := filepath.Join(t.TempDir(), "empty-secret")
	if err := os.WriteFile(emptyPath, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("write empty secret file: %v", err)
	}

	if _, err := LoadSessionSecretFile(emptyPath); err == nil {
		t.Fatal("expected whitespace-only secret file to be rejected")
	}
	if _, err := LoadSessionSecretFile(""); err == nil {
		t.Fatal("expected empty path to be rejected")
	}
	if _, err := LoadSessionSecretFile(filepath.Join(t.TempDir(), "missing-secret")); err == nil {
		t.Fatal("expected missing secret file to be rejected")
	}
}

func TestLoadSessionSecretFromFilePrefersEnvironment(t *testing.T) {
	t.Setenv("SESSION_SECRET", "environment-secret")
	path := filepath.Join(t.TempDir(), "poolnext-secret")
	if err := os.WriteFile(path, []byte(strings.Repeat("file-secret-", 4)), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	secret, loaded, err := loadSessionSecretFromFile(path)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	if loaded || secret != "" {
		t.Fatalf("expected environment SESSION_SECRET to win, got loaded=%v secret=%q", loaded, secret)
	}
}

func TestLoadSessionSecretFromFileUsesEnvVariablePath(t *testing.T) {
	t.Setenv("SESSION_SECRET", "")
	secret := strings.Repeat("EnvFileSecret", 4)
	path := filepath.Join(t.TempDir(), "poolnext-secret")
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}
	t.Setenv("SESSION_SECRET_FILE", path)

	loaded, ok, err := loadSessionSecretFromFile("")
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	if !ok || loaded != secret {
		t.Fatalf("expected SESSION_SECRET_FILE to be used, got ok=%v secret=%q", ok, loaded)
	}
}

func TestApplyDataDirDefaults(t *testing.T) {
	restore := snapshotPathState()
	t.Cleanup(restore)
	t.Setenv("SQLITE_PATH", "")
	t.Setenv("UPLOAD_PATH", "")

	dataDir := t.TempDir()
	if err := applyDataDirDefaults(dataDir); err != nil {
		t.Fatalf("apply data dir: %v", err)
	}

	expectedDir, err := filepath.Abs(dataDir)
	if err != nil {
		t.Fatalf("resolve expected data dir: %v", err)
	}
	if DataDir != expectedDir {
		t.Fatalf("unexpected DataDir: %q", DataDir)
	}
	if SQLitePath != filepath.Join(expectedDir, "poolx.db") {
		t.Fatalf("unexpected SQLitePath: %q", SQLitePath)
	}
	if UploadPath != filepath.Join(expectedDir, "upload") {
		t.Fatalf("unexpected UploadPath: %q", UploadPath)
	}
	if RuntimeDataRoot != filepath.Join(expectedDir, "data") {
		t.Fatalf("unexpected RuntimeDataRoot: %q", RuntimeDataRoot)
	}
	if _, statErr := os.Stat(expectedDir); statErr != nil {
		t.Fatalf("data directory was not created: %v", statErr)
	}
}

func TestApplyDataDirDefaultsKeepsExplicitPaths(t *testing.T) {
	restore := snapshotPathState()
	t.Cleanup(restore)
	SQLitePath = "/custom/poolx.db"
	UploadPath = "/custom/upload"
	t.Setenv("SQLITE_PATH", "/custom/poolx.db")
	t.Setenv("UPLOAD_PATH", "/custom/upload")

	dataDir := t.TempDir()
	if err := applyDataDirDefaults(dataDir); err != nil {
		t.Fatalf("apply data dir: %v", err)
	}
	if SQLitePath != "/custom/poolx.db" {
		t.Fatalf("explicit SQLITE_PATH was overridden: %q", SQLitePath)
	}
	if UploadPath != "/custom/upload" {
		t.Fatalf("explicit UPLOAD_PATH was overridden: %q", UploadPath)
	}
	expectedDir, err := filepath.Abs(dataDir)
	if err != nil {
		t.Fatalf("resolve expected data dir: %v", err)
	}
	if RuntimeDataRoot != filepath.Join(expectedDir, "data") {
		t.Fatalf("unexpected RuntimeDataRoot: %q", RuntimeDataRoot)
	}
}

func TestApplyDataDirDefaultsIgnoresBlankDir(t *testing.T) {
	restore := snapshotPathState()
	t.Cleanup(restore)

	if err := applyDataDirDefaults("   "); err != nil {
		t.Fatalf("blank data dir should be a no-op: %v", err)
	}
	if DataDir != "" {
		t.Fatalf("expected DataDir to stay empty, got %q", DataDir)
	}
	if RuntimeDataRoot != "data" {
		t.Fatalf("expected RuntimeDataRoot to stay relative, got %q", RuntimeDataRoot)
	}
}

func snapshotPathState() func() {
	previousSQLitePath := SQLitePath
	previousUploadPath := UploadPath
	previousDataDir := DataDir
	previousRuntimeDataRoot := RuntimeDataRoot
	return func() {
		SQLitePath = previousSQLitePath
		UploadPath = previousUploadPath
		DataDir = previousDataDir
		RuntimeDataRoot = previousRuntimeDataRoot
	}
}
