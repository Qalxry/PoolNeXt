package kernel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetMihomoConnectionsDecodesControllerSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer controller-secret" {
			t.Fatalf("missing controller authorization header")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"downloadTotal":20,"uploadTotal":10,"memory":30,"connections":[{"id":"connection-1","metadata":{"network":"tcp","sourceIP":"127.0.0.1","sourcePort":"50000","host":"example.com","destinationPort":"443","inboundName":"entry"},"upload":4,"download":5,"start":"2026-07-16T00:00:00Z","chains":["node","group"],"providerChains":[],"rule":"DOMAIN","rulePayload":"example.com"}]}`))
	}))
	defer server.Close()

	payload, err := GetMihomoConnections(context.Background(), strings.TrimPrefix(server.URL, "http://"), "controller-secret")
	if err != nil {
		t.Fatalf("get connections: %v", err)
	}
	if len(payload.Connections) != 1 || payload.Connections[0].Metadata.Host != "example.com" {
		t.Fatalf("unexpected snapshot: %+v", payload)
	}
}

func TestStartMihomoProcessUsesAbsolutePaths(t *testing.T) {
	tempDir := t.TempDir()
	binaryPath := filepath.Join(tempDir, "fake-mihomo.sh")
	argsLogPath := filepath.Join(tempDir, "args.log")
	configPath := filepath.Join(tempDir, "config.yaml")
	workDir := filepath.Join(tempDir, "runtime")

	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("external-controller: 127.0.0.1:19090\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(argsLogPath) + "\n"
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake mihomo: %v", err)
	}

	cmd, err := StartMihomoProcess(binaryPath, workDir, configPath, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("start mihomo process: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait mihomo process: %v", err)
	}

	content, err := os.ReadFile(argsLogPath)
	if err != nil {
		t.Fatalf("read args log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 4 {
		t.Fatalf("unexpected args logged: %q", string(content))
	}

	expectedWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		t.Fatalf("resolve expected workdir: %v", err)
	}
	expectedConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		t.Fatalf("resolve expected config path: %v", err)
	}

	if lines[0] != "-d" || lines[1] != expectedWorkDir {
		t.Fatalf("unexpected workdir args: %v", lines[:2])
	}
	if lines[2] != "-f" || lines[3] != expectedConfigPath {
		t.Fatalf("unexpected config args: %v", lines[2:])
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
