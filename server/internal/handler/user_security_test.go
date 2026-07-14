package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

func TestRequireSameOriginMutationRejectsCrossSiteRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name        string
		contentType string
		origin      string
		fetchSite   string
	}{
		{name: "missing origin", contentType: "application/json"},
		{name: "cross-site origin", contentType: "application/json", origin: "https://evil.example", fetchSite: "cross-site"},
		{name: "simple form", contentType: "application/x-www-form-urlencoded", origin: "https://poolnext.example", fetchSite: "same-origin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodPost, "https://poolnext.example/api/oauth/email/bind", strings.NewReader(`{"email":"user@example.com","code":"123456"}`))
			request.Host = "poolnext.example"
			request.Header.Set("Content-Type", test.contentType)
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			}
			context.Request = request

			if requireSameOriginMutation(context) {
				t.Fatal("unsafe mutation request was accepted")
			}
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: got %d want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestRequireSameOriginMutationAcceptsSameOriginJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "https://poolnext.example/api/oauth/email/bind", strings.NewReader(`{}`))
	request.Host = "poolnext.example"
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Origin", "https://poolnext.example")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	context.Request = request

	if !requireSameOriginMutation(context) {
		t.Fatalf("same-origin JSON mutation was rejected: %s", recorder.Body.String())
	}
}

func TestAuthAuditValueRemovesControlsAndCapsLength(t *testing.T) {
	value := authAuditValue(strings.Repeat("a", authAuditFieldLimit+20) + "\nforged")
	if strings.ContainsAny(value, "\r\n\t") {
		t.Fatalf("audit value retained control characters: %q", value)
	}
	if utf8.RuneCountInString(value) > authAuditFieldLimit+3 {
		t.Fatalf("audit value was not capped: %d runes", utf8.RuneCountInString(value))
	}
}
