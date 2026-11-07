package types

import "time"

// Skill approval request states.
const (
	SkillApprovalStatusPending  = "pending"
	SkillApprovalStatusApproved = "approved"
	SkillApprovalStatusRejected = "rejected"
)

// TenantSkillApprovalRequestEntity is one member's request to turn a chat
// artifact into a workspace skill. A workspace admin decides it; approving
// runs the same catalog-register + install path a direct install uses.
type TenantSkillApprovalRequestEntity struct {
	ID uint64 `gorm:"primaryKey;autoIncrement"`
	// TenantID is the workspace the artifact and the target sandboxes belong to.
	TenantID    uint64 `gorm:"index:idx_skill_approval_tenant_status"`
	RequesterID string `gorm:"type:varchar(64);not null"`

	// The artifact is addressed exactly like the download endpoint: session,
	// message and its position in the message's Artifacts slice.
	SessionID     string `gorm:"type:varchar(36);not null"`
	MessageID     string `gorm:"type:varchar(36);not null"`
	ArtifactIndex int    `gorm:"not null"`
	FileName      string `gorm:"type:varchar(255);not null;default:''"`

	// ConfigIDs is the JSON array of sandbox configs to install onto, captured
	// when the request is made so approval installs what the member saw.
	ConfigIDs string `gorm:"type:text;not null;default:'[]'"`

	Status     string `gorm:"type:varchar(32);not null;default:'pending';index:idx_skill_approval_tenant_status"`
	ApproverID string `gorm:"type:varchar(64);not null;default:''"`
	Reason     string `gorm:"type:text;not null;default:''"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName pins the table so GORM's pluralizer cannot drift.
func (e *TenantSkillApprovalRequestEntity) TableName() string {
	return "tenant_skill_approval_requests"
}

// IsPending reports whether the request still awaits a decision.
func (e *TenantSkillApprovalRequestEntity) IsPending() bool {
	return e.Status == SkillApprovalStatusPending
}
