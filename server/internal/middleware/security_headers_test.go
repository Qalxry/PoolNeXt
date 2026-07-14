package middleware

import (
	"net/http"
	"net/http/httptest"
	"poolx/internal/pkg/common"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeaders(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalTurnstileEnabled := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = false
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.TurnstileCheckEnabled = originalTurnstileEnabled
		common.OptionMapRWMutex.Unlock()
	})

	engine := gin.New()
	engine.Use(SecurityHeaders())
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "object-src 'none'") || !strings.Contains(policy, "frame-ancestors 'none'") {
		t.Fatalf("Content-Security-Policy = %q", policy)
	}
	if strings.Contains(policy, turnstileOrigin) {
		t.Fatalf("default Content-Security-Policy unexpectedly allows Turnstile: %q", policy)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := response.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
}

func TestSecurityHeadersAllowTurnstileOnlyWhenEnabled(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	originalTurnstileEnabled := common.TurnstileCheckEnabled
	common.TurnstileCheckEnabled = true
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.TurnstileCheckEnabled = originalTurnstileEnabled
		common.OptionMapRWMutex.Unlock()
	})

	engine := gin.New()
	engine.Use(SecurityHeaders())
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	policy := response.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"script-src 'self' 'unsafe-inline' " + turnstileOrigin,
		"connect-src 'self' ws: wss: " + turnstileOrigin,
		"frame-src 'self' " + turnstileOrigin,
	} {
		if !strings.Contains(policy, directive) {
			t.Fatalf("Content-Security-Policy missing %q: %q", directive, policy)
		}
	}
	if strings.Contains(policy, "*.cloudflare.com") || strings.Contains(policy, "script-src 'self' 'unsafe-inline' https:;") {
		t.Fatalf("Turnstile policy is broader than the exact challenge origin: %q", policy)
	}
}
