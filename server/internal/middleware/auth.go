package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"strconv"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const SessionLifetimeSeconds = 12 * 60 * 60

var ErrInvalidSession = errors.New("session is missing, expired, or revoked")

func sessionCookieSecure(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SESSION_COOKIE_SECURE"))) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return c.Request.TLS != nil || requestUsesTrustedForwardedHTTPS(c.Request)
	}
}

func requestUsesTrustedForwardedHTTPS(request *http.Request) bool {
	if request == nil || !requestFromTrustedProxy(request) {
		return false
	}

	seenProtocol := false
	for _, headerValue := range request.Header.Values("X-Forwarded-Proto") {
		for _, protocol := range strings.Split(headerValue, ",") {
			protocol = strings.TrimSpace(protocol)
			if protocol == "" || !strings.EqualFold(protocol, "https") {
				return false
			}
			seenProtocol = true
		}
	}
	return seenProtocol
}

func requestFromTrustedProxy(request *http.Request) bool {
	remoteIP := remoteRequestIP(request)
	if remoteIP == nil {
		return false
	}

	for _, entry := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" || strings.EqualFold(entry, "none") {
			continue
		}
		if trustedIP := net.ParseIP(entry); trustedIP != nil {
			if trustedIP.Equal(remoteIP) {
				return true
			}
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(remoteIP) {
			return true
		}
	}
	return false
}

func remoteRequestIP(request *http.Request) net.IP {
	if request == nil {
		return nil
	}
	address := strings.TrimSpace(request.RemoteAddr)
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	address = strings.Trim(address, "[]")
	if zoneIndex := strings.LastIndex(address, "%"); zoneIndex >= 0 {
		address = address[:zoneIndex]
	}
	return net.ParseIP(address)
}

// ApplySessionCookiePolicy centralizes the browser credential policy. SameSite
// Lax permits the top-level GitHub OAuth callback while blocking most
// cross-site subrequests from carrying the session cookie.
func ApplySessionCookiePolicy(c *gin.Context, session sessions.Session) {
	session.Options(sessions.Options{
		Path:     "/",
		MaxAge:   SessionLifetimeSeconds,
		Secure:   sessionCookieSecure(c),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func SetAuthenticatedSession(c *gin.Context, user *model.User) error {
	if user == nil || user.Id == 0 || user.AuthVersion == 0 {
		return ErrInvalidSession
	}
	session := sessions.Default(c)
	ApplySessionCookiePolicy(c, session)
	session.Set("id", user.Id)
	session.Set("username", user.Username)
	session.Set("role", user.Role)
	session.Set("status", user.Status)
	session.Set("auth_version", user.AuthVersion)
	return session.Save()
}

func DestroySession(c *gin.Context) error {
	session := sessions.Default(c)
	session.Clear()
	session.Options(sessions.Options{
		Path:     "/",
		MaxAge:   -1,
		Secure:   sessionCookieSecure(c),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return session.Save()
}

func sessionInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > uint64(^uint(0)>>1) {
			return 0, false
		}
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// CurrentSessionUser treats the database as the authorization source of truth
// and only uses the signed session cookie as an identifier plus revocation
// version. Role and status values stored by older clients are never trusted.
func CurrentSessionUser(c *gin.Context) (*model.User, error) {
	session := sessions.Default(c)
	id, hasID := sessionInteger(session.Get("id"))
	version, hasVersion := sessionInteger(session.Get("auth_version"))
	if !hasID || !hasVersion || id <= 0 || version <= 0 {
		return nil, ErrInvalidSession
	}
	user, err := model.GetUserById(int(id), true)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidSession
		}
		return nil, fmt.Errorf("load session user: %w", err)
	}
	if user.AuthVersion != uint64(version) || user.Status != common.UserStatusEnabled {
		return nil, ErrInvalidSession
	}
	return user, nil
}

func forcedPasswordChangeAllowed(c *gin.Context) bool {
	switch c.Request.URL.Path {
	case "/api/user/self", "/api/user/self/update", "/api/user/logout":
		return true
	default:
		return false
	}
}

func abortUnauthorized(c *gin.Context, message string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"success": false,
		"message": message,
	})
	c.Abort()
}

func abortForbidden(c *gin.Context, message string) {
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": message,
	})
	c.Abort()
}

func authHelper(c *gin.Context, minRole int) {
	var user *model.User
	authByToken := false

	if token := c.Request.Header.Get("Authorization"); token != "" {
		user = model.ValidateUserToken(token)
		if user == nil {
			abortUnauthorized(c, "无权进行此操作，token 无效")
			return
		}
		authByToken = true
	} else if sessions.Default(c).Get("id") != nil {
		var err error
		user, err = CurrentSessionUser(c)
		if err != nil {
			_ = DestroySession(c)
			abortUnauthorized(c, "会话已过期或已被撤销，请重新登录")
			return
		}
	} else {
		abortUnauthorized(c, "无权进行此操作，未登录或 token 无效")
		return
	}

	if user.MustChangePassword && !forcedPasswordChangeAllowed(c) {
		abortForbidden(c, "首次登录或密码重置后必须先修改密码")
		return
	}
	if user.Role < minRole {
		abortForbidden(c, "无权进行此操作，权限不足")
		return
	}
	c.Set("username", user.Username)
	c.Set("role", user.Role)
	c.Set("id", user.Id)
	c.Set("authByToken", authByToken)
	c.Set("mustChangePassword", user.MustChangePassword)
	c.Next()
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCommonUser)
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleAdminUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleRootUser)
	}
}

// NoTokenAuth You should always use this after normal auth middlewares.
func NoTokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		if c.GetBool("authByToken") {
			abortForbidden(c, "本接口不支持使用 token 进行验证")
			return
		}
		c.Next()
	}
}

// TokenOnlyAuth You should always use this after normal auth middlewares.
func TokenOnlyAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !c.GetBool("authByToken") {
			abortForbidden(c, "本接口仅支持使用 token 进行验证")
			return
		}
		c.Next()
	}
}
