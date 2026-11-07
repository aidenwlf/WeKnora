package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
)

// Errors the approval endpoints map onto status codes.
var (
	ErrSkillApprovalNotFound   = errors.New("approval request not found")
	ErrSkillApprovalNotPending = errors.New("approval request is no longer pending")
)

// approverID is the authenticated user recorded on a decision. Empty when the
// caller is an API key principal without a user identity.
func approverID(ctx context.Context) string {
	id, _ := types.UserIDFromContext(ctx)
	return id
}

// ArtifactArchiveReader loads the bytes of one chat artifact after applying the
// same session-ownership and sharing rules the download endpoint uses. The
// session handler implements it because it already owns that resolution.
type ArtifactArchiveReader interface {
	ReadArtifactArchive(
		ctx context.Context, sessionID, messageID string, index int,
	) (fileName string, archive []byte, err error)
}

// SkillCatalogInstaller is the skill surface an approval needs: record the
// archive as a workspace definition, then install that definition onto the
// sandbox configs the member picked.
type SkillCatalogInstaller interface {
	RegisterCatalogFromArchive(
		ctx context.Context, tenantID uint64, archive []byte,
	) (*types.TenantSkillCatalogEntity, error)
	InstallCatalogToConfigs(
		ctx context.Context, tenantID uint64, catalogID string, configIDs []string,
	) (*CatalogInstallResult, error)
}

// SkillApprovalService decides whether a chat artifact installs immediately or
// waits for a workspace admin.
type SkillApprovalService struct {
	approvals repository.TenantSkillApprovalRepository
	installer SkillCatalogInstaller
	reader    ArtifactArchiveReader
}

// NewSkillApprovalService wires the approval ledger to the install path.
func NewSkillApprovalService(
	approvals repository.TenantSkillApprovalRepository,
	installer SkillCatalogInstaller,
	reader ArtifactArchiveReader,
) *SkillApprovalService {
	return &SkillApprovalService{approvals: approvals, installer: installer, reader: reader}
}

// InstallFromArtifactInput identifies one chat artifact and its target
// sandboxes.
type InstallFromArtifactInput struct {
	SessionID     string   `json:"session_id"`
	MessageID     string   `json:"message_id"`
	ArtifactIndex int      `json:"artifact_index"`
	ConfigIDs     []string `json:"config_ids"`
}

// InstallFromArtifactResult tells the caller which of two things happened.
type InstallFromArtifactResult struct {
	Installed  bool   `json:"installed"`
	ApprovalID uint64 `json:"approval_id,omitempty"`
	CatalogID  string `json:"catalog_id,omitempty"`
	FileName   string `json:"file_name,omitempty"`
}

// InstallFromArtifact installs the artifact when the caller may write the
// catalog, and otherwise queues it for an admin.
func (s *SkillApprovalService) InstallFromArtifact(
	ctx context.Context, tenantID uint64, in InstallFromArtifactInput,
) (*InstallFromArtifactResult, error) {
	configIDs := normalizeConfigIDs(in.ConfigIDs)
	if len(configIDs) == 0 {
		return nil, fmt.Errorf("at least one sandbox config is required")
	}
	if strings.TrimSpace(in.SessionID) == "" || strings.TrimSpace(in.MessageID) == "" || in.ArtifactIndex < 0 {
		return nil, fmt.Errorf("session_id, message_id and artifact_index are required")
	}

	fileName, archive, err := s.reader.ReadArtifactArchive(ctx, in.SessionID, in.MessageID, in.ArtifactIndex)
	if err != nil {
		return nil, err
	}
	result := &InstallFromArtifactResult{FileName: fileName}

	if types.TenantRoleFromContext(ctx).HasPermission(types.TenantRoleAdmin) {
		catalogID, err := s.install(ctx, tenantID, archive, configIDs)
		if err != nil {
			return nil, err
		}
		result.Installed = true
		result.CatalogID = catalogID
		return result, nil
	}

	requester, _ := types.UserIDFromContext(ctx)
	encoded, err := json.Marshal(configIDs)
	if err != nil {
		return nil, err
	}
	req := &types.TenantSkillApprovalRequestEntity{
		TenantID:      tenantID,
		RequesterID:   requester,
		SessionID:     in.SessionID,
		MessageID:     in.MessageID,
		ArtifactIndex: in.ArtifactIndex,
		FileName:      fileName,
		ConfigIDs:     string(encoded),
		Status:        types.SkillApprovalStatusPending,
	}
	if err := s.approvals.CreateRequest(ctx, req); err != nil {
		return nil, err
	}
	result.ApprovalID = req.ID
	return result, nil
}

// ListPending returns the workspace's open approval requests.
func (s *SkillApprovalService) ListPending(
	ctx context.Context, tenantID uint64,
) ([]*types.TenantSkillApprovalRequestEntity, error) {
	return s.approvals.ListPending(ctx, tenantID)
}

// Approve installs a queued request and records the decision.
func (s *SkillApprovalService) Approve(
	ctx context.Context, tenantID uint64, requestID uint64, reason string,
) (*InstallFromArtifactResult, error) {
	req, err := s.approvals.GetRequest(ctx, tenantID, requestID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrSkillApprovalNotFound
	}
	if !req.IsPending() {
		return nil, ErrSkillApprovalNotPending
	}

	var configIDs []string
	if err := json.Unmarshal([]byte(req.ConfigIDs), &configIDs); err != nil {
		return nil, fmt.Errorf("invalid request targets: %w", err)
	}
	fileName, archive, err := s.reader.ReadArtifactArchive(ctx, req.SessionID, req.MessageID, req.ArtifactIndex)
	if err != nil {
		return nil, err
	}
	catalogID, err := s.install(ctx, tenantID, archive, normalizeConfigIDs(configIDs))
	if err != nil {
		return nil, err
	}
	if _, err := s.approvals.Decide(
		ctx, tenantID, requestID, types.SkillApprovalStatusApproved, approverID(ctx), reason,
	); err != nil {
		return nil, err
	}
	return &InstallFromArtifactResult{Installed: true, CatalogID: catalogID, FileName: fileName}, nil
}

// Reject closes a queued request without installing.
func (s *SkillApprovalService) Reject(
	ctx context.Context, tenantID uint64, requestID uint64, reason string,
) error {
	rejected, err := s.approvals.Decide(
		ctx, tenantID, requestID, types.SkillApprovalStatusRejected, approverID(ctx), reason,
	)
	if err != nil {
		return err
	}
	if !rejected {
		return ErrSkillApprovalNotPending
	}
	return nil
}

func (s *SkillApprovalService) install(
	ctx context.Context, tenantID uint64, archive []byte, configIDs []string,
) (string, error) {
	if len(archive) == 0 {
		return "", fmt.Errorf("skill bundle is empty")
	}
	catalog, err := s.installer.RegisterCatalogFromArchive(ctx, tenantID, archive)
	if err != nil {
		return "", err
	}
	if _, err := s.installer.InstallCatalogToConfigs(ctx, tenantID, catalog.ID, configIDs); err != nil {
		return "", err
	}
	return catalog.ID, nil
}

func normalizeConfigIDs(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
