package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"
)

const mihomoControllerResponseLimit = 16 << 20

type MihomoConnectionMetadata struct {
	Network           string `json:"network"`
	Type              string `json:"type"`
	SourceIP          string `json:"sourceIP"`
	DestinationIP     string `json:"destinationIP"`
	SourcePort        string `json:"sourcePort"`
	DestinationPort   string `json:"destinationPort"`
	InboundIP         string `json:"inboundIP"`
	InboundPort       string `json:"inboundPort"`
	InboundName       string `json:"inboundName"`
	InboundUser       string `json:"inboundUser"`
	Host              string `json:"host"`
	Process           string `json:"process"`
	ProcessPath       string `json:"processPath"`
	SpecialProxy      string `json:"specialProxy"`
	RemoteDestination string `json:"remoteDestination"`
	SniffHost         string `json:"sniffHost"`
}

type MihomoConnection struct {
	ID             string                   `json:"id"`
	Metadata       MihomoConnectionMetadata `json:"metadata"`
	Upload         int64                    `json:"upload"`
	Download       int64                    `json:"download"`
	Start          time.Time                `json:"start"`
	Chains         []string                 `json:"chains"`
	ProviderChains []string                 `json:"providerChains"`
	Rule           string                   `json:"rule"`
	RulePayload    string                   `json:"rulePayload"`
}

type MihomoConnectionSnapshot struct {
	DownloadTotal int64              `json:"downloadTotal"`
	UploadTotal   int64              `json:"uploadTotal"`
	Connections   []MihomoConnection `json:"connections"`
	Memory        uint64             `json:"memory"`
}

type MihomoTrafficSnapshot struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

var MihomoHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
}

func StartMihomoProcess(binaryPath string, workDir string, configPath string, stdout io.Writer, stderr io.Writer) (*exec.Cmd, error) {
	resolvedWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve mihomo workdir: %w", err)
	}
	resolvedConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve mihomo config path: %w", err)
	}
	cmd := exec.Command(binaryPath, "-d", resolvedWorkDir, "-f", resolvedConfigPath)
	cmd.Dir = resolvedWorkDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func WaitForControllerReady(ctx context.Context, controllerAddress string, secret string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := ProbeController(ctx, controllerAddress, secret); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func ProbeController(ctx context.Context, controllerAddress string, secret string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/version", controllerAddress), nil)
	if err != nil {
		return err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := MihomoHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("controller status: %s", resp.Status)
	}
	return nil
}

func ReloadMihomoConfig(ctx context.Context, controllerAddress string, secret string, path string) error {
	payload, err := json.Marshal(map[string]string{
		"path":    path,
		"payload": "",
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("http://%s/configs?force=true", controllerAddress), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := MihomoHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reload controller config failed: %s", resp.Status)
	}
	return nil
}

func GetMihomoVersion(ctx context.Context, controllerAddress string, secret string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/version", controllerAddress), nil)
	if err != nil {
		return "", err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := MihomoHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("controller status: %s", resp.Status)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return payload.Version, nil
}

func GetMihomoConnections(ctx context.Context, controllerAddress string, secret string) (*MihomoConnectionSnapshot, error) {
	var payload MihomoConnectionSnapshot
	if err := getMihomoControllerJSON(ctx, controllerAddress, secret, "/connections", &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func GetMihomoTraffic(ctx context.Context, controllerAddress string, secret string) (*MihomoTrafficSnapshot, error) {
	var payload MihomoTrafficSnapshot
	if err := getMihomoTrafficJSON(ctx, controllerAddress, secret, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func getMihomoTrafficJSON(ctx context.Context, controllerAddress string, secret string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/traffic", controllerAddress), nil)
	if err != nil {
		return err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := MihomoHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("controller status: %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, mihomoControllerResponseLimit)).Decode(target); err != nil {
		return fmt.Errorf("decode controller response: %w", err)
	}
	return nil
}

func getMihomoControllerJSON(ctx context.Context, controllerAddress string, secret string, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s%s", controllerAddress, path), nil)
	if err != nil {
		return err
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := MihomoHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("controller status: %s", resp.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, mihomoControllerResponseLimit))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode controller response: %w", err)
	}
	return nil
}
