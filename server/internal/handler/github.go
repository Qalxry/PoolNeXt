package controller

import (
	"crypto/subtle"
	"fmt"
	"poolx/internal/middleware"
	"poolx/internal/model"
	"poolx/internal/pkg/utils/security"
	"poolx/internal/service"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	githubOAuthStateKey    = "github_oauth_state"
	githubOAuthIssuedAtKey = "github_oauth_issued_at"
	githubOAuthModeKey     = "github_oauth_mode"
	githubOAuthRedirectKey = "github_oauth_redirect"
	githubOAuthStateTTL    = 10 * time.Minute
	githubOAuthCodeMaxLen  = 1024
	githubOAuthStateMaxLen = 128
)

type gitHubOAuthStartResponse struct {
	AuthorizeURL string `json:"authorize_url"`
}

type gitHubOAuthResult struct {
	User     model.User `json:"user"`
	Redirect string     `json:"redirect"`
	Mode     string     `json:"mode"`
}

func GitHubOAuth(c *gin.Context) {
	if c.Query("code") == "" {
		startGitHubOAuth(c)
		return
	}
	completeGitHubOAuth(c)
}

func startGitHubOAuth(c *gin.Context) {
	mode := c.DefaultQuery("mode", "login")
	if mode != "login" && mode != "bind" {
		respondFailure(c, "无效的 OAuth 模式")
		return
	}
	if mode == "bind" {
		user, err := middleware.CurrentSessionUser(c)
		if err != nil {
			respondUnauthorized(c, "登录会话已过期，请重新登录")
			return
		}
		if user.MustChangePassword {
			respondFailure(c, "请先修改一次性密码，再绑定第三方账号")
			return
		}
	}
	redirect, err := service.NormalizeLocalRedirect(c.Query("redirect"))
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	state := security.GenerateRandomString(32)
	if state == "" {
		respondFailure(c, "无法创建 OAuth 会话，请重试")
		return
	}
	authorizeURL, err := service.BuildGitHubAuthorizationURL(state)
	if err != nil {
		respondFailure(c, err.Error())
		return
	}

	session := sessions.Default(c)
	middleware.ApplySessionCookiePolicy(c, session)
	session.Set(githubOAuthStateKey, state)
	session.Set(githubOAuthIssuedAtKey, time.Now().Unix())
	session.Set(githubOAuthModeKey, mode)
	session.Set(githubOAuthRedirectKey, redirect)
	if err := session.Save(); err != nil {
		respondFailure(c, "无法保存 OAuth state，请重试")
		return
	}
	respondSuccess(c, gitHubOAuthStartResponse{AuthorizeURL: authorizeURL})
}

func completeGitHubOAuth(c *gin.Context) {
	session := sessions.Default(c)
	expectedState, _ := session.Get(githubOAuthStateKey).(string)
	providedState := c.Query("state")
	code := c.Query("code")
	if len(code) > githubOAuthCodeMaxLen || len(providedState) > githubOAuthStateMaxLen {
		respondFailure(c, "GitHub OAuth 参数过长")
		return
	}
	if expectedState == "" || providedState == "" ||
		subtle.ConstantTimeCompare([]byte(expectedState), []byte(providedState)) != 1 {
		respondFailure(c, "GitHub OAuth state 无效，请重新发起登录")
		return
	}
	issuedAt, ok := oauthIssuedAt(session.Get(githubOAuthIssuedAtKey))
	if !ok || time.Since(time.Unix(issuedAt, 0)) > githubOAuthStateTTL || issuedAt > time.Now().Add(time.Minute).Unix() {
		clearGitHubOAuthState(c, session)
		respondFailure(c, "GitHub OAuth state 已过期，请重新发起登录")
		return
	}
	mode, _ := session.Get(githubOAuthModeKey).(string)
	redirect, err := service.NormalizeLocalRedirect(fmt.Sprint(session.Get(githubOAuthRedirectKey)))
	if err != nil || (mode != "login" && mode != "bind") {
		clearGitHubOAuthState(c, session)
		respondFailure(c, "GitHub OAuth 会话无效，请重新发起登录")
		return
	}
	if err := clearGitHubOAuthState(c, session); err != nil {
		respondFailure(c, "无法消费 OAuth state，请重试")
		return
	}

	if mode == "bind" {
		currentUser, err := middleware.CurrentSessionUser(c)
		if err != nil {
			respondUnauthorized(c, "登录会话已过期，请重新登录")
			return
		}
		if err := service.BindGitHubAccount(c.Request.Context(), currentUser.Id, code); err != nil {
			respondFailure(c, err.Error())
			return
		}
		updatedUser, err := model.GetUserById(currentUser.Id, true)
		if err != nil {
			respondFailure(c, err.Error())
			return
		}
		respondSuccess(c, gitHubOAuthResult{
			User:     cleanAuthUser(updatedUser),
			Redirect: redirect,
			Mode:     mode,
		})
		return
	}

	user, err := service.HandleGitHubOAuth(c.Request.Context(), code)
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	if err := middleware.SetAuthenticatedSession(c, user); err != nil {
		respondFailure(c, "无法保存会话信息，请重试")
		return
	}
	respondSuccess(c, gitHubOAuthResult{
		User:     cleanAuthUser(user),
		Redirect: redirect,
		Mode:     mode,
	})
}

func clearGitHubOAuthState(c *gin.Context, session sessions.Session) error {
	middleware.ApplySessionCookiePolicy(c, session)
	session.Delete(githubOAuthStateKey)
	session.Delete(githubOAuthIssuedAtKey)
	session.Delete(githubOAuthModeKey)
	session.Delete(githubOAuthRedirectKey)
	return session.Save()
}

func oauthIssuedAt(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case uint64:
		return int64(typed), true
	default:
		return 0, false
	}
}
