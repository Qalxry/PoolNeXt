package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupAuthTest(t *testing.T) (*gin.Engine, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open auth test database: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate auth test database: %v", err)
	}
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	user := &model.User{
		Username:    "root",
		Password:    "secure-password",
		DisplayName: "Root",
		Role:        common.RoleRootUser,
		Status:      common.UserStatusEnabled,
		AuthVersion: 1,
	}
	if err := user.Insert(); err != nil {
		t.Fatalf("insert auth test user: %v", err)
	}

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("test-secret-with-at-least-32-bytes"))))
	engine.POST("/session", func(c *gin.Context) {
		if err := SetAuthenticatedSession(c, user); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	engine.GET("/api/root", RootAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	engine.GET("/api/user/self", UserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return engine, user
}

func TestSessionCookiePolicyAndRevocation(t *testing.T) {
	engine, user := setupAuthTest(t)

	loginRequest := httptest.NewRequest(http.MethodPost, "https://poolnext.test/session", nil)
	loginRequest.TLS = &tls.ConnectionState{}
	loginResult := httptest.NewRecorder()
	engine.ServeHTTP(loginResult, loginRequest)
	if loginResult.Code != http.StatusNoContent {
		t.Fatalf("create session status: %d", loginResult.Code)
	}
	setCookie := loginResult.Header().Get("Set-Cookie")
	for _, attribute := range []string{"HttpOnly", "Secure", "SameSite=Lax", "Max-Age=43200"} {
		if !strings.Contains(setCookie, attribute) {
			t.Fatalf("session cookie missing %s: %s", attribute, setCookie)
		}
	}
	cookies := loginResult.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session cookie was not issued")
	}

	protectedRequest := httptest.NewRequest(http.MethodGet, "https://poolnext.test/api/root", nil)
	protectedRequest.TLS = &tls.ConnectionState{}
	protectedRequest.AddCookie(cookies[0])
	protectedResult := httptest.NewRecorder()
	engine.ServeHTTP(protectedResult, protectedRequest)
	if protectedResult.Code != http.StatusNoContent {
		t.Fatalf("valid session status: %d body=%s", protectedResult.Code, protectedResult.Body.String())
	}

	user.Password = "another-password"
	if err := user.UpdateWithPasswordPolicy(true, false); err != nil {
		t.Fatalf("change password: %v", err)
	}
	revokedRequest := httptest.NewRequest(http.MethodGet, "https://poolnext.test/api/root", nil)
	revokedRequest.TLS = &tls.ConnectionState{}
	revokedRequest.AddCookie(cookies[0])
	revokedResult := httptest.NewRecorder()
	engine.ServeHTTP(revokedResult, revokedRequest)
	if revokedResult.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status: %d body=%s", revokedResult.Code, revokedResult.Body.String())
	}
}

func TestSessionCookiePolicyUsesTrustedForwardedHTTPS(t *testing.T) {
	t.Setenv("SESSION_COOKIE_SECURE", "")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8")
	engine, _ := setupAuthTest(t)

	request := httptest.NewRequest(http.MethodPost, "http://poolnext.test/session", nil)
	request.RemoteAddr = "10.12.0.4:44321"
	request.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("create proxied session status: %d", response.Code)
	}
	if setCookie := response.Header().Get("Set-Cookie"); !strings.Contains(setCookie, "Secure") {
		t.Fatalf("trusted HTTPS proxy session cookie is not Secure: %s", setCookie)
	}
}

func TestForcedPasswordChangeLimitsAuthenticatedRoutes(t *testing.T) {
	engine, user := setupAuthTest(t)
	user.MustChangePassword = true
	if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark password change required: %v", err)
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/session", nil)
	loginResult := httptest.NewRecorder()
	engine.ServeHTTP(loginResult, loginRequest)
	cookieValue := loginResult.Result().Cookies()[0]

	protectedRequest := httptest.NewRequest(http.MethodGet, "/api/root", nil)
	protectedRequest.AddCookie(cookieValue)
	protectedResult := httptest.NewRecorder()
	engine.ServeHTTP(protectedResult, protectedRequest)
	if protectedResult.Code != http.StatusForbidden {
		t.Fatalf("forced password route status: %d body=%s", protectedResult.Code, protectedResult.Body.String())
	}

	selfRequest := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
	selfRequest.AddCookie(cookieValue)
	selfResult := httptest.NewRecorder()
	engine.ServeHTTP(selfResult, selfRequest)
	if selfResult.Code != http.StatusNoContent {
		t.Fatalf("forced password self route status: %d body=%s", selfResult.Code, selfResult.Body.String())
	}
}

func TestDeletedUserSessionIsRejected(t *testing.T) {
	engine, user := setupAuthTest(t)

	loginRequest := httptest.NewRequest(http.MethodPost, "/session", nil)
	loginResult := httptest.NewRecorder()
	engine.ServeHTTP(loginResult, loginRequest)
	cookieValue := loginResult.Result().Cookies()[0]

	if err := user.Delete(); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	protectedRequest := httptest.NewRequest(http.MethodGet, "/api/root", nil)
	protectedRequest.AddCookie(cookieValue)
	protectedResult := httptest.NewRecorder()
	engine.ServeHTTP(protectedResult, protectedRequest)
	if protectedResult.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user session status: %d body=%s", protectedResult.Code, protectedResult.Body.String())
	}
}

func TestExplicitInvalidBearerDoesNotFallBackToSession(t *testing.T) {
	engine, _ := setupAuthTest(t)

	loginRequest := httptest.NewRequest(http.MethodPost, "/session", nil)
	loginResult := httptest.NewRecorder()
	engine.ServeHTTP(loginResult, loginRequest)
	cookieValue := loginResult.Result().Cookies()[0]

	protectedRequest := httptest.NewRequest(http.MethodGet, "/api/root", nil)
	protectedRequest.AddCookie(cookieValue)
	protectedRequest.Header.Set("Authorization", "Bearer invalid-token")
	protectedResult := httptest.NewRecorder()
	engine.ServeHTTP(protectedResult, protectedRequest)
	if protectedResult.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer status: %d body=%s", protectedResult.Code, protectedResult.Body.String())
	}
}

func TestSessionCookieSecureTrustedProxyPolicy(t *testing.T) {
	tests := []struct {
		name           string
		secureOverride string
		trustedProxies string
		remoteAddress  string
		forwardedProto string
		directTLS      bool
		expectedSecure bool
	}{
		{
			name:           "direct TLS",
			directTLS:      true,
			expectedSecure: true,
		},
		{
			name:           "trusted IPv4 proxy with HTTPS",
			trustedProxies: "10.0.0.0/8",
			remoteAddress:  "10.12.0.4:44321",
			forwardedProto: "https",
			expectedSecure: true,
		},
		{
			name:           "trusted IPv6 proxy with HTTPS",
			trustedProxies: "2001:db8::/32",
			remoteAddress:  "[2001:db8::42]:44321",
			forwardedProto: "HTTPS",
			expectedSecure: true,
		},
		{
			name:           "untrusted proxy header is ignored",
			trustedProxies: "10.0.0.0/8",
			remoteAddress:  "198.51.100.7:44321",
			forwardedProto: "https",
			expectedSecure: false,
		},
		{
			name:           "mixed forwarded protocols fail closed",
			trustedProxies: "10.0.0.0/8",
			remoteAddress:  "10.12.0.4:44321",
			forwardedProto: "https, http",
			expectedSecure: false,
		},
		{
			name:           "explicit true overrides transport",
			secureOverride: "true",
			expectedSecure: true,
		},
		{
			name:           "explicit false overrides direct TLS",
			secureOverride: "false",
			directTLS:      true,
			expectedSecure: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SESSION_COOKIE_SECURE", test.secureOverride)
			t.Setenv("TRUSTED_PROXIES", test.trustedProxies)

			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			request := httptest.NewRequest(http.MethodGet, "http://poolnext.test/session", nil)
			if test.remoteAddress != "" {
				request.RemoteAddr = test.remoteAddress
			}
			if test.forwardedProto != "" {
				request.Header.Set("X-Forwarded-Proto", test.forwardedProto)
			}
			if test.directTLS {
				request.TLS = &tls.ConnectionState{}
			}
			context.Request = request

			if got := sessionCookieSecure(context); got != test.expectedSecure {
				t.Fatalf("sessionCookieSecure() = %v, want %v", got, test.expectedSecure)
			}
		})
	}
}
