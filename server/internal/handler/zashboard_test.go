package controller

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"poolx/internal/pkg/common"

	"github.com/gin-gonic/gin"
)

func TestZashboardClashAllowlistMatrix(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		path      string
		webSocket bool
		allowed   bool
	}{
		{name: "version", method: http.MethodGet, path: "/version", allowed: true},
		{name: "configs read", method: http.MethodGet, path: "/configs", allowed: true},
		{name: "proxy list", method: http.MethodGet, path: "/proxies", allowed: true},
		{name: "proxy delay", method: http.MethodGet, path: "/proxies/auto/delay", allowed: true},
		{name: "group delay", method: http.MethodGet, path: "/group/auto/delay", allowed: true},
		{name: "provider read", method: http.MethodGet, path: "/providers/proxies/provider", allowed: true},
		{name: "provider health", method: http.MethodGet, path: "/providers/proxies/provider/healthcheck", allowed: true},
		{name: "provider proxy health", method: http.MethodGet, path: "/providers/proxies/provider/node/healthcheck", allowed: true},
		{name: "dns query", method: http.MethodGet, path: "/dns/query", allowed: true},
		{name: "select proxy", method: http.MethodPut, path: "/proxies/auto", allowed: true},
		{name: "unfix proxy", method: http.MethodDelete, path: "/proxies/auto", allowed: true},
		{name: "close connection", method: http.MethodDelete, path: "/connections/id", allowed: true},
		{name: "logs websocket", method: http.MethodGet, path: "/logs", webSocket: true, allowed: true},
		{name: "traffic websocket", method: http.MethodGet, path: "/traffic", webSocket: true, allowed: true},
		{name: "memory websocket", method: http.MethodGet, path: "/memory", webSocket: true, allowed: true},
		{name: "connections websocket", method: http.MethodGet, path: "/connections", webSocket: true, allowed: true},
		{name: "upgrade read is still hidden", method: http.MethodGet, path: "/upgrade", allowed: false},
		{name: "core upgrade", method: http.MethodPost, path: "/upgrade", allowed: false},
		{name: "ui upgrade", method: http.MethodPost, path: "/upgrade/ui", allowed: false},
		{name: "geo upgrade", method: http.MethodPost, path: "/upgrade/geo", allowed: false},
		{name: "restart", method: http.MethodPost, path: "/restart", allowed: false},
		{name: "replace configs", method: http.MethodPut, path: "/configs", allowed: false},
		{name: "patch configs", method: http.MethodPatch, path: "/configs", allowed: false},
		{name: "config geo update", method: http.MethodPost, path: "/configs/geo", allowed: false},
		{name: "provider update", method: http.MethodPut, path: "/providers/proxies/provider", allowed: false},
		{name: "rule provider update", method: http.MethodPut, path: "/providers/rules/provider", allowed: false},
		{name: "cache mutation", method: http.MethodPost, path: "/cache/dns/flush", allowed: false},
		{name: "storage mutation", method: http.MethodPut, path: "/storage/key", allowed: false},
		{name: "rule mutation", method: http.MethodPatch, path: "/rules/disable", allowed: false},
		{name: "debug mutation", method: http.MethodPut, path: "/debug/gc", allowed: false},
		{name: "websocket on arbitrary read path", method: http.MethodGet, path: "/version", webSocket: true, allowed: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isZashboardClashRequestAllowed(test.method, test.path, test.webSocket); got != test.allowed {
				t.Fatalf("allowlist result for %s %s websocket=%v: got %v want %v", test.method, test.path, test.webSocket, got, test.allowed)
			}
		})
	}
}

func TestZashboardClashGetSideEffectsAreExplicitlyClassified(t *testing.T) {
	for _, requestPath := range []string{
		"/proxies/node/delay",
		"/group/auto/delay",
		"/providers/proxies/provider/healthcheck",
		"/providers/proxies/provider/node/healthcheck",
	} {
		if !isZashboardClashBoundedStateGetPath(requestPath) {
			t.Fatalf("expected bounded state-changing GET classification for %q", requestPath)
		}
		if !isZashboardClashGetPathAllowed(requestPath) {
			t.Fatalf("expected bounded state-changing GET to remain allowed for %q", requestPath)
		}
		if isZashboardClashReadOnlyPath(requestPath) {
			t.Fatalf("state-changing GET was incorrectly classified as read-only: %q", requestPath)
		}
	}

	if isZashboardClashBoundedStateGetPath("/configs") {
		t.Fatal("read-only config request was classified as state-changing")
	}
}

func TestProxyZashboardClashRejectsDangerousMutationBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.Any("/api/zashboard/clash/*path", ProxyZashboardClash)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/zashboard/clash/upgrade/ui", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("dangerous request reached upstream %d times", upstreamHits.Load())
	}
}

func TestProxyZashboardClashForwardsAllowedReadWithoutCredentialLeak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type observedRequest struct {
		path          string
		rawQuery      string
		authorization string
		cookie        string
	}
	observed := make(chan observedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed <- observedRequest{
			path:          request.URL.Path,
			rawQuery:      request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"),
			cookie:        request.Header.Get("Cookie"),
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.GET("/api/zashboard/clash/*path", ProxyZashboardClash)
	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()
	request, err := http.NewRequest(http.MethodGet, proxyServer.URL+"/api/zashboard/clash/version?source=zashboard&token=attacker", nil)
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer poolnext-session-token")
	request.Header.Set("Cookie", "session=poolnext-session")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send proxy request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected upstream response, got %d", response.StatusCode)
	}
	got := <-observed
	if got.path != "/version" {
		t.Fatalf("unexpected upstream path: %s", got.path)
	}
	if got.authorization != "Bearer controller-secret" {
		t.Fatalf("unexpected upstream authorization: %q", got.authorization)
	}
	if got.cookie != "" {
		t.Fatalf("PoolNeXt session cookie leaked upstream: %q", got.cookie)
	}
	if got.rawQuery != "source=zashboard" {
		t.Fatalf("unexpected upstream query: %q", got.rawQuery)
	}
}

func TestProxyZashboardClashForwardsApprovedWebSocketWithControllerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type observedRequest struct {
		path          string
		rawQuery      string
		authorization string
		cookie        string
		upgrade       string
		connection    string
	}
	observed := make(chan observedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed <- observedRequest{
			path:          request.URL.Path,
			rawQuery:      request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"),
			cookie:        request.Header.Get("Cookie"),
			upgrade:       request.Header.Get("Upgrade"),
			connection:    request.Header.Get("Connection"),
		}
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.GET("/api/zashboard/clash/*path", ProxyZashboardClash)
	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()
	request, err := http.NewRequest(http.MethodGet, proxyServer.URL+"/api/zashboard/clash/logs?level=info&token=attacker", nil)
	if err != nil {
		t.Fatalf("create websocket proxy request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer poolnext-session-token")
	request.Header.Set("Cookie", "session=poolnext-session")
	request.Header.Set("Connection", "keep-alive, Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send websocket proxy request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected upstream response, got %d", response.StatusCode)
	}
	got := <-observed
	if got.path != "/logs" {
		t.Fatalf("unexpected upstream path: %s", got.path)
	}
	if got.authorization != "Bearer controller-secret" {
		t.Fatalf("unexpected upstream authorization: %q", got.authorization)
	}
	if got.cookie != "" {
		t.Fatalf("PoolNeXt session cookie leaked upstream: %q", got.cookie)
	}
	if got.rawQuery != "level=info&token=controller-secret" {
		t.Fatalf("unexpected upstream websocket query: %q", got.rawQuery)
	}
	if !strings.EqualFold(got.upgrade, "websocket") || !strings.Contains(strings.ToLower(got.connection), "upgrade") {
		t.Fatalf("websocket upgrade headers were not preserved: connection=%q upgrade=%q", got.connection, got.upgrade)
	}
}

func TestProxyZashboardClashTunnelsApprovedWebSocket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	type observedRequest struct {
		rawQuery      string
		authorization string
		cookie        string
	}
	observed := make(chan observedRequest, 1)
	tunnelResult := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed <- observedRequest{
			rawQuery:      request.URL.RawQuery,
			authorization: request.Header.Get("Authorization"),
			cookie:        request.Header.Get("Cookie"),
		}

		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			tunnelResult <- fmt.Errorf("upstream response writer cannot hijack")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			tunnelResult <- err
			return
		}
		defer connection.Close()
		if _, err = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"); err != nil {
			tunnelResult <- err
			return
		}
		if err = buffered.Flush(); err != nil {
			tunnelResult <- err
			return
		}
		payload, err := buffered.ReadString('\n')
		if err != nil {
			tunnelResult <- err
			return
		}
		if _, err = buffered.WriteString("echo:" + payload); err != nil {
			tunnelResult <- err
			return
		}
		tunnelResult <- buffered.Flush()
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.GET("/api/zashboard/clash/*path", ProxyZashboardClash)
	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()
	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy server URL: %v", err)
	}
	connection, err := net.DialTimeout("tcp", proxyURL.Host, 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy server: %v", err)
	}
	defer connection.Close()
	if err = connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set tunnel deadline: %v", err)
	}

	if _, err = fmt.Fprintf(connection, "GET /api/zashboard/clash/logs?token=attacker HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nAuthorization: Bearer poolnext-session-token\r\nCookie: session=poolnext-session\r\n\r\n", proxyURL.Host); err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}
	reader := bufio.NewReader(connection)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read upgrade status: %v", err)
	}
	if !strings.Contains(statusLine, " 101 ") {
		t.Fatalf("expected 101 upgrade, got %q", strings.TrimSpace(statusLine))
	}
	for {
		headerLine, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("read upgrade headers: %v", readErr)
		}
		if headerLine == "\r\n" {
			break
		}
	}
	if _, err = connection.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write tunneled payload: %v", err)
	}
	echo, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read tunneled payload: %v", err)
	}
	if echo != "echo:ping\n" {
		t.Fatalf("unexpected tunneled response: %q", echo)
	}
	if err = <-tunnelResult; err != nil {
		t.Fatalf("upstream tunnel failed: %v", err)
	}
	got := <-observed
	if got.rawQuery != "token=controller-secret" {
		t.Fatalf("unexpected upstream websocket query: %q", got.rawQuery)
	}
	if got.authorization != "Bearer controller-secret" {
		t.Fatalf("unexpected upstream authorization: %q", got.authorization)
	}
	if got.cookie != "" {
		t.Fatalf("PoolNeXt session cookie leaked upstream: %q", got.cookie)
	}
}

func TestProxyZashboardClashRejectsUnsupportedUpgradeProtocol(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.GET("/api/zashboard/clash/*path", ProxyZashboardClash)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/zashboard/clash/logs", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "h2c")
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("unsupported upgrade reached upstream %d times", upstreamHits.Load())
	}
}

func TestProxyZashboardClashRejectsEncodedTraversalThroughGin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.Any("/api/zashboard/clash/*path", ProxyZashboardClash)
	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()

	for _, requestPath := range []string{
		"/api/zashboard/clash/%2e%2e/upgrade",
		"/api/zashboard/clash/proxies%2f..%2fupgrade",
		"/api/zashboard/clash/%252e%252e%252fupgrade",
		"/api/zashboard/clash/%252fupgrade",
	} {
		request, err := http.NewRequest(http.MethodGet, proxyServer.URL+requestPath, nil)
		if err != nil {
			t.Fatalf("create request for %q: %v", requestPath, err)
		}
		response, err := proxyServer.Client().Do(request)
		if err != nil {
			t.Fatalf("send request for %q: %v", requestPath, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("encoded path %q status = %d, want 403", requestPath, response.StatusCode)
		}
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("encoded traversal reached upstream %d times", upstreamHits.Load())
	}
}

func TestProxyZashboardClashRejectsInconsistentRawPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		upstreamHits.Add(1)
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.GET("/api/zashboard/clash/*path", ProxyZashboardClash)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/zashboard/clash/version", nil)
	request.URL.RawPath = "/api/zashboard/clash/%2e%2e/upgrade"
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("inconsistent RawPath status = %d, want 403", recorder.Code)
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("inconsistent RawPath reached upstream %d times", upstreamHits.Load())
	}
}

func TestProxyZashboardClashAllowsEncodedUnicodeResourceName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	observedPath := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedPath <- request.URL.Path
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.PUT("/api/zashboard/clash/*path", ProxyZashboardClash)
	proxyServer := httptest.NewServer(engine)
	defer proxyServer.Close()

	request, err := http.NewRequest(http.MethodPut, proxyServer.URL+"/api/zashboard/clash/proxies/%E8%8A%82%E7%82%B9", strings.NewReader(`{"name":"direct"}`))
	if err != nil {
		t.Fatalf("create unicode path request: %v", err)
	}
	response, err := proxyServer.Client().Do(request)
	if err != nil {
		t.Fatalf("send unicode path request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("unicode path status = %d, want 204", response.StatusCode)
	}
	if got := <-observedPath; got != "/proxies/节点" {
		t.Fatalf("upstream unicode path = %q", got)
	}
}

func TestProxyZashboardClashRefreshesDeadlinesPastServerWriteTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamResult := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			upstreamResult <- fmt.Errorf("upstream response writer cannot hijack")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			upstreamResult <- err
			return
		}
		defer connection.Close()
		if _, err = buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"); err != nil {
			upstreamResult <- err
			return
		}
		if err = buffered.Flush(); err != nil {
			upstreamResult <- err
			return
		}
		time.Sleep(150 * time.Millisecond)
		if _, err = buffered.WriteString("late-frame\n"); err == nil {
			err = buffered.Flush()
		}
		upstreamResult <- err
	}))
	defer upstream.Close()
	setZashboardControllerForTest(t, upstream.URL, "controller-secret")

	engine := gin.New()
	engine.GET("/api/zashboard/clash/*path", ProxyZashboardClash)
	proxyServer := httptest.NewUnstartedServer(engine)
	proxyServer.Config.WriteTimeout = 30 * time.Millisecond
	proxyServer.Start()
	defer proxyServer.Close()

	proxyURL, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	connection, err := net.DialTimeout("tcp", proxyURL.Host, 2*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer connection.Close()
	if err = connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set connection deadline: %v", err)
	}
	if _, err = fmt.Fprintf(connection, "GET /api/zashboard/clash/logs HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", proxyURL.Host); err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}

	reader := bufio.NewReader(connection)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.Contains(statusLine, " 101 ") {
		t.Fatalf("upgrade status = %q", strings.TrimSpace(statusLine))
	}
	for {
		headerLine, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("read upgrade headers: %v", readErr)
		}
		if headerLine == "\r\n" {
			break
		}
	}
	frame, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read delayed frame: %v", err)
	}
	if frame != "late-frame\n" {
		t.Fatalf("delayed frame = %q", frame)
	}
	if err = <-upstreamResult; err != nil {
		t.Fatalf("upstream tunnel failed: %v", err)
	}
}

func TestActivityDeadlineConnRefreshesReadAndWriteDeadlines(t *testing.T) {
	underlying := &deadlineRecordingConn{}
	connection := &activityDeadlineConn{
		Conn:         underlying,
		readTimeout:  time.Minute,
		writeTimeout: 2 * time.Minute,
	}
	started := time.Now()
	_, _ = connection.Read(make([]byte, 1))
	if underlying.readDeadline.Before(started.Add(59 * time.Second)) {
		t.Fatalf("read deadline was not refreshed: %v", underlying.readDeadline)
	}
	if _, err := connection.Write([]byte("frame")); err != nil {
		t.Fatalf("write through deadline connection: %v", err)
	}
	if underlying.writeDeadline.Before(started.Add(119 * time.Second)) {
		t.Fatalf("write deadline was not refreshed: %v", underlying.writeDeadline)
	}
}

func TestNormalizeZashboardClashPathRejectsTraversal(t *testing.T) {
	for _, requestPath := range []string{"//upgrade", "/proxies/../upgrade", "/configs\\geo", "/proxies//auto"} {
		if _, valid := normalizeZashboardClashPath(requestPath); valid {
			t.Fatalf("expected unsafe path %q to be rejected", requestPath)
		}
	}
}

func setZashboardControllerForTest(t *testing.T, serverURL string, secret string) {
	t.Helper()
	common.OptionMapRWMutex.Lock()
	originalController := common.ClashExternalController
	originalSecret := common.ClashSecret
	common.ClashExternalController = strings.TrimPrefix(serverURL, "http://")
	common.ClashSecret = secret
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.ClashExternalController = originalController
		common.ClashSecret = originalSecret
		common.OptionMapRWMutex.Unlock()
	})
}

type deadlineRecordingConn struct {
	readDeadline  time.Time
	writeDeadline time.Time
}

func (connection *deadlineRecordingConn) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (connection *deadlineRecordingConn) Write(buffer []byte) (int, error) {
	return len(buffer), nil
}

func (connection *deadlineRecordingConn) Close() error {
	return nil
}

func (connection *deadlineRecordingConn) LocalAddr() net.Addr {
	return &net.TCPAddr{}
}

func (connection *deadlineRecordingConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{}
}

func (connection *deadlineRecordingConn) SetDeadline(deadline time.Time) error {
	connection.readDeadline = deadline
	connection.writeDeadline = deadline
	return nil
}

func (connection *deadlineRecordingConn) SetReadDeadline(deadline time.Time) error {
	connection.readDeadline = deadline
	return nil
}

func (connection *deadlineRecordingConn) SetWriteDeadline(deadline time.Time) error {
	connection.writeDeadline = deadline
	return nil
}
