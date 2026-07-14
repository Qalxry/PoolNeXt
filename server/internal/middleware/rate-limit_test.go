package middleware

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

func TestSlidingWindowLimiterIsAtomicUnderConcurrency(t *testing.T) {
	t.Parallel()

	limiter := newSlidingWindowLimiter()
	now := time.Unix(1_700_000_000, 0)
	const (
		requests = 500
		limit    = 25
	)

	var allowed atomic.Int64
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(requests)
	for range requests {
		go func() {
			defer waitGroup.Done()
			<-start
			if limiter.allow("client", limit, time.Minute, 20*time.Minute, now) {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	waitGroup.Wait()

	if got := allowed.Load(); got != limit {
		t.Fatalf("allowed requests = %d, want exactly %d", got, limit)
	}
}

func TestSlidingWindowLimiterExpiresOldRequests(t *testing.T) {
	t.Parallel()

	limiter := newSlidingWindowLimiter()
	now := time.Unix(1_700_000_000, 0)
	if !limiter.allow("client", 2, time.Second, time.Minute, now) {
		t.Fatal("first request was rejected")
	}
	if !limiter.allow("client", 2, time.Second, time.Minute, now) {
		t.Fatal("second request was rejected")
	}
	if limiter.allow("client", 2, time.Second, time.Minute, now) {
		t.Fatal("request above the limit was accepted")
	}
	if !limiter.allow("client", 2, time.Second, time.Minute, now.Add(time.Second)) {
		t.Fatal("request was not accepted after the window elapsed")
	}
}

func TestSlidingWindowLimiterToleratesOutOfOrderTimestamps(t *testing.T) {
	t.Parallel()

	limiter := newSlidingWindowLimiter()
	base := time.Unix(1_700_000_000, 0)
	if !limiter.allow("client", 2, time.Second, time.Minute, base.Add(500*time.Millisecond)) {
		t.Fatal("newer request was rejected")
	}
	if !limiter.allow("client", 2, time.Second, time.Minute, base) {
		t.Fatal("out-of-order request was rejected")
	}
	if !limiter.allow("client", 2, time.Second, time.Minute, base.Add(1100*time.Millisecond)) {
		t.Fatal("expired out-of-order request was not removed")
	}
}

func TestStaticWebAssetsBypassGlobalWebRateLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{method: http.MethodGet, path: "/_next/static/chunks/app/log.js", want: true},
		{method: http.MethodGet, path: "/zashboard/assets/index.js", want: true},
		{method: http.MethodGet, path: "/zashboard/sw.js", want: true},
		{method: http.MethodHead, path: "/favicon.ico", want: true},
		{method: http.MethodGet, path: "/nodes", want: false},
		{method: http.MethodGet, path: "/workspace.txt", want: false},
		{method: http.MethodPost, path: "/asset.js", want: false},
	}

	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, nil)
		if got := isStaticWebAssetRequest(request); got != test.want {
			t.Errorf("isStaticWebAssetRequest(%s %q) = %v, want %v", test.method, test.path, got, test.want)
		}
	}
}

func TestRedisSlidingWindowLimiterIsAtomicUnderConcurrency(t *testing.T) {
	t.Parallel()

	client := startTestRedis(t)
	const (
		requests = 100
		limit    = 10
	)

	var allowed atomic.Int64
	errorsFound := make(chan error, requests)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(requests)
	for range requests {
		go func() {
			defer waitGroup.Done()
			<-start
			ok, err := redisSlidingWindowAllow(
				context.Background(),
				client,
				"rateLimit:{test}:events",
				limit,
				time.Minute,
				20*time.Minute,
			)
			if err != nil {
				errorsFound <- err
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errorsFound)

	for err := range errorsFound {
		t.Fatalf("redisSlidingWindowAllow() error = %v", err)
	}
	if got := allowed.Load(); got != limit {
		t.Fatalf("allowed requests = %d, want exactly %d", got, limit)
	}
}

func TestExpensiveOperationGateCapsConcurrency(t *testing.T) {
	t.Parallel()

	engine := gin.New()
	engine.Use(ExpensiveOperationGate(1))
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	engine.POST("/api/runtime/start", func(c *gin.Context) {
		close(requestStarted)
		<-releaseRequest
		c.Status(http.StatusNoContent)
	})

	firstResponse := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		engine.ServeHTTP(firstResponse, httptest.NewRequest(http.MethodPost, "/api/runtime/start", nil))
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("first expensive request did not start")
	}

	secondResponse := httptest.NewRecorder()
	engine.ServeHTTP(secondResponse, httptest.NewRequest(http.MethodPost, "/api/runtime/start", nil))
	if secondResponse.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", secondResponse.Code, http.StatusTooManyRequests)
	}
	if secondResponse.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", secondResponse.Header().Get("Retry-After"))
	}

	close(releaseRequest)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first expensive request did not finish")
	}
	if firstResponse.Code != http.StatusNoContent {
		t.Fatalf("first status = %d, want %d", firstResponse.Code, http.StatusNoContent)
	}
}

func TestExpensiveOperationClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{method: http.MethodPost, path: "/api/runtime/start", want: true},
		{method: http.MethodPost, path: "/api/source-configs/test", want: true},
		{method: http.MethodPost, path: "/api/proxy-nodes/health/run", want: true},
		{method: http.MethodPost, path: "/api/port-profiles/42/runtime/save", want: true},
		{method: http.MethodPost, path: "/api/runtime/stop", want: false},
		{method: http.MethodGet, path: "/api/runtime/status", want: false},
		{method: http.MethodPost, path: "/api/user/login", want: false},
	}
	for _, test := range tests {
		if got := isExpensiveOperation(test.method, test.path); got != test.want {
			t.Errorf("isExpensiveOperation(%q, %q) = %v, want %v", test.method, test.path, got, test.want)
		}
	}
}

func startTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	redisServerPath, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is unavailable")
	}

	socketPath := filepath.Join(t.TempDir(), "redis.sock")
	command := exec.Command(
		redisServerPath,
		"--save", "",
		"--appendonly", "no",
		"--port", "0",
		"--unixsocket", socketPath,
		"--unixsocketperm", "700",
	)
	var processOutput synchronizedBuffer
	command.Stdout = &processOutput
	command.Stderr = &processOutput
	if err := command.Start(); err != nil {
		t.Skipf("could not start redis-server: %v", err)
	}

	processDone := make(chan error, 1)
	go func() {
		processDone <- command.Wait()
	}()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-processDone:
		case <-time.After(2 * time.Second):
			_ = command.Process.Kill()
			<-processDone
		}
	})

	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socketPath})
	t.Cleanup(func() { _ = client.Close() })
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := client.Ping(context.Background()).Err(); err == nil {
			return client
		}
		if time.Now().After(deadline) {
			t.Fatalf("redis-server did not become ready\n%s", processOutput.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type synchronizedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(data)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}
