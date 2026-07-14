package middleware

import (
	"poolx/internal/pkg/common"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const turnstileOrigin = "https://challenges.cloudflare.com"

func buildContentSecurityPolicy(turnstileEnabled bool) string {
	scriptSources := "'self' 'unsafe-inline'"
	frameSources := "'self'"
	connectSources := "'self' ws: wss:"
	if turnstileEnabled {
		scriptSources += " " + turnstileOrigin
		frameSources += " " + turnstileOrigin
		connectSources += " " + turnstileOrigin
	}

	return strings.Join([]string{
		"default-src 'self'",
		"base-uri 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"script-src " + scriptSources,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https:",
		"font-src 'self' data:",
		"connect-src " + connectSources,
		"frame-src " + frameSources,
		"media-src 'self' blob:",
	}, "; ") + ";"
}

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		common.OptionMapRWMutex.RLock()
		turnstileEnabled := common.TurnstileCheckEnabled
		common.OptionMapRWMutex.RUnlock()

		header := c.Writer.Header()
		header.Set("Content-Security-Policy", buildContentSecurityPolicy(turnstileEnabled))
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"http://localhost:3000"}
	return cors.New(config)
}
