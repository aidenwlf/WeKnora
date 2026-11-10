package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// UniverService owns the collaborative knowledge base surface: entries stored
// in WeKnora plus the calls that reach the Univer collaboration server.
//
// Entries live inside a knowledge base of type "univer"; access follows the
// knowledge base's own permissions.
type UniverService interface {
	// Enabled reports whether the collaboration server is configured.
	Enabled() bool
	// PublicPath is the origin-relative prefix the browser opens the
	// collaboration client against (proxied to the collaboration server).
	PublicPath() string
	// SessionCookie mints the short-lived identity cookie a browser needs for
	// the collaboration client and returns its name, value and max age.
	SessionCookie(userID string) (name, value string, maxAge int, err error)
	// CookieName is the name of the collaboration identity cookie.
	CookieName() string
	// SessionUser verifies a cookie value and returns the identity it carries.
	SessionUser(value string) (userID string, ok bool)
	// ForwardHeaders are the headers the proxy adds when it forwards a
	// browser request to the collaboration server.
	ForwardHeaders(userID string) map[string]string

	CreateUnit(ctx context.Context, tenantID uint64, kbID, userID, name, kind string) (*types.UniverUnit, error)
	ListUnits(ctx context.Context, tenantID uint64, kbID string) ([]*types.UniverUnit, error)
	GetUnit(ctx context.Context, tenantID uint64, kbID, id string) (*types.UniverUnit, error)
	DeleteUnit(ctx context.Context, tenantID uint64, kbID, id string) error
	// ResolveUnit accepts a unit id, a collaboration unit id, or a name, inside
	// any of the given knowledge bases.
	ResolveUnit(ctx context.Context, tenantID uint64, kbIDs []string, ref string) (*types.UniverUnit, error)

	// ReadContent returns a textual rendering of the unit, shaped by its kind.
	ReadContent(ctx context.Context, userID string, unit *types.UniverUnit, cellRange string) (string, error)
	// WriteContent applies an edit. Spreadsheets take a cell matrix; documents
	// take appended text. Other kinds reject the call rather than pretend.
	WriteContent(ctx context.Context, userID string, unit *types.UniverUnit, cellRange string, values [][]any) error
	// Describe returns the workbook/table/page structure behind one unit.
	Describe(ctx context.Context, userID string, unit *types.UniverUnit) (map[string]any, error)

	// CheckUnitAccess answers the collaboration server's ACL question for one
	// unit and action ("read" or "write").
	CheckUnitAccess(ctx context.Context, userID, unitID, action string) (bool, error)
}
