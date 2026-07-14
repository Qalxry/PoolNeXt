package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIdentityBindingRoutesArePostOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	wanted := map[string]bool{
		"/api/oauth/email/bind":  false,
		"/api/oauth/wechat/bind": false,
	}
	for _, route := range engine.Routes() {
		if _, tracked := wanted[route.Path]; !tracked {
			continue
		}
		if route.Method == http.MethodGet {
			t.Fatalf("state-changing identity route still accepts GET: %s", route.Path)
		}
		if route.Method == http.MethodPost {
			wanted[route.Path] = true
		}
	}
	for path, found := range wanted {
		if !found {
			t.Fatalf("POST identity binding route is missing: %s", path)
		}
	}
}
