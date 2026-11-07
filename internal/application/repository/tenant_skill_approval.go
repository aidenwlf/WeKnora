package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
)

// TenantSkillApprovalRepository persists members' requests to install a chat
// artifact as a workspace skill.
type TenantSkillApprovalRepository interface {
	CreateRequest(ctx context.Context, e *types.TenantSkillApprovalRequestEntity) error
	// GetRequest returns nil (no error) when the row is absent or belongs to
	// another workspace, so callers can answer 404 directly.
	GetRequest(ctx context.Context, tenantID uint64, id uint64) (*types.TenantSkillApprovalRequestEntity, error)
	// ListPending returns the workspace's open requests, oldest first so the
	// queue is served in the order members asked.
	ListPending(ctx context.Context, tenantID uint64) ([]*types.TenantSkillApprovalRequestEntity, error)
	// Decide records the decision. It only applies to a row that is still
	// pending, so two admins deciding at once cannot both act on it.
	Decide(
		ctx context.Context, tenantID uint64, id uint64, status, approverID, reason string,
	) (bool, error)
}

type tenantSkillApprovalRepository struct{ db *gorm.DB }

// NewTenantSkillApprovalRepository returns a GORM-backed implementation.
func NewTenantSkillApprovalRepository(db *gorm.DB) TenantSkillApprovalRepository {
	return &tenantSkillApprovalRepository{db: db}
}

func (r *tenantSkillApprovalRepository) CreateRequest(
	ctx context.Context, e *types.TenantSkillApprovalRequestEntity,
) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *tenantSkillApprovalRepository) GetRequest(
	ctx context.Context, tenantID uint64, id uint64,
) (*types.TenantSkillApprovalRequestEntity, error) {
	var e types.TenantSkillApprovalRequestEntity
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *tenantSkillApprovalRepository) ListPending(
	ctx context.Context, tenantID uint64,
) ([]*types.TenantSkillApprovalRequestEntity, error) {
	var list []*types.TenantSkillApprovalRequestEntity
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", tenantID, types.SkillApprovalStatusPending).
		Order("created_at ASC").
		Find(&list).Error
	return list, err
}

func (r *tenantSkillApprovalRepository) Decide(
	ctx context.Context, tenantID uint64, id uint64, status, approverID, reason string,
) (bool, error) {
	res := r.db.WithContext(ctx).
		Model(&types.TenantSkillApprovalRequestEntity{}).
		Where("tenant_id = ? AND id = ? AND status = ?", tenantID, id, types.SkillApprovalStatusPending).
		Updates(map[string]interface{}{
			"status":      status,
			"approver_id": approverID,
			"reason":      reason,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
