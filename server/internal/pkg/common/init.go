package common

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

var (
	Port                  = flag.Int("port", 17778, "the listening port")
	PrintVersion          = flag.Bool("version", false, "print version and exit")
	PrintHelp             = flag.Bool("help", false, "print help and exit")
	ResetPassword         = flag.Bool("reset-password", false, "interactively reset the admin password and exit")
	LogDir                = flag.String("log-dir", "", "specify the log directory")
	DataDirPath           = flag.String("data-dir", "", "base directory for the database, uploads and runtime data")
	SessionSecretPathFlag = flag.String("session-secret-file", "", "read SESSION_SECRET from this file")
)

// UploadPath Maybe override by ENV_VAR
var UploadPath = "upload"

// DataDir is the absolute data directory when --data-dir is set.
var DataDir string

// RuntimeDataRoot is the base directory for generated runtime configuration.
var RuntimeDataRoot = "data"

func printHelp() {
	fmt.Println("PoolNeXt " + Version + " - Proxy Pool Control Plane backend.")
	fmt.Println("Usage: poolnext-server [--port <port>] [--data-dir <dir>] [--session-secret-file <file>] [--log-dir <dir>] [--version] [--reset-password] [--help]")
}

// LoadSessionSecretFile reads a SESSION_SECRET value from path and trims surrounding whitespace.
func LoadSessionSecretFile(path string) (string, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return "", fmt.Errorf("session secret file path is empty")
	}
	content, err := os.ReadFile(trimmedPath)
	if err != nil {
		return "", fmt.Errorf("read session secret file: %w", err)
	}
	secret := strings.TrimSpace(string(content))
	if secret == "" {
		return "", fmt.Errorf("session secret file is empty: %s", trimmedPath)
	}
	return secret, nil
}

func loadSessionSecretFromFile(explicitPath string) (string, bool, error) {
	if os.Getenv("SESSION_SECRET") != "" {
		return "", false, nil
	}
	path := strings.TrimSpace(explicitPath)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("SESSION_SECRET_FILE"))
	}
	if path == "" {
		return "", false, nil
	}
	secret, err := LoadSessionSecretFile(path)
	if err != nil {
		return "", false, err
	}
	return secret, true, nil
}

// applyDataDirDefaults derives the default database, upload and runtime paths from dataDir.
// Explicit SQLITE_PATH / UPLOAD_PATH environment variables still take precedence.
func applyDataDirDefaults(dataDir string) error {
	trimmed := strings.TrimSpace(dataDir)
	if trimmed == "" {
		return nil
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}
	if err = os.MkdirAll(absolute, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	DataDir = absolute
	if strings.TrimSpace(os.Getenv("SQLITE_PATH")) == "" {
		SQLitePath = filepath.Join(absolute, "poolx.db")
	}
	if strings.TrimSpace(os.Getenv("UPLOAD_PATH")) == "" {
		UploadPath = filepath.Join(absolute, "upload")
	}
	RuntimeDataRoot = filepath.Join(absolute, "data")
	return nil
}

func init() {
	executableName := strings.ToLower(filepath.Base(os.Args[0]))
	if !strings.Contains(executableName, ".test") {
		flag.Parse()
	}

	if *PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	if *PrintHelp {
		printHelp()
		os.Exit(0)
	}

	if os.Getenv("SESSION_SECRET") != "" {
		SessionSecret = os.Getenv("SESSION_SECRET")
	} else if secret, loaded, err := loadSessionSecretFromFile(*SessionSecretPathFlag); err != nil {
		slog.Error("load session secret file failed", "error", err)
		os.Exit(1)
	} else if loaded {
		SessionSecret = secret
		_ = os.Setenv("SESSION_SECRET", secret)
	}
	if os.Getenv("SQLITE_PATH") != "" {
		SQLitePath = os.Getenv("SQLITE_PATH")
	}
	if os.Getenv("SQL_DSN") != "" {
		SQLDSN = os.Getenv("SQL_DSN")
	}
	if os.Getenv("DSN") != "" {
		SQLDSN = os.Getenv("DSN")
	}
	if os.Getenv("UPLOAD_PATH") != "" {
		UploadPath = os.Getenv("UPLOAD_PATH")
	}
	if err := applyDataDirDefaults(*DataDirPath); err != nil {
		slog.Error("resolve data directory failed", "error", err)
		os.Exit(1)
	}
	SetLogLevel(os.Getenv("LOG_LEVEL"))
	if *LogDir != "" {
		var err error
		*LogDir, err = filepath.Abs(*LogDir)
		if err != nil {
			slog.Error("resolve log directory failed", "error", err)
			os.Exit(1)
		}
		if _, err := os.Stat(*LogDir); os.IsNotExist(err) {
			err = os.Mkdir(*LogDir, 0777)
			if err != nil {
				slog.Error("create log directory failed", "error", err)
				os.Exit(1)
			}
		}
	}
	if _, err := os.Stat(UploadPath); os.IsNotExist(err) {
		_ = os.Mkdir(UploadPath, 0777)
	}
}
