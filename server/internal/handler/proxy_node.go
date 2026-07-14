package controller

import (
	"poolx/internal/model"
	"poolx/internal/service"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type proxyNodeStatusRequest struct {
	Enabled bool `json:"enabled"`
}

// GetProxyNodes godoc
// @Summary List proxy nodes with paging and filters
// @Tags ProxyNode
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/proxy-nodes [get]
func GetProxyNodes(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("p"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	sourceConfigID, _ := strconv.Atoi(c.Query("source_config_id"))

	var enabled *bool
	if value := strings.TrimSpace(c.Query("enabled")); value != "" {
		parsed := value == "true" || value == "1"
		enabled = &parsed
	}

	var quarantined *bool
	if value := strings.TrimSpace(c.Query("quarantined")); value != "" {
		parsed := value == "true" || value == "1"
		quarantined = &parsed
	}

	nodes, err := service.ListProxyNodes(service.ProxyNodeListInput{
		Page:           page,
		Offset:         offset,
		Limit:          limit,
		Keyword:        c.Query("keyword"),
		SourceConfigID: sourceConfigID,
		Enabled:        enabled,
		TestStatus:     c.Query("test_status"),
		Quarantined:    quarantined,
	})
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, nodes)
}

func GetProxyNodeHealth(c *gin.Context) {
	summary, err := service.GetNodeHealthSummary()
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, summary)
}

func RunProxyNodeHealthCheck(c *gin.Context) {
	result, err := service.RunNodeHealthCheck(c.Request.Context())
	if err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelWarn, "manual proxy node health check failed | username="+c.GetString("username")+" | reason="+err.Error())
		respondFailure(c, err.Error())
		return
	}
	_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelInfo, "manual proxy node health check completed | username="+c.GetString("username")+" | checked="+strconv.Itoa(result.Checked)+" | failed="+strconv.Itoa(result.Failed))
	respondSuccess(c, result)
}

// UpdateProxyNodeStatus godoc
// @Summary Enable or disable a proxy node
// @Tags ProxyNode
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/proxy-nodes/{id}/status [post]
func UpdateProxyNodeStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		respondBadRequest(c, "无效的参数")
		return
	}

	var request proxyNodeStatusRequest
	if err := decodeJSONBody(c.Request.Body, &request); err != nil {
		respondBadRequest(c, "无效的参数")
		return
	}

	if err := service.SetProxyNodeEnabled(id, request.Enabled); err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelWarn, "proxy node status update failed | username="+c.GetString("username")+" | node_id="+strconv.Itoa(id)+" | reason="+err.Error())
		respondFailure(c, err.Error())
		return
	}

	respondSuccessMessage(c, "")
}

// DeleteProxyNode godoc
// @Summary Delete a proxy node
// @Tags ProxyNode
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/proxy-nodes/{id}/delete [post]
func DeleteProxyNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		respondBadRequest(c, "无效的参数")
		return
	}

	if err := service.DeleteProxyNode(id); err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelWarn, "proxy node delete failed | username="+c.GetString("username")+" | node_id="+strconv.Itoa(id)+" | reason="+err.Error())
		respondFailure(c, err.Error())
		return
	}

	_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelInfo, "proxy node deleted | username="+c.GetString("username")+" | node_id="+strconv.Itoa(id))
	respondSuccessMessage(c, "")
}

// DeleteProxyNodes godoc
// @Summary Delete selected proxy nodes
// @Tags ProxyNode
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/proxy-nodes/delete [post]
func DeleteProxyNodes(c *gin.Context) {
	var request service.ProxyNodeDeleteInput
	if err := decodeJSONBody(c.Request.Body, &request); err != nil {
		respondBadRequest(c, "无效的参数")
		return
	}

	deleted, err := service.DeleteProxyNodes(request.NodeIDs)
	if err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelWarn, "proxy node batch delete failed | username="+c.GetString("username")+" | reason="+err.Error())
		respondFailure(c, err.Error())
		return
	}

	_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelInfo, "proxy nodes deleted | username="+c.GetString("username")+" | count="+strconv.Itoa(deleted))
	respondSuccess(c, gin.H{"deleted": deleted})
}

// TestProxyNodes godoc
// @Summary Test selected proxy nodes and persist the result
// @Tags ProxyNode
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/proxy-nodes/test [post]
func TestProxyNodes(c *gin.Context) {
	var request service.NodeTestInput
	if err := decodeJSONBody(c.Request.Body, &request); err != nil {
		respondBadRequest(c, "无效的参数")
		return
	}

	results, err := service.ExecuteNodeTests(c.Request.Context(), request)
	if err != nil {
		_ = service.AppLog.Push(model.AppLogClassificationBusiness, model.AppLogLevelWarn, "proxy node test failed | username="+c.GetString("username")+" | reason="+err.Error())
		respondFailure(c, err.Error())
		return
	}

	respondSuccess(c, results)
}

// UpdateProxyNodeTags godoc
// @Summary Batch update selected proxy node tags
// @Tags ProxyNode
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/proxy-nodes/tags [post]
func UpdateProxyNodeTags(c *gin.Context) {
	var request service.ProxyNodeTagsInput
	if err := decodeJSONBody(c.Request.Body, &request); err != nil {
		respondBadRequest(c, "无效的参数")
		return
	}
	updated, err := service.UpdateProxyNodeTags(request.NodeIDs, request.Tags)
	if err != nil {
		respondFailure(c, err.Error())
		return
	}
	respondSuccess(c, gin.H{"updated": updated})
}
