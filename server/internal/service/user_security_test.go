package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"poolx/internal/model"
	"poolx/internal/pkg/common"
	"poolx/internal/pkg/utils/security"
	"strings"
	"sync/atomic"
	"testing"
)

func setupUserServiceTestDB(t *testing.T) {
	t.Helper()
	originalSQLitePath := common.SQLitePath
	common.SQLitePath = filepath.Join(t.TempDir(), "user-service-test.db")
	if err := model.InitDB(); err != nil {
		t.Fatalf("init user service test db: %v", err)
	}
	model.InitOptionMap()
	t.Cleanup(func() {
		if err := model.CloseDB(); err != nil {
			t.Fatalf("close user service test db: %v", err)
		}
		model.DB = nil
		common.SQLitePath = originalSQLitePath
	})
}

func TestNormalizeLocalRedirect(t *testing.T) {
	valid, err := NormalizeLocalRedirect("/nodes?status=active#selected")
	if err != nil || valid != "/nodes?status=active#selected" {
		t.Fatalf("valid redirect rejected: value=%q err=%v", valid, err)
	}
	for _, redirect := range []string{
		"https://evil.example/path",
		"//evil.example/path",
		"/\\evil.example/path",
		"/%5cevil.example/path",
		"/%2f%2fevil.example/path",
		"/%0d%0aLocation:%20https://evil.example/path",
		"/%zz",
	} {
		if _, err := NormalizeLocalRedirect(redirect); err == nil {
			t.Fatalf("unsafe redirect accepted: %q", redirect)
		}
	}
}

func TestGenerateUserTokenCarriesCurrentCredentialVersion(t *testing.T) {
	setupUserServiceTestDB(t)

	root := model.User{Username: "root"}
	if err := root.FillUserByUsername(); err != nil {
		t.Fatalf("load root user: %v", err)
	}
	token, err := GenerateUserToken(root.Id)
	if err != nil {
		t.Fatalf("GenerateUserToken: %v", err)
	}
	if !strings.HasPrefix(token, "v1.") {
		t.Fatalf("token does not carry auth version: %q", token)
	}
	if model.ValidateUserToken("Bearer "+token) == nil {
		t.Fatal("fresh token was not accepted")
	}

	root.Password = "rotated-root-password"
	if err := root.UpdateWithPasswordPolicy(true, false); err != nil {
		t.Fatalf("rotate root password: %v", err)
	}
	if model.ValidateUserToken("Bearer "+token) != nil {
		t.Fatal("password change did not revoke the generated token")
	}
}

func TestBuildGitHubAuthorizationURLIncludesState(t *testing.T) {
	previousEnabled := common.GitHubOAuthEnabled
	previousClientID := common.GitHubClientId
	common.GitHubOAuthEnabled = true
	common.GitHubClientId = "client-id"
	t.Cleanup(func() {
		common.GitHubOAuthEnabled = previousEnabled
		common.GitHubClientId = previousClientID
	})

	value, err := BuildGitHubAuthorizationURL("opaque-state")
	if err != nil {
		t.Fatalf("BuildGitHubAuthorizationURL: %v", err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.Query().Get("state") != "opaque-state" {
		t.Fatalf("unexpected authorization URL: %s", value)
	}
}

func TestGitHubIdentityUsesStableNumericIDAndIgnoresLegacyLoginBinding(t *testing.T) {
	setupUserServiceTestDB(t)
	previousRegisterEnabled := common.RegisterEnabled
	common.RegisterEnabled = true
	t.Cleanup(func() { common.RegisterEnabled = previousRegisterEnabled })

	legacy := model.User{
		Username:    "legacy-gh",
		DisplayName: "Legacy",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	if err := legacy.Insert(); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if err := model.DB.Model(&legacy).Update("github_id", "octocat").Error; err != nil {
		t.Fatalf("seed legacy login binding: %v", err)
	}

	first, err := resolveGitHubUser(&GitHubUser{ID: 583231, Login: "octocat", Name: "Octocat"})
	if err != nil {
		t.Fatalf("resolve GitHub user: %v", err)
	}
	if first.Id == legacy.Id {
		t.Fatal("legacy mutable login binding was trusted as a GitHub identity")
	}
	if first.GitHubId != "583231" {
		t.Fatalf("GitHub numeric id was not persisted: %+v", first)
	}

	second, err := resolveGitHubUser(&GitHubUser{ID: 583231, Login: "renamed-account", Name: "Renamed"})
	if err != nil {
		t.Fatalf("resolve renamed GitHub user: %v", err)
	}
	if second.Id != first.Id {
		t.Fatalf("GitHub login rename created another user: first=%d second=%d", first.Id, second.Id)
	}
}

func TestGitHubHTTPRequestsUseCallerContext(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/token" {
			_, _ = writer.Write([]byte(`{"access_token":"token"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"id":42,"login":"user"}`))
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := getGitHubUserInfoByCodeWithClient(ctx, "code", upstream.Client(), upstream.URL+"/token", upstream.URL+"/user")
	if err == nil {
		t.Fatal("canceled GitHub request unexpectedly succeeded")
	}
	if calls.Load() != 0 {
		t.Fatalf("canceled context was not attached before the request, calls=%d", calls.Load())
	}
}

func TestEmailBindingConsumesCodeAndEnforcesUniqueIdentity(t *testing.T) {
	setupUserServiceTestDB(t)
	first := model.User{Username: "emailone", DisplayName: "One", Role: 1, Status: 1}
	second := model.User{Username: "emailtwo", DisplayName: "Two", Role: 1, Status: 1}
	if err := first.Insert(); err != nil {
		t.Fatalf("insert first user: %v", err)
	}
	if err := second.Insert(); err != nil {
		t.Fatalf("insert second user: %v", err)
	}

	security.RegisterVerificationCodeWithKey("user@example.com", "111111", security.EmailVerificationPurpose)
	if err := BindEmail(context.Background(), first.Id, " User@Example.COM ", "111111"); err != nil {
		t.Fatalf("bind first email: %v", err)
	}
	if err := BindEmail(context.Background(), first.Id, "user@example.com", "111111"); err == nil {
		t.Fatal("verification code was reusable")
	}

	security.RegisterVerificationCodeWithKey("user@example.com", "222222", security.EmailVerificationPurpose)
	if err := BindEmail(context.Background(), second.Id, "user@example.com", "222222"); err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("duplicate email binding was not rejected cleanly: %v", err)
	}
}

func TestPasswordResetTokenIsConsumedExactlyOnce(t *testing.T) {
	setupUserServiceTestDB(t)
	user := model.User{
		Username:    "resetuser",
		Password:    "old-password",
		DisplayName: "Reset",
		Role:        1,
		Status:      1,
		Email:       "reset@example.com",
	}
	if err := user.Insert(); err != nil {
		t.Fatalf("insert reset user: %v", err)
	}
	security.RegisterVerificationCodeWithKey(user.Email, "reset-token", security.PasswordResetPurpose)
	if _, err := ResetPassword(PasswordResetInput{Email: user.Email, Token: "reset-token"}); err != nil {
		t.Fatalf("first password reset failed: %v", err)
	}
	if _, err := ResetPassword(PasswordResetInput{Email: user.Email, Token: "reset-token"}); err == nil {
		t.Fatal("password reset token was reusable")
	}
}

func TestAdminMutationsEnforceRoleInTheWriteStatement(t *testing.T) {
	setupUserServiceTestDB(t)
	target := model.User{
		Username:    "peeradmin",
		Password:    "admin-password",
		DisplayName: "Peer",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
	}
	if err := target.Insert(); err != nil {
		t.Fatalf("insert target admin: %v", err)
	}

	update := model.User{Id: target.Id, DisplayName: "Changed", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	if err := UpdateUserAsAdmin(update, common.RoleAdminUser); err == nil {
		t.Fatal("peer admin update was accepted")
	}
	if err := DeleteUserAsAdmin(target.Id, common.RoleAdminUser); err == nil {
		t.Fatal("peer admin delete was accepted")
	}
	var persisted model.User
	if err := model.DB.First(&persisted, target.Id).Error; err != nil {
		t.Fatalf("load protected target: %v", err)
	}
	if persisted.DisplayName != "Peer" || persisted.Role != common.RoleAdminUser {
		t.Fatalf("forbidden admin mutation changed the target: %+v", persisted)
	}
}

func TestAuthenticationRejectsOversizedFields(t *testing.T) {
	previousEnabled := common.PasswordLoginEnabled
	common.PasswordLoginEnabled = true
	t.Cleanup(func() { common.PasswordLoginEnabled = previousEnabled })

	if _, err := AuthenticateUser(LoginInput{Username: strings.Repeat("u", maxUsernameLength+1), Password: "password"}); err == nil {
		t.Fatal("oversized login username was accepted")
	}
	if _, err := AuthenticateUser(LoginInput{Username: "user", Password: strings.Repeat("p", maxPasswordLength+1)}); err == nil {
		t.Fatal("oversized login password was accepted")
	}
}
