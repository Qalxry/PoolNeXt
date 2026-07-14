package controller

import (
	"errors"
	"net/http"
	"poolx/internal/model"
	"poolx/internal/service"

	"github.com/gin-gonic/gin"
)

type mihomoDownloadRequest struct {
	InstallPath string `json:"install_path"`
}

type mihomoInspectRequest struct {
	InstallPath string `json:"install_path"`
}

// InspectMihomoBinary godoc
// @Summary Register an existing Mihomo binary as external immutable and verify its version
// @Tags Kernel
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/kernel/mihomo/inspect [post]
func InspectMihomoBinary(c *gin.Context) {
	var request mihomoInspectRequest
	if err := decodeJSONBody(c.Request.Body, &request); err != nil {
		respondBadRequest(c, "invalid request payload")
		return
	}

	result, err := service.InspectMihomoBinary(c.Request.Context(), request.InstallPath)
	if err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationSystem, model.AppLogLevelError, "mihomo binary inspect failed | reason="+err.Error())
		respondMihomoKernelError(c, err)
		return
	}

	_ = service.AppLog.Push(model.AppLogClassificationSystem, model.AppLogLevelInfo, "mihomo binary inspected | mode="+result.Mode+" | path="+result.InstallPath+" | version="+result.DetectedVersion)
	c.JSON(200, gin.H{
		"success": true,
		"message": "Mihomo 二进制检查通过。",
		"data":    result,
	})
}

// UploadMihomoBinary godoc
// @Summary Upload a managed Mihomo binary, install it to target path and verify version
// @Tags Kernel
// @Accept mpfd
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/kernel/mihomo/upload [post]
func UploadMihomoBinary(c *gin.Context) {
	fileHeader, err := c.FormFile("binary")
	if err != nil {
		respondFailure(c, "请先选择要上传的 Mihomo 二进制文件。")
		return
	}

	installPath := c.PostForm("install_path")
	file, err := fileHeader.Open()
	if err != nil {
		respondFailure(c, "读取上传文件失败。")
		return
	}
	defer func() {
		_ = file.Close()
	}()

	result, err := service.InstallUploadedMihomoBinary(c.Request.Context(), fileHeader.Filename, installPath, file)
	if err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationSystem, model.AppLogLevelError, "mihomo binary upload failed | reason="+err.Error())
		respondMihomoKernelError(c, err)
		return
	}

	_ = service.AppLog.Push(model.AppLogClassificationSystem, model.AppLogLevelInfo, "mihomo binary uploaded | mode="+result.Mode+" | path="+result.InstallPath+" | version="+result.DetectedVersion)
	c.JSON(200, gin.H{
		"success": true,
		"message": "Mihomo 二进制已上传并完成版本校验。",
		"data":    result,
	})
}

// DownloadMihomoBinary godoc
// @Summary Download the latest official Mihomo binary into managed storage and verify its version
// @Tags Kernel
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/kernel/mihomo/download [post]
func DownloadMihomoBinary(c *gin.Context) {
	var request mihomoDownloadRequest
	if err := decodeJSONBody(c.Request.Body, &request); err != nil {
		respondBadRequest(c, "invalid request payload")
		return
	}

	result, err := service.DownloadAndInstallMihomoBinary(c.Request.Context(), request.InstallPath)
	if err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationSystem, model.AppLogLevelError, "mihomo binary download failed | reason="+err.Error())
		respondMihomoKernelError(c, err)
		return
	}

	_ = service.AppLog.Push(model.AppLogClassificationSystem, model.AppLogLevelInfo, "mihomo binary downloaded | mode="+result.Mode+" | path="+result.InstallPath+" | version="+result.DetectedVersion)
	c.JSON(200, gin.H{
		"success": true,
		"message": "已从官方仓库下载 Mihomo 并完成版本校验。",
		"data":    result,
	})
}

func respondMihomoKernelError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrExternalImmutableKernel) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if errors.Is(err, service.ErrKernelOwnershipUnknown) ||
		errors.Is(err, service.ErrUnsafeKernelPath) ||
		errors.Is(err, service.ErrKernelPathChanged) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if errors.Is(err, service.ErrKernelProtectionUnavailable) {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	respondFailure(c, err.Error())
}
