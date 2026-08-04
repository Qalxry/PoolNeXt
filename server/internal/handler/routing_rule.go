package controller

import (
	"poolx/internal/service"

	"github.com/gin-gonic/gin"
)

// GetRoutingRules godoc
// @Summary List ordered Mihomo routing rules
// @Tags Workspace
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/routing-rules [get]
func GetRoutingRules(c *gin.Context) {
	items, err := service.GetRoutingRules()
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, items)
}

// ReplaceRoutingRules godoc
// @Summary Replace ordered Mihomo routing rules
// @Tags Workspace
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/routing-rules [post]
func ReplaceRoutingRules(c *gin.Context) {
	var payload struct {
		Items []service.RoutingRulePayload `json:"items"`
	}
	if err := decodeJSONBody(c.Request.Body, &payload); err != nil {
		respondBadRequest(c, "无效的参数")
		return
	}
	items, err := service.ReplaceRoutingRules(payload.Items)
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, items)
}
