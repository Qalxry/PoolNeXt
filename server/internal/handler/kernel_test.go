package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"poolx/internal/service"

	"github.com/gin-gonic/gin"
)

func TestRespondMihomoKernelErrorReturnsForbiddenForImmutableMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondMihomoKernelError(context, fmt.Errorf("%w: operation=overwrite", service.ErrExternalImmutableKernel))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
}

func TestRespondMihomoKernelErrorReturnsConflictForUnknownOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondMihomoKernelError(context, fmt.Errorf("%w: source=legacy", service.ErrKernelOwnershipUnknown))

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", recorder.Code)
	}
}

func TestRespondMihomoKernelErrorReturnsUnavailableForProtectionStoreFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	respondMihomoKernelError(context, fmt.Errorf("%w: database offline", service.ErrKernelProtectionUnavailable))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", recorder.Code)
	}
}
