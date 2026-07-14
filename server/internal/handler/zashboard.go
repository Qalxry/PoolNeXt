package controller

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	pathpkg "path"
	"poolx/internal/pkg/common"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	zashboardUpstreamDialTimeout           = 10 * time.Second
	zashboardUpstreamResponseHeaderTimeout = 10 * time.Second
	zashboardWebSocketIdleTimeout          = 2 * time.Minute
	zashboardWebSocketWriteTimeout         = 15 * time.Second
)

var zashboardClashReadOnlyPaths = map[string]struct{}{
	"/":                  {},
	"/configs":           {},
	"/connections":       {},
	"/dns/query":         {},
	"/group":             {},
	"/group/weights":     {},
	"/logs":              {},
	"/memory":            {},
	"/providers/proxies": {},
	"/providers/rules":   {},
	"/proxies":           {},
	"/rules":             {},
	"/traffic":           {},
	"/version":           {},
}

var zashboardClashWebSocketPaths = map[string]struct{}{
	"/connections": {},
	"/logs":        {},
	"/memory":      {},
	"/traffic":     {},
}

func ProxyZashboardClash(c *gin.Context) {
	proxyPath, validPath := normalizeZashboardClashPath(c.Param("path"))
	webSocket := isWebSocketRequest(c.Request)
	if !validPath || !isZashboardClashEscapedPathSafe(c.Request) || hasUnsupportedUpgrade(c.Request, webSocket) || !isZashboardClashRequestAllowed(c.Request.Method, proxyPath, webSocket) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "该 Mihomo 控制接口未向 Zashboard 开放。",
		})
		return
	}

	common.OptionMapRWMutex.RLock()
	controllerAddress := strings.TrimSpace(common.ClashExternalController)
	secret := strings.TrimSpace(common.ClashSecret)
	common.OptionMapRWMutex.RUnlock()
	if controllerAddress == "" {
		controllerAddress = common.DefaultClashExternalController
	}

	target, err := url.Parse("http://" + controllerAddress)
	if err != nil {
		respondFailure(c, "Clash 控制地址无效")
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	if webSocket {
		proxy.Transport = newZashboardWebSocketTransport()
	}
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = proxyPath
		req.URL.RawPath = ""
		req.Host = target.Host
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")

		query := req.URL.Query()
		query.Del("token")
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
			if webSocket {
				query.Set("token", secret)
			}
		}
		req.URL.RawQuery = query.Encode()
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, proxyErr error) {
		c.JSON(502, gin.H{
			"success": false,
			"message": "连接 Clash 控制接口失败: " + proxyErr.Error(),
		})
	}

	if !webSocket {
		proxy.ServeHTTP(c.Writer, c.Request)
		return
	}
	if err = clearZashboardResponseWriteDeadline(c.Writer); err != nil {
		respondFailure(c, "无法建立 Zashboard 实时连接")
		return
	}
	proxy.ServeHTTP(&activityDeadlineResponseWriter{
		ResponseWriter: c.Writer,
		readTimeout:    zashboardWebSocketIdleTimeout,
		writeTimeout:   zashboardWebSocketWriteTimeout,
	}, c.Request)
}

func clearZashboardResponseWriteDeadline(writer http.ResponseWriter) error {
	return http.NewResponseController(writer).SetWriteDeadline(time.Time{})
}

func isZashboardClashRequestAllowed(method string, requestPath string, webSocket bool) bool {
	if webSocket {
		if method != http.MethodGet {
			return false
		}
		_, allowed := zashboardClashWebSocketPaths[requestPath]
		return allowed
	}

	switch method {
	case http.MethodGet:
		return isZashboardClashGetPathAllowed(requestPath)
	case http.MethodPut:
		return matchesZashboardPath(requestPath, "proxies", "*")
	case http.MethodDelete:
		return matchesZashboardPath(requestPath, "proxies", "*") ||
			requestPath == "/connections" || matchesZashboardPath(requestPath, "connections", "*")
	default:
		return false
	}
}

func isZashboardClashGetPathAllowed(requestPath string) bool {
	return isZashboardClashReadOnlyPath(requestPath) || isZashboardClashBoundedStateGetPath(requestPath)
}

func isZashboardClashReadOnlyPath(requestPath string) bool {
	if _, allowed := zashboardClashReadOnlyPaths[requestPath]; allowed {
		return true
	}

	segments := zashboardPathSegments(requestPath)
	if len(segments) == 0 {
		return false
	}

	switch segments[0] {
	case "proxies", "group":
		return len(segments) == 2 || (len(segments) == 3 && segments[2] == "weights")
	case "providers":
		if len(segments) < 3 || segments[1] != "proxies" {
			return false
		}
		return len(segments) == 3 || (len(segments) == 4 && segments[3] != "healthcheck")
	default:
		return false
	}
}

// Delay and health-check GETs are intentionally exposed because Zashboard
// needs them for diagnostics. They are not read-only: Mihomo updates latency
// state, and /group/{name}/delay also clears a non-selector forced choice and
// its cached selection. The allowlist keeps these bounded effects explicit.
func isZashboardClashBoundedStateGetPath(requestPath string) bool {
	if matchesZashboardPath(requestPath, "proxies", "*", "delay") ||
		matchesZashboardPath(requestPath, "group", "*", "delay") {
		return true
	}

	segments := zashboardPathSegments(requestPath)
	return len(segments) >= 4 && len(segments) <= 5 &&
		segments[0] == "providers" && segments[1] == "proxies" &&
		segments[len(segments)-1] == "healthcheck"
}

func matchesZashboardPath(requestPath string, pattern ...string) bool {
	segments := zashboardPathSegments(requestPath)
	if len(segments) != len(pattern) {
		return false
	}
	for index, expected := range pattern {
		if expected != "*" && segments[index] != expected {
			return false
		}
	}
	return true
}

func zashboardPathSegments(requestPath string) []string {
	if requestPath == "/" {
		return nil
	}
	segments := strings.Split(strings.TrimPrefix(requestPath, "/"), "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return nil
		}
	}
	return segments
}

func normalizeZashboardClashPath(requestPath string) (string, bool) {
	if requestPath == "" {
		return "/", true
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	if strings.ContainsRune(requestPath, '\x00') || strings.Contains(requestPath, "\\") {
		return "", false
	}

	cleanPath := pathpkg.Clean(requestPath)
	if requestPath != cleanPath && requestPath != cleanPath+"/" {
		return "", false
	}
	if cleanPath == "." || !strings.HasPrefix(cleanPath, "/") {
		return "", false
	}
	return cleanPath, true
}

func isZashboardClashEscapedPathSafe(request *http.Request) bool {
	if request == nil || request.URL == nil {
		return false
	}
	if request.URL.RawPath != "" {
		decodedPath, err := url.PathUnescape(request.URL.RawPath)
		if err != nil || decodedPath != request.URL.Path {
			return false
		}
	}
	return !hasForbiddenZashboardPathEscape(request.URL.EscapedPath())
}

func hasForbiddenZashboardPathEscape(escapedPath string) bool {
	for index := 0; index < len(escapedPath); index++ {
		if escapedPath[index] != '%' {
			continue
		}
		if index+2 >= len(escapedPath) {
			return true
		}
		value, valid := decodeHexByte(escapedPath[index+1], escapedPath[index+2])
		if !valid {
			return true
		}
		switch value {
		case 0, '.', '/', '\\', '%':
			return true
		}
		index += 2
	}
	return false
}

func decodeHexByte(high byte, low byte) (byte, bool) {
	highValue, highValid := decodeHexDigit(high)
	lowValue, lowValid := decodeHexDigit(low)
	return highValue<<4 | lowValue, highValid && lowValid
}

func decodeHexDigit(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func isWebSocketRequest(request *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(request.Header.Get("Upgrade")), "websocket") &&
		headerContainsToken(request.Header.Values("Connection"), "upgrade")
}

func hasUnsupportedUpgrade(request *http.Request, webSocket bool) bool {
	if webSocket {
		return false
	}
	return strings.TrimSpace(request.Header.Get("Upgrade")) != "" || headerContainsToken(request.Header.Values("Connection"), "upgrade")
}

func headerContainsToken(values []string, expected string) bool {
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), expected) {
				return true
			}
		}
	}
	return false
}

type activityDeadlineConn struct {
	net.Conn
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func (connection *activityDeadlineConn) Read(buffer []byte) (int, error) {
	if connection.readTimeout > 0 {
		if err := connection.SetReadDeadline(time.Now().Add(connection.readTimeout)); err != nil {
			return 0, err
		}
	}
	return connection.Conn.Read(buffer)
}

func (connection *activityDeadlineConn) Write(buffer []byte) (int, error) {
	if connection.writeTimeout > 0 {
		if err := connection.SetWriteDeadline(time.Now().Add(connection.writeTimeout)); err != nil {
			return 0, err
		}
	}
	return connection.Conn.Write(buffer)
}

type activityDeadlineResponseWriter struct {
	gin.ResponseWriter
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func (writer *activityDeadlineResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	connection, buffered, err := writer.ResponseWriter.Hijack()
	if err != nil {
		return nil, nil, err
	}
	if err = connection.SetDeadline(time.Time{}); err != nil {
		_ = connection.Close()
		return nil, nil, err
	}
	if writer.writeTimeout > 0 {
		if err = connection.SetWriteDeadline(time.Now().Add(writer.writeTimeout)); err != nil {
			_ = connection.Close()
			return nil, nil, err
		}
	}
	return &activityDeadlineConn{
		Conn:         connection,
		readTimeout:  writer.readTimeout,
		writeTimeout: writer.writeTimeout,
	}, buffered, nil
}

func (writer *activityDeadlineResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func newZashboardWebSocketTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = zashboardUpstreamResponseHeaderTimeout
	dialer := &net.Dialer{
		Timeout:   zashboardUpstreamDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
		connection, err := dialer.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &activityDeadlineConn{
			Conn:         connection,
			readTimeout:  zashboardWebSocketIdleTimeout,
			writeTimeout: zashboardWebSocketWriteTimeout,
		}, nil
	}
	return transport
}
