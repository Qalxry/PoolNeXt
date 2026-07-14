package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"poolx/internal/pkg/common"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestGitHubOAuthStartIssuesStateAndRejectsOpenRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousEnabled := common.GitHubOAuthEnabled
	previousClientID := common.GitHubClientId
	common.GitHubOAuthEnabled = true
	common.GitHubClientId = "client-id"
	t.Cleanup(func() {
		common.GitHubOAuthEnabled = previousEnabled
		common.GitHubClientId = previousClientID
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("test-secret-with-at-least-32-bytes"))))
	engine.GET("/api/oauth/github", GitHubOAuth)

	badRequest := httptest.NewRequest(http.MethodGet, "/api/oauth/github?redirect=%2F%2Fevil.example", nil)
	badResult := httptest.NewRecorder()
	engine.ServeHTTP(badResult, badRequest)
	if !strings.Contains(badResult.Body.String(), "redirect") || !strings.Contains(badResult.Body.String(), `"success":false`) {
		t.Fatalf("open redirect was not rejected: %s", badResult.Body.String())
	}

	startRequest := httptest.NewRequest(http.MethodGet, "/api/oauth/github?redirect=%2Fnodes", nil)
	startResult := httptest.NewRecorder()
	engine.ServeHTTP(startResult, startRequest)
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			AuthorizeURL string `json:"authorize_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(startResult.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if !envelope.Success {
		t.Fatalf("OAuth start failed: %s", startResult.Body.String())
	}
	authorizeURL, err := url.Parse(envelope.Data.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	state := authorizeURL.Query().Get("state")
	if len(state) < 24 {
		t.Fatalf("OAuth state is missing or weak: %q", state)
	}

	callbackRequest := httptest.NewRequest(http.MethodGet, "/api/oauth/github?code=test-code&state=wrong-state", nil)
	for _, cookieValue := range startResult.Result().Cookies() {
		callbackRequest.AddCookie(cookieValue)
	}
	callbackResult := httptest.NewRecorder()
	engine.ServeHTTP(callbackResult, callbackRequest)
	if !strings.Contains(callbackResult.Body.String(), `"success":false`) || !strings.Contains(callbackResult.Body.String(), "state") {
		t.Fatalf("invalid OAuth state was not rejected: %s", callbackResult.Body.String())
	}
}
