package types

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

// Univer unit kinds. One knowledge base of type "univer" holds entries of any
// of these kinds, the way a document knowledge base holds files.
const (
	UniverKindSheet = "sheet" // 表格
	UniverKindDoc   = "doc"   // 文档
	UniverKindBase  = "base"  // 多维表格
	UniverKindSlide = "slide" // 幻灯片
)

// UniverKinds lists the supported unit kinds in display order.
var UniverKinds = []string{UniverKindSheet, UniverKindDoc, UniverKindBase, UniverKindSlide}

// NormalizeUniverKind validates a kind name.
func NormalizeUniverKind(raw string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(raw))
	for _, kind := range UniverKinds {
		if v == kind {
			return v, true
		}
	}
	return "", false
}

// UniverUnit is one live, collaboratively edited entry inside a knowledge base
// of type "univer".
//
// The content itself lives in the Univer collaboration store, keyed by UnitID.
// This row records which knowledge base owns it and which product it is.
type UniverUnit struct {
	ID              string         `gorm:"column:id;primaryKey;size:36"                json:"id"`
	TenantID        uint64         `gorm:"column:tenant_id;index"                      json:"tenant_id"`
	KnowledgeBaseID string         `gorm:"column:knowledge_base_id;size:36;index"      json:"knowledge_base_id"`
	Name            string         `gorm:"column:name;size:200"                        json:"name"`
	Kind            string         `gorm:"column:kind;size:16"                         json:"kind"`
	UnitID          string         `gorm:"column:unit_id;size:128;uniqueIndex"         json:"unit_id"`
	CreatorID       string         `gorm:"column:creator_id;size:128"                  json:"creator_id"`
	CreatedAt       time.Time      `gorm:"column:created_at"                           json:"created_at"`
	UpdatedAt       time.Time      `gorm:"column:updated_at"                           json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"column:deleted_at;index"                     json:"-"`
}

// TableName returns the persistence table for UniverUnit.
func (UniverUnit) TableName() string { return "univer_units" }
