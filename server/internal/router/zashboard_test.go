package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestZashboardProxyRouterRegistersOnlyApprovedMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	methods := make(map[string]bool)
	for _, route := range engine.Routes() {
		if route.Path == "/api/zashboard/clash/*path" {
			methods[route.Method] = true
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if !methods[method] {
			t.Fatalf("expected %s Zashboard proxy route", method)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodOptions} {
		if methods[method] {
			t.Fatalf("unexpected %s Zashboard proxy route", method)
		}
	}
}
