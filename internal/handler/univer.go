package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// UniverHandler exposes collaborative entries inside a knowledge base.
type UniverHandler struct {
	service interfaces.UniverService
}

// NewUniverHandler builds the handler.
func NewUniverHandler(service interfaces.UniverService) *UniverHandler {
	return &UniverHandler{service: service}
}

func (h *UniverHandler) available(c *gin.Context) bool {
	if h == nil || h.service == nil || !h.service.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "collaborative knowledge bases are not enabled on this deployment",
		})
		return false
	}
	return true
}

// Status reports whether the integration is available.
//
// @Summary      协同知识库可用状态
// @Tags         univer
// @Produce      json
// @Success      200 {object} handler.DataResponse
// @Router       /univer/status [get]
func (h *UniverHandler) Status(c *gin.Context) {
	enabled := h != nil && h.service != nil && h.service.Enabled()
	data := gin.H{"enabled": enabled}
	if enabled {
		data["base_path"] = h.service.PublicPath()
		data["kinds"] = types.UniverKinds
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// ListUnits lists the entries of one collaborative knowledge base.
//
// @Summary      协同条目列表
// @Tags         univer
// @Produce      json
// @Success      200 {object} handler.DataResponse
// @Router       /knowledge-bases/{kb_id}/univer/units [get]
func (h *UniverHandler) ListUnits(c *gin.Context) {
	if !h.available(c) {
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	units, err := h.service.ListUnits(c.Request.Context(), tenantID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": units})
}

// CreateUnit creates an entry of the requested kind.
//
// @Summary      创建协同条目
// @Tags         univer
// @Accept       json
// @Produce      json
// @Success      200 {object} handler.DataResponse
// @Router       /knowledge-bases/{kb_id}/univer/units [post]
func (h *UniverHandler) CreateUnit(c *gin.Context) {
	if !h.available(c) {
		return
	}
	var body struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid body"})
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	userID := c.GetString(types.UserIDContextKey.String())
	unit, err := h.service.CreateUnit(
		c.Request.Context(), tenantID, c.Param("id"), userID, body.Name, body.Kind,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": unit})
}

// GetUnit returns one entry.
//
// @Summary      协同条目详情
// @Tags         univer
// @Produce      json
// @Success      200 {object} handler.DataResponse
// @Router       /knowledge-bases/{kb_id}/univer/units/{id} [get]
func (h *UniverHandler) GetUnit(c *gin.Context) {
	if !h.available(c) {
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	unit, err := h.service.GetUnit(c.Request.Context(), tenantID, c.Param("id"), c.Param("unit_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	if unit == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "unit not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": unit})
}

// DeleteUnit removes an entry from the knowledge base listing.
//
// @Summary      删除协同条目
// @Tags         univer
// @Produce      json
// @Success      200 {object} handler.MessageResponse
// @Router       /knowledge-bases/{kb_id}/univer/units/{id} [delete]
func (h *UniverHandler) DeleteUnit(c *gin.Context) {
	if !h.available(c) {
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	unit, err := h.service.GetUnit(c.Request.Context(), tenantID, c.Param("id"), c.Param("unit_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	if unit == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "unit not found"})
		return
	}
	if err := h.service.DeleteUnit(
		c.Request.Context(), tenantID, c.Param("id"), c.Param("unit_id"),
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "deleted"})
}

// UnitSession mints the identity cookie the browser needs to open one entry.
//
// @Summary      协同条目会话
// @Tags         univer
// @Produce      json
// @Success      200 {object} handler.DataResponse
// @Router       /knowledge-bases/{kb_id}/univer/units/{id}/session [post]
func (h *UniverHandler) UnitSession(c *gin.Context) {
	if !h.available(c) {
		return
	}
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	userID := c.GetString(types.UserIDContextKey.String())
	unit, err := h.service.GetUnit(c.Request.Context(), tenantID, c.Param("id"), c.Param("unit_id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	if unit == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "unit not found"})
		return
	}
	name, value, maxAge, err := h.service.SessionCookie(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	// Path scoping keeps the cookie off ordinary API calls.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, value, maxAge, h.service.PublicPath(), "", secure, true)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"id":                unit.ID,
			"name":              unit.Name,
			"kind":              unit.Kind,
			"unit_id":           unit.UnitID,
			"base_path":         h.service.PublicPath(),
			"knowledge_base_id": unit.KnowledgeBaseID,
		},
	})
}

// InternalACL answers the collaboration server's access question. It is
// reachable only with the shared secret and deliberately lives outside the
// tenant-scoped API tree.
func (h *UniverHandler) InternalACL(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	var body struct {
		UserID string `json:"user_id"`
		UnitID string `json:"unit_id"`
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if strings.TrimSpace(body.UserID) == "" || strings.TrimSpace(body.UnitID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id and unit_id are required"})
		return
	}
	allowed, err := h.service.CheckUnitAccess(c.Request.Context(), body.UserID, body.UnitID, body.Action)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"allowed": allowed})
}
