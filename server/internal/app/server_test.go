package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"poolx/internal/model"
	"poolx/internal/service"
)

const weakSessionSecretChildEnv = "POOLNEXT_TEST_WEAK_SESSION_SECRET_CHILD"

func TestRunServerRejectsWeakSessionSecretAtStartup(t *testing.T) {
	if os.Getenv(weakSessionSecretChildEnv) == "1" {
		RunServer(nil, "unused", nil, "unused")
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestRunServerRejectsWeakSessionSecretAtStartup$")
	command.Env = append(
		filteredEnvironment(os.Environ(), "SESSION_SECRET", weakSessionSecretChildEnv),
		"SESSION_SECRET=replace-with-random-string-1234567890",
		weakSessionSecretChildEnv+"=1",
	)
	output, err := command.CombinedOutput()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("weak-secret process error = %v, output = %s", err, output)
	}
	if !strings.Contains(string(output), "invalid session secret") {
		t.Fatalf("startup rejection was not logged: %s", output)
	}
}

func TestValidateSessionSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{name: "strong", secret: "F4nBq8vZP1sY6dR9mK2xC7wL5tH3jN0a", wantErr: false},
		{name: "blank", secret: "   ", wantErr: true},
		{name: "surrounding whitespace", secret: " F4nBq8vZP1sY6dR9mK2xC7wL5tH3jN0a", wantErr: true},
		{name: "short", secret: "short-secret", wantErr: true},
		{name: "placeholder", secret: "replace-with-random-string-1234567890", wantErr: true},
		{name: "low diversity", secret: strings.Repeat("abcd", 16), wantErr: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateSessionSecret(test.secret)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateSessionSecret() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestResolveSessionSecretRequiresExplicitSafeMode(t *testing.T) {
	t.Parallel()

	strongSecret := "F4nBq8vZP1sY6dR9mK2xC7wL5tH3jN0a"
	tests := []struct {
		name          string
		environment   map[string]string
		executable    string
		mode          string
		wantSecret    string
		wantEphemeral bool
		wantErr       bool
	}{
		{
			name:        "configured strong secret",
			environment: map[string]string{"SESSION_SECRET": strongSecret},
			executable:  "poolnext",
			mode:        gin.ReleaseMode,
			wantSecret:  strongSecret,
		},
		{
			name:        "missing secret",
			environment: map[string]string{},
			executable:  "poolnext",
			mode:        gin.ReleaseMode,
			wantErr:     true,
		},
		{
			name: "explicit debug ephemeral secret",
			environment: map[string]string{
				allowEphemeralSessionSecretEnv: "true",
			},
			executable:    "poolnext",
			mode:          gin.DebugMode,
			wantSecret:    strongSecret,
			wantEphemeral: true,
		},
		{
			name: "explicit test binary ephemeral secret",
			environment: map[string]string{
				allowEphemeralSessionSecretEnv: "true",
			},
			executable:    "app.test",
			mode:          gin.ReleaseMode,
			wantSecret:    strongSecret,
			wantEphemeral: true,
		},
		{
			name: "production cannot opt into ephemeral secret",
			environment: map[string]string{
				allowEphemeralSessionSecretEnv: "true",
			},
			executable: "poolnext",
			mode:       gin.ReleaseMode,
			wantErr:    true,
		},
		{
			name: "invalid opt in",
			environment: map[string]string{
				allowEphemeralSessionSecretEnv: "sometimes",
			},
			executable: "poolnext",
			mode:       gin.DebugMode,
			wantErr:    true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			secret, ephemeral, err := resolveSessionSecret(
				mapLookup(test.environment),
				strongSecret,
				test.executable,
				test.mode,
			)
			if (err != nil) != test.wantErr {
				t.Fatalf("resolveSessionSecret() error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if secret != test.wantSecret {
				t.Fatalf("resolveSessionSecret() secret = %q, want %q", secret, test.wantSecret)
			}
			if ephemeral != test.wantEphemeral {
				t.Fatalf("resolveSessionSecret() ephemeral = %v, want %v", ephemeral, test.wantEphemeral)
			}
		})
	}
}

func TestLoadHTTPRuntimeConfig(t *testing.T) {
	t.Parallel()

	config, err := loadHTTPRuntimeConfig(mapLookup(map[string]string{
		trustedProxiesEnv:       "127.0.0.1, 10.0.0.0/8,127.0.0.1",
		maxRequestBodyBytesEnv:  "1024",
		maxUploadBodyBytesEnv:   "2048",
		maxExpensiveRequestsEnv: "7",
	}))
	if err != nil {
		t.Fatalf("loadHTTPRuntimeConfig() error = %v", err)
	}
	if len(config.TrustedProxies) != 2 {
		t.Fatalf("trusted proxy count = %d, want 2", len(config.TrustedProxies))
	}
	if config.MaxRequestBodyBytes != 1024 || config.MaxUploadBodyBytes != 2048 {
		t.Fatalf("unexpected body limits: %+v", config)
	}
	if config.MaxExpensiveRequests != 7 {
		t.Fatalf("max expensive requests = %d, want 7", config.MaxExpensiveRequests)
	}

	_, err = loadHTTPRuntimeConfig(mapLookup(map[string]string{maxRequestBodyBytesEnv: "0"}))
	if err == nil {
		t.Fatal("loadHTTPRuntimeConfig() accepted a zero body limit")
	}
	_, err = loadHTTPRuntimeConfig(mapLookup(map[string]string{trustedProxiesEnv: "127.0.0.1,,10.0.0.1"}))
	if err == nil {
		t.Fatal("loadHTTPRuntimeConfig() accepted an empty trusted proxy entry")
	}
}

func TestConfigureTrustedProxiesRejectsSpoofedForwardedIPByDefault(t *testing.T) {
	t.Parallel()

	clientIP := func(t *testing.T, trustedProxies []string) string {
		t.Helper()
		engine := gin.New()
		if err := configureTrustedProxies(engine, trustedProxies); err != nil {
			t.Fatalf("configureTrustedProxies() error = %v", err)
		}
		engine.GET("/ip", func(c *gin.Context) {
			c.String(http.StatusOK, c.ClientIP())
		})

		request := httptest.NewRequest(http.MethodGet, "/ip", nil)
		request.RemoteAddr = "203.0.113.9:12345"
		request.Header.Set("X-Forwarded-For", "198.51.100.7")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response.Body.String()
	}

	if got := clientIP(t, nil); got != "203.0.113.9" {
		t.Fatalf("untrusted proxy ClientIP() = %q, want direct peer", got)
	}
	if got := clientIP(t, []string{"203.0.113.9"}); got != "198.51.100.7" {
		t.Fatalf("trusted proxy ClientIP() = %q, want forwarded client", got)
	}
}

func TestRequestBodyLimit(t *testing.T) {
	t.Parallel()

	t.Run("rejects known oversized JSON body", func(t *testing.T) {
		called := false
		engine := gin.New()
		engine.Use(requestBodyLimit(4, 8))
		engine.POST("/", func(c *gin.Context) {
			called = true
			c.Status(http.StatusNoContent)
		})

		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
		}
		if called {
			t.Fatal("oversized request reached the handler")
		}
	})

	t.Run("uses the larger upload limit", func(t *testing.T) {
		engine := gin.New()
		engine.Use(requestBodyLimit(4, 8))
		engine.POST("/api/source-configs/parse", func(c *gin.Context) {
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.String(http.StatusOK, string(body))
		})

		request := httptest.NewRequest(http.MethodPost, "/api/source-configs/parse", strings.NewReader("123456"))
		request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		if response.Code != http.StatusOK || response.Body.String() != "123456" {
			t.Fatalf("upload response = (%d, %q), want (200, %q)", response.Code, response.Body.String(), "123456")
		}
	})

	t.Run("does not grant upload limit from content type alone", func(t *testing.T) {
		called := false
		engine := gin.New()
		engine.Use(requestBodyLimit(4, 8))
		engine.POST("/api/user/login", func(c *gin.Context) {
			called = true
			c.Status(http.StatusNoContent)
		})

		request := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader("123456"))
		request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
		}
		if called {
			t.Fatal("spoofed multipart request reached the handler")
		}
	})

	t.Run("bounds chunked bodies", func(t *testing.T) {
		engine := gin.New()
		engine.Use(requestBodyLimit(4, 8))
		engine.POST("/", func(c *gin.Context) {
			_, err := io.ReadAll(c.Request.Body)
			var maxBytesError *http.MaxBytesError
			if !errors.As(err, &maxBytesError) {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Status(http.StatusRequestEntityTooLarge)
		})

		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
		request.ContentLength = -1
		request.TransferEncoding = []string{"chunked"}
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)

		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
		}
	})
}

func TestNewHTTPServerHasDefensiveTimeouts(t *testing.T) {
	t.Parallel()

	server := newHTTPServer(":0", http.NotFoundHandler(), context.Background())
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("server timeouts must all be positive: %+v", server)
	}
	if server.MaxHeaderBytes <= 0 {
		t.Fatalf("MaxHeaderBytes = %d, want positive", server.MaxHeaderBytes)
	}
	if server.BaseContext == nil {
		t.Fatal("BaseContext must propagate the application context")
	}
}

func TestRuntimeNeedsShutdownWhenPersistedPIDExists(t *testing.T) {
	pid := 2083015

	tests := []struct {
		name   string
		status *service.RuntimeStatus
		want   bool
	}{
		{name: "nil status", status: nil, want: false},
		{
			name: "stale persisted pid",
			status: &service.RuntimeStatus{
				Instance: &model.KernelInstance{PID: &pid},
			},
			want: true,
		},
		{
			name:   "managed running process",
			status: &service.RuntimeStatus{Running: true},
			want:   true,
		},
		{
			name:   "fully stopped",
			status: &service.RuntimeStatus{Instance: &model.KernelInstance{}},
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := runtimeNeedsShutdown(test.status); got != test.want {
				t.Fatalf("runtimeNeedsShutdown() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestServeHTTPListenerGracefullyDrainsActiveRequest(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}

	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	handler := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		response.WriteHeader(http.StatusNoContent)
	})
	server := newHTTPServer(listener.Addr().String(), handler, context.Background())
	server.ReadTimeout = time.Second
	server.WriteTimeout = time.Second

	serveContext, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serveHTTPListener(serveContext, server, listener, time.Second)
	}()

	responseDone := make(chan *http.Response, 1)
	requestErrors := make(chan error, 1)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	go func() {
		response, requestErr := client.Get("http://" + listener.Addr().String())
		if requestErr != nil {
			requestErrors <- requestErr
			return
		}
		responseDone <- response
	}()

	select {
	case <-requestStarted:
	case err := <-requestErrors:
		t.Fatalf("request failed before handler started: %v", err)
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}

	cancelServe()
	close(releaseRequest)

	select {
	case response := <-responseDone:
		defer response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
		}
	case err := <-requestErrors:
		t.Fatalf("active request was not drained: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("active request did not finish")
	}

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serveHTTPListener() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP server did not shut down")
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func filteredEnvironment(environment []string, names ...string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		keep := true
		for _, name := range names {
			if strings.HasPrefix(entry, name+"=") {
				keep = false
				break
			}
		}
		if keep {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
