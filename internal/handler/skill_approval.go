package handler

import (
	"context"
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
)

// skillApprovalService is the surface the approval endpoints need.
type skillApprovalService interface {
	InstallFromArtifact(
		ctx context.Context, tenantID uint64, in service.InstallFromArtifactInput,
	) (*service.InstallFromArtifactResult, error)
	ListPending(
		ctx context.Context, tenantID uint64,
	) ([]*types.TenantSkillApprovalRequestEntity, error)
	Approve(
		ctx context.Context, tenantID uint64, requestID uint64, reason string,
	) (*service.InstallFromArtifactResult, error)
	Reject(ctx context.Context, tenantID uint64, requestID uint64, reason string) error
}

// SkillApprovalHandler serves "add this chat artifact as a skill". A member
// without catalog write permission gets a queued request an admin decides.
type SkillApprovalHandler struct {
	service skillApprovalService
}

// NewSkillApprovalHandler creates the handler. The concrete service is the
// constructor argument because the container resolves concrete types; the
// narrow interface above is what the handler actually uses.
func NewSkillApprovalHandler(service *service.SkillApprovalService) *SkillApprovalHandler {
	return &SkillApprovalHandler{service: service}
}

// InstallFromArtifact queues or performs the install of one chat artifact.
//
// @Summary      将对话产物安装为技能
// @Description  有技能目录写权限时直接安装；否则提交给管理员审批。
// @Tags         Skills
// @Accept       json
// @Produce      json
// @Param        request  body  service.InstallFromArtifactInput  true  "artifact location and target sandboxes"
// @Success      202  {object}  map[string]interface{}
// @Failure      400  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /skills/install-from-artifact [post]
func (h *SkillApprovalHandler) InstallFromArtifact(c *gin.Context) {
	if h.service == nil {
		_ = c.Error(apperrors.NewInternalServerError("skill approvals are not configured"))
		return
	}
	var in service.InstallFromArtifactInput
	if err := c.ShouldBindJSON(&in); err != nil {
		_ = c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}
	tenantID := sandboxConfigTenantID(c)
	if tenantID == 0 {
		_ = c.Error(apperrors.NewUnauthorizedError("workspace not resolved"))
		return
	}
	result, err := h.service.InstallFromArtifact(c.Request.Context(), tenantID, in)
	if err != nil {
		writeSkillApprovalError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": result})
}

// ListApprovalRequests returns the workspace's pending skill approvals.
//
// @Summary      List pending skill approvals
// @Tags         Skills
// @Produce      json
// @Success      200  {object}  handler.DataResponse{data=[]types.TenantSkillApprovalRequestEntity}
// @Security     Bearer
// @Router       /skills/approval-requests [get]
func (h *SkillApprovalHandler) ListApprovalRequests(c *gin.Context) {
	if h.service == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []any{}})
		return
	}
	rows, err := h.service.ListPending(c.Request.Context(), sandboxConfigTenantID(c))
	if err != nil {
		writeSkillApprovalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

type skillApprovalDecisionRequest struct {
	Reason string `json:"reason"`
}

// ApproveApprovalRequest installs the approved artifact and closes the request.
//
// @Summary      Approve a skill approval request
// @Tags         Skills
// @Accept       json
// @Produce      json
// @Param        id       path  int  true  "approval request id"
// @Param        request  body  skillApprovalDecisionRequest  false  "optional note"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /skills/approval-requests/{id}/approve [post]
func (h *SkillApprovalHandler) ApproveApprovalRequest(c *gin.Context) {
	if h.service == nil {
		_ = c.Error(apperrors.NewInternalServerError("skill approvals are not configured"))
		return
	}
	id, ok := approvalRequestID(c)
	if !ok {
		return
	}
	var req skillApprovalDecisionRequest
	_ = c.ShouldBindJSON(&req)
	result, err := h.service.Approve(c.Request.Context(), sandboxConfigTenantID(c), id, req.Reason)
	if err != nil {
		writeSkillApprovalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// RejectApprovalRequest closes a request without installing.
//
// @Summary      Reject a skill approval request
// @Tags         Skills
// @Accept       json
// @Produce      json
// @Param        id       path  int  true  "approval request id"
// @Param        request  body  skillApprovalDecisionRequest  true  "reason"
// @Success      200  {object}  map[string]interface{}
// @Security     Bearer
// @Router       /skills/approval-requests/{id}/reject [post]
func (h *SkillApprovalHandler) RejectApprovalRequest(c *gin.Context) {
	if h.service == nil {
		_ = c.Error(apperrors.NewInternalServerError("skill approvals are not configured"))
		return
	}
	id, ok := approvalRequestID(c)
	if !ok {
		return
	}
	var req skillApprovalDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		_ = c.Error(apperrors.NewBadRequestError("reason is required"))
		return
	}
	if err := h.service.Reject(c.Request.Context(), sandboxConfigTenantID(c), id, req.Reason); err != nil {
		writeSkillApprovalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func approvalRequestID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		_ = c.Error(apperrors.NewBadRequestError("invalid approval request id"))
		return 0, false
	}
	return id, true
}

func writeSkillApprovalError(c *gin.Context, err error) {
	switch {
	case stderrors.Is(err, service.ErrSkillApprovalNotFound):
		_ = c.Error(apperrors.NewNotFoundError(err.Error()))
	case stderrors.Is(err, service.ErrSkillApprovalNotPending):
		_ = c.Error(apperrors.NewConflictError(err.Error()))
	default:
		_ = c.Error(apperrors.NewBadRequestError(err.Error()))
	}
}
