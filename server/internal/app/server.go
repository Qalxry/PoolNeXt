package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	redissessions "github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
	"poolx/internal/middleware"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/utils/geoip"
	"poolx/internal/router"
	"poolx/internal/service"
)

const (
	allowEphemeralSessionSecretEnv = "POOLNEXT_ALLOW_EPHEMERAL_SESSION_SECRET"
	trustedProxiesEnv              = "TRUSTED_PROXIES"
	maxRequestBodyBytesEnv         = "MAX_REQUEST_BODY_BYTES"
	maxUploadBodyBytesEnv          = "MAX_UPLOAD_BODY_BYTES"
	maxExpensiveRequestsEnv        = "MAX_EXPENSIVE_REQUESTS"

	defaultMaxRequestBodyBytes  int64 = 8 << 20
	defaultMaxUploadBodyBytes   int64 = 128 << 20
	defaultMaxMultipartMemory   int64 = 8 << 20
	defaultMaxExpensiveRequests       = 4

	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 2 * time.Minute
	defaultWriteTimeout      = 5 * time.Minute
	defaultIdleTimeout       = 2 * time.Minute
	defaultShutdownTimeout   = 20 * time.Second
	defaultMaxHeaderBytes    = 64 << 10
)

type httpRuntimeConfig struct {
	TrustedProxies       []string
	MaxRequestBodyBytes  int64
	MaxUploadBodyBytes   int64
	MaxExpensiveRequests int
	ShutdownTimeout      time.Duration
}

func RunServer(assetFS fs.FS, buildDir string, indexPage []byte, zashboardDir string) {
	common.SetupGinLog()
	slog.Info("PoolNeXt started", "version", common.Version)
	if os.Getenv("GIN_MODE") == gin.DebugMode {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	httpConfig, err := loadHTTPRuntimeConfig(os.LookupEnv)
	if err != nil {
		slog.Error("invalid HTTP server configuration", "error", err)
		os.Exit(1)
	}

	sessionSecret, ephemeralSecret, err := resolveSessionSecret(
		os.LookupEnv,
		common.SessionSecret,
		filepath.Base(os.Args[0]),
		gin.Mode(),
	)
	if err != nil {
		slog.Error("invalid session secret", "error", err)
		os.Exit(1)
	}
	common.SessionSecret = sessionSecret
	if ephemeralSecret {
		slog.Warn("using an ephemeral session secret; sessions will be invalidated on restart")
	}

	engine := gin.Default()
	engine.MaxMultipartMemory = min(defaultMaxMultipartMemory, httpConfig.MaxUploadBodyBytes)
	if err = configureTrustedProxies(engine, httpConfig.TrustedProxies); err != nil {
		slog.Error("configure trusted proxies failed", "error", err)
		os.Exit(1)
	}
	if hasUniversalTrustedProxy(httpConfig.TrustedProxies) {
		slog.Warn("all forwarding proxies are trusted; client IP headers can be spoofed unless an upstream overwrites them")
	}
	engine.Use(middleware.SecurityHeaders())
	engine.Use(middleware.CORS())
	engine.Use(requestBodyLimit(httpConfig.MaxRequestBodyBytes, httpConfig.MaxUploadBodyBytes))
	engine.Use(middleware.ExpensiveOperationGate(httpConfig.MaxExpensiveRequests))

	if err = model.InitDB(); err != nil {
		slog.Error("initialize database failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := model.CloseDB(); closeErr != nil {
			slog.Error("close database failed", "error", closeErr)
		}
	}()

	if err = common.InitRedisClient(); err != nil {
		slog.Error("initialize redis failed", "error", err)
		os.Exit(1)
	}
	if common.RDB != nil {
		defer func() {
			if closeErr := common.RDB.Close(); closeErr != nil {
				slog.Warn("close redis client failed", "error", closeErr)
			}
		}()
	}

	model.InitOptionMap()
	geoip.InitGeoIP()
	if err = service.AppLog.Push(model.AppLogClassificationSystem, model.AppLogLevelInfo, fmt.Sprintf("PoolNeXt server started | version=%s | db_backend=%s | redis_enabled=%t", common.Version, dbBackend(common.SQLDSN), common.RedisEnabled)); err != nil {
		slog.Warn("write startup app log failed", "error", err)
	}

	store, err := newSessionStore(sessionSecret)
	if err != nil {
		slog.Error("initialize session store failed", "error", err)
		os.Exit(1)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer func() {
			if closeErr := closer.Close(); closeErr != nil {
				slog.Warn("close session store failed", "error", closeErr)
			}
		}()
	}
	engine.Use(sessions.Sessions("session", store))

	router.SetRouter(engine, assetFS, buildDir, indexPage, zashboardDir)
	port := os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(*common.Port)
	}
	address := ":" + port

	requestBaseContext, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	httpServer := newHTTPServer(address, engine, requestBaseContext)
	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	slog.Info(
		"server config",
		"address", address,
		"gin_mode", gin.Mode(),
		"log_level", common.GetLogLevel(),
		"db_backend", dbBackend(common.SQLDSN),
		"sqlite_path", common.SQLitePath,
		"redis_enabled", common.RedisEnabled,
		"upload_path", common.UploadPath,
		"log_dir", valueOrDefault(*common.LogDir, "stdout"),
		"trusted_proxy_count", len(httpConfig.TrustedProxies),
		"max_request_body_bytes", httpConfig.MaxRequestBodyBytes,
		"max_upload_body_bytes", httpConfig.MaxUploadBodyBytes,
		"max_expensive_requests", httpConfig.MaxExpensiveRequests,
		"read_header_timeout", httpServer.ReadHeaderTimeout,
		"read_timeout", httpServer.ReadTimeout,
		"write_timeout", httpServer.WriteTimeout,
		"idle_timeout", httpServer.IdleTimeout,
	)
	slog.Info("server listening", "address", address)
	if err = serveHTTP(signalContext, httpServer, httpConfig.ShutdownTimeout); err != nil {
		slog.Error("server stopped with error", "error", err)
		return
	}
	slog.Info("server stopped")
}

func newSessionStore(sessionSecret string) (sessions.Store, error) {
	if common.RedisEnabled {
		opt := common.ParseRedisOption()
		return redissessions.NewStore(opt.MinIdleConns, opt.Network, opt.Addr, opt.Password, []byte(sessionSecret))
	}
	return cookie.NewStore([]byte(sessionSecret)), nil
}

func loadHTTPRuntimeConfig(lookupEnv func(string) (string, bool)) (httpRuntimeConfig, error) {
	maxRequestBodyBytes, err := positiveInt64Env(lookupEnv, maxRequestBodyBytesEnv, defaultMaxRequestBodyBytes)
	if err != nil {
		return httpRuntimeConfig{}, err
	}
	maxUploadBodyBytes, err := positiveInt64Env(lookupEnv, maxUploadBodyBytesEnv, defaultMaxUploadBodyBytes)
	if err != nil {
		return httpRuntimeConfig{}, err
	}
	maxExpensiveRequestsValue, err := positiveInt64Env(lookupEnv, maxExpensiveRequestsEnv, defaultMaxExpensiveRequests)
	if err != nil {
		return httpRuntimeConfig{}, err
	}
	if maxExpensiveRequestsValue > 1024 {
		return httpRuntimeConfig{}, fmt.Errorf("%s cannot exceed 1024", maxExpensiveRequestsEnv)
	}

	trustedProxies, err := trustedProxiesFromEnv(lookupEnv)
	if err != nil {
		return httpRuntimeConfig{}, err
	}
	return httpRuntimeConfig{
		TrustedProxies:       trustedProxies,
		MaxRequestBodyBytes:  maxRequestBodyBytes,
		MaxUploadBodyBytes:   maxUploadBodyBytes,
		MaxExpensiveRequests: int(maxExpensiveRequestsValue),
		ShutdownTimeout:      defaultShutdownTimeout,
	}, nil
}

func positiveInt64Env(lookupEnv func(string) (string, bool), name string, fallback int64) (int64, error) {
	rawValue, exists := lookupEnv(name)
	if !exists || strings.TrimSpace(rawValue) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(rawValue), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func trustedProxiesFromEnv(lookupEnv func(string) (string, bool)) ([]string, error) {
	rawValue, exists := lookupEnv(trustedProxiesEnv)
	if !exists || strings.TrimSpace(rawValue) == "" || strings.EqualFold(strings.TrimSpace(rawValue), "none") {
		return nil, nil
	}

	parts := strings.Split(rawValue, ",")
	trustedProxies := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		proxy := strings.TrimSpace(part)
		if proxy == "" {
			return nil, fmt.Errorf("%s contains an empty proxy entry", trustedProxiesEnv)
		}
		if strings.EqualFold(proxy, "none") {
			return nil, fmt.Errorf("%s cannot combine none with proxy addresses", trustedProxiesEnv)
		}
		if _, ok := seen[proxy]; ok {
			continue
		}
		seen[proxy] = struct{}{}
		trustedProxies = append(trustedProxies, proxy)
	}
	return trustedProxies, nil
}

func configureTrustedProxies(engine *gin.Engine, trustedProxies []string) error {
	engine.ForwardedByClientIP = len(trustedProxies) > 0
	return engine.SetTrustedProxies(trustedProxies)
}

func hasUniversalTrustedProxy(trustedProxies []string) bool {
	for _, proxy := range trustedProxies {
		switch strings.TrimSpace(proxy) {
		case "0.0.0.0/0", "::/0":
			return true
		}
	}
	return false
}

func resolveSessionSecret(
	lookupEnv func(string) (string, bool),
	generatedSecret string,
	executableName string,
	ginMode string,
) (string, bool, error) {
	if configuredSecret, exists := lookupEnv("SESSION_SECRET"); exists {
		if err := validateSessionSecret(configuredSecret); err != nil {
			return "", false, err
		}
		return configuredSecret, false, nil
	}

	allowEphemeral := false
	if rawValue, exists := lookupEnv(allowEphemeralSessionSecretEnv); exists {
		parsed, err := strconv.ParseBool(strings.TrimSpace(rawValue))
		if err != nil {
			return "", false, fmt.Errorf("%s must be a boolean", allowEphemeralSessionSecretEnv)
		}
		allowEphemeral = parsed
	}
	if !allowEphemeral {
		return "", false, fmt.Errorf("SESSION_SECRET must be explicitly configured with at least 32 strong characters")
	}

	isTestExecutable := strings.Contains(strings.ToLower(executableName), ".test")
	if ginMode != gin.DebugMode && !isTestExecutable {
		return "", false, fmt.Errorf("%s is only permitted in debug mode or test binaries", allowEphemeralSessionSecretEnv)
	}
	if err := validateSessionSecret(generatedSecret); err != nil {
		return "", false, fmt.Errorf("generated ephemeral session secret is invalid: %w", err)
	}
	return generatedSecret, true, nil
}

func validateSessionSecret(secret string) error {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return fmt.Errorf("SESSION_SECRET cannot be blank")
	}
	if trimmed != secret {
		return fmt.Errorf("SESSION_SECRET cannot have leading or trailing whitespace")
	}
	if len([]byte(secret)) < 32 {
		return fmt.Errorf("SESSION_SECRET must contain at least 32 bytes")
	}

	lowerSecret := strings.ToLower(secret)
	knownWeakValues := []string{
		"replace-with-random-string",
		"replace_me_with_a_random_string",
		"change-me",
		"changeme",
		"session-secret",
		"your-session-secret",
		"example-session-secret",
	}
	for _, weakValue := range knownWeakValues {
		if lowerSecret == weakValue || strings.Contains(lowerSecret, weakValue) {
			return fmt.Errorf("SESSION_SECRET uses a known placeholder value")
		}
	}

	uniqueCharacters := make(map[rune]struct{})
	for _, character := range secret {
		uniqueCharacters[character] = struct{}{}
	}
	if len(uniqueCharacters) < 8 {
		return fmt.Errorf("SESSION_SECRET has insufficient character diversity")
	}
	return nil
}

func requestBodyLimit(maxRequestBodyBytes int64, maxUploadBodyBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}

		limit := maxRequestBodyBytes
		if isUploadRequest(c.Request) {
			limit = maxUploadBodyBytes
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"success": false,
				"message": "request body too large",
			})
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func isUploadRequest(request *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(request.Header.Get("Content-Type")))
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return false
	}

	switch request.URL.Path {
	case "/api/file", "/api/file/",
		"/api/update/manual-upload",
		"/api/kernel/mihomo/upload",
		"/api/source-configs/parse":
		return request.Method == http.MethodPost
	default:
		return false
	}
}

func newHTTPServer(address string, handler http.Handler, baseContext context.Context) *http.Server {
	if baseContext == nil {
		baseContext = context.Background()
	}
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
		BaseContext: func(net.Listener) context.Context {
			return baseContext
		},
	}
}

func serveHTTP(ctx context.Context, server *http.Server, shutdownTimeout time.Duration) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	return serveHTTPListener(ctx, server, listener, shutdownTimeout)
}

func serveHTTPListener(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		return normalizeHTTPServerError(err)
	case <-ctx.Done():
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	shutdownErr := server.Shutdown(shutdownContext)
	cancelShutdown()
	if shutdownErr != nil {
		closeErr := server.Close()
		serveErr := normalizeHTTPServerError(<-serveErrors)
		if closeErr != nil || serveErr != nil {
			return errors.Join(
				fmt.Errorf("graceful HTTP shutdown failed: %w", shutdownErr),
				closeErr,
				serveErr,
			)
		}
		return fmt.Errorf("graceful HTTP shutdown failed: %w", shutdownErr)
	}

	return normalizeHTTPServerError(<-serveErrors)
}

func normalizeHTTPServerError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func valueOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func dbBackend(dsn string) string {
	if dsn != "" {
		return "postgres"
	}
	return "sqlite"
}
