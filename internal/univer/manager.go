package univer

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	// DefaultInternalURL is where the collaboration server listens locally.
	DefaultInternalURL = "http://127.0.0.1:3010"
	// DefaultPublicPath is the origin-relative prefix the browser uses. The Go
	// proxy forwards it to the collaboration server, so the browser keeps the
	// WeKnora origin and its path-scoped identity cookie.
	DefaultPublicPath = "/api/v1/univer/universer-api"
	// DefaultCookieName carries the browser identity for the collaboration
	// client. It is scoped to PublicPath.
	DefaultCookieName = "weknora_univer"
	// sessionTTL bounds how long a browser may keep editing before refetching
	// its identity cookie.
	sessionTTL = 8 * time.Hour
)

// Manager implements interfaces.UniverService.
type Manager struct {
	db       *gorm.DB
	members  interfaces.TenantMemberService
	client   *Client
	server   *Supervisor
	secret   string
	public   string
	cookie   string
	internal string
	enabled  bool
}

// Options carries the deployment wiring for the collaboration integration.
type Options struct {
	Enabled     bool
	Internal    string
	PublicPath  string
	CookieName  string
	Secret      string
	Managed     bool
	NodeEntry   string
	DatabaseURL string
}

// OptionsFromEnv reads the collaboration settings.
func OptionsFromEnv() Options {
	return Options{
		Enabled:     truthy(os.Getenv("UNIVER_ENABLED")),
		Internal:    envOr("UNIVER_INTERNAL_URL", DefaultInternalURL),
		PublicPath:  envOr("UNIVER_PUBLIC_PATH", DefaultPublicPath),
		CookieName:  envOr("UNIVER_COOKIE_NAME", DefaultCookieName),
		Secret:      os.Getenv("UNIVER_SHARED_SECRET"),
		Managed:     truthy(os.Getenv("UNIVER_MANAGED")),
		NodeEntry:   envOr("UNIVER_NODE_ENTRY", "server/univer-collab/main.js"),
		DatabaseURL: os.Getenv("UNIVER_DB_URL"),
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	}
	return false
}

// NewManager builds the collaboration integration. It never fails on a missing
// server: an unconfigured integration reports Enabled()==false and the rest of
// WeKnora keeps working.
func NewManager(db *gorm.DB, members interfaces.TenantMemberService, opts Options) *Manager {
	m := &Manager{
		db:       db,
		members:  members,
		client:   NewClient(opts.Internal, opts.Secret),
		secret:   opts.Secret,
		public:   strings.TrimRight(opts.PublicPath, "/"),
		cookie:   opts.CookieName,
		internal: opts.Internal,
		enabled:  opts.Enabled,
	}
	if opts.Managed {
		m.server = NewSupervisor(SupervisorOptions{
			Entry:       opts.NodeEntry,
			Port:        portFromURL(opts.Internal),
			Secret:      opts.Secret,
			DatabaseURL: opts.DatabaseURL,
		})
	}
	return m
}

// portFromURL extracts the listening port from the internal URL.
func portFromURL(raw string) int {
	idx := strings.LastIndex(raw, ":")
	if idx < 0 || idx == len(raw)-1 {
		return 3010
	}
	var port int
	if _, err := fmt.Sscanf(raw[idx+1:], "%d", &port); err != nil || port <= 0 {
		return 3010
	}
	return port
}

// Enabled reports whether the integration is configured.
func (m *Manager) Enabled() bool { return m != nil && m.enabled }

// PublicPath is the browser-facing prefix.
func (m *Manager) PublicPath() string {
	if m == nil {
		return DefaultPublicPath
	}
	return m.public
}

// Start launches the managed collaboration server, when one is configured.
func (m *Manager) Start(ctx context.Context) error {
	if m == nil || m.server == nil {
		return nil
	}
	return m.server.Start(ctx)
}

// Close stops the managed collaboration server.
func (m *Manager) Close() {
	if m != nil && m.server != nil {
		m.server.Stop()
	}
}

// ---------------------------------------------------------------------------
// Identity cookie
// ---------------------------------------------------------------------------

type sessionPayload struct {
	UID string `json:"uid"`
	Exp int64  `json:"exp"`
}

func (m *Manager) sign(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(m.secret))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SessionCookie mints the collaboration identity cookie. The collaboration
// server verifies it with the same shared secret.
func (m *Manager) SessionCookie(userID string) (string, string, int, error) {
	if m == nil || !m.enabled {
		return "", "", 0, errors.New("univer integration is disabled")
	}
	if m.secret == "" {
		return "", "", 0, errors.New("UNIVER_SHARED_SECRET is not configured")
	}
	if strings.TrimSpace(userID) == "" {
		return "", "", 0, errors.New("user is required")
	}
	exp := time.Now().Add(sessionTTL).Unix()
	raw, err := json.Marshal(sessionPayload{UID: userID, Exp: exp})
	if err != nil {
		return "", "", 0, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return m.cookie, encoded + "." + m.sign(raw), int(sessionTTL.Seconds()), nil
}

// CookieName is the collaboration identity cookie.
func (m *Manager) CookieName() string {
	if m == nil {
		return DefaultCookieName
	}
	return m.cookie
}

// SessionUser verifies a cookie value minted by SessionCookie.
func (m *Manager) SessionUser(value string) (string, bool) {
	if m == nil || m.secret == "" || value == "" {
		return "", false
	}
	body, mac, found := strings.Cut(value, ".")
	if !found {
		return "", false
	}
	raw := mustDecode(body)
	expected := m.sign(raw)
	if subtle.ConstantTimeCompare([]byte(mac), []byte(expected)) != 1 {
		return "", false
	}
	var payload sessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false
	}
	if payload.Exp == 0 || time.Now().Unix() > payload.Exp || payload.UID == "" {
		return "", false
	}
	return payload.UID, true
}

func mustDecode(encoded string) []byte {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	return raw
}

// ForwardHeaders are the headers the Go proxy adds for the collaboration
// server: the caller identity plus the shared secret that vouches for it.
func (m *Manager) ForwardHeaders(userID string) map[string]string {
	headers := map[string]string{"X-WeKnora-User": userID}
	if m != nil && m.secret != "" {
		headers["X-WeKnora-Secret"] = m.secret
	}
	return headers
}

// ---------------------------------------------------------------------------
// Units inside a knowledge base
// ---------------------------------------------------------------------------

func (m *Manager) requireEnabled() error {
	if m == nil || !m.enabled {
		return errors.New("univer integration is disabled")
	}
	return nil
}

// CreateUnit creates the collaboration unit and records its knowledge base.
func (m *Manager) CreateUnit(
	ctx context.Context, tenantID uint64, kbID, userID, name, kind string,
) (*types.UniverUnit, error) {
	if err := m.requireEnabled(); err != nil {
		return nil, err
	}
	normalizedKind, ok := types.NormalizeUniverKind(kind)
	if !ok {
		return nil, fmt.Errorf("unsupported unit kind %q", kind)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("unit name is required")
	}
	if len([]rune(name)) > 200 {
		return nil, errors.New("unit name is limited to 200 characters")
	}
	id := uuid.NewString()
	unitID := normalizedKind + "-" + strings.ReplaceAll(id, "-", "")
	if err := m.client.CreateUnit(ctx, userID, CreateUnitRequest{
		UnitID:   unitID,
		Kind:     normalizedKind,
		Name:     name,
		Sheet:    name,
		RowCount: 500,
		ColCount: 40,
	}); err != nil {
		return nil, err
	}
	unit := &types.UniverUnit{
		ID:              id,
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		Name:            name,
		Kind:            normalizedKind,
		UnitID:          unitID,
		CreatorID:       userID,
	}
	if err := m.db.WithContext(ctx).Create(unit).Error; err != nil {
		return nil, err
	}
	return unit, nil
}

// ListUnits returns the knowledge base's units, newest first.
func (m *Manager) ListUnits(ctx context.Context, tenantID uint64, kbID string) ([]*types.UniverUnit, error) {
	if err := m.requireEnabled(); err != nil {
		return nil, err
	}
	var units []*types.UniverUnit
	err := m.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
		Order("created_at DESC").
		Find(&units).Error
	return units, err
}

// GetUnit looks a unit up by its public id inside one knowledge base.
func (m *Manager) GetUnit(ctx context.Context, tenantID uint64, kbID, id string) (*types.UniverUnit, error) {
	if err := m.requireEnabled(); err != nil {
		return nil, err
	}
	var unit types.UniverUnit
	err := m.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).
		First(&unit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &unit, nil
}

// DeleteUnit removes the metadata row. The collaboration unit is left in place
// so an accidental delete can be recovered from the store.
func (m *Manager) DeleteUnit(ctx context.Context, tenantID uint64, kbID, id string) error {
	if err := m.requireEnabled(); err != nil {
		return err
	}
	return m.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND id = ?", tenantID, kbID, id).
		Delete(&types.UniverUnit{}).Error
}

// ResolveUnit accepts a unit id, a collaboration unit id, or a name, restricted
// to the given knowledge bases.
func (m *Manager) ResolveUnit(
	ctx context.Context, tenantID uint64, kbIDs []string, ref string,
) (*types.UniverUnit, error) {
	if err := m.requireEnabled(); err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("unit reference is required")
	}
	query := m.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if len(kbIDs) > 0 {
		query = query.Where("knowledge_base_id IN ?", kbIDs)
	}
	var unit types.UniverUnit
	if err := query.Where("id = ? OR unit_id = ?", ref, ref).First(&unit).Error; err == nil {
		return &unit, nil
	}
	fuzzy := m.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if len(kbIDs) > 0 {
		fuzzy = fuzzy.Where("knowledge_base_id IN ?", kbIDs)
	}
	err := fuzzy.Where("LOWER(name) = LOWER(?)", ref).First(&unit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("no collaborative entry named %q in scope", ref)
	}
	if err != nil {
		return nil, err
	}
	return &unit, nil
}

// ---------------------------------------------------------------------------
// Content
// ---------------------------------------------------------------------------

// actingUser attributes a change to the caller, falling back to the creator
// when the call has no user (maintenance jobs).
func actingUser(userID string, unit *types.UniverUnit) string {
	if strings.TrimSpace(userID) != "" {
		return userID
	}
	return unit.CreatorID
}

// ReadContent returns a kind-specific textual rendering.
func (m *Manager) ReadContent(
	ctx context.Context, userID string, unit *types.UniverUnit, cellRange string,
) (string, error) {
	if err := m.requireEnabled(); err != nil {
		return "", err
	}
	return m.client.ReadContent(ctx, actingUser(userID, unit), unit.UnitID, cellRange)
}

// WriteContent applies an edit shaped by the unit kind.
func (m *Manager) WriteContent(
	ctx context.Context, userID string, unit *types.UniverUnit, cellRange string, values [][]any,
) error {
	if err := m.requireEnabled(); err != nil {
		return err
	}
	actor := actingUser(userID, unit)
	switch unit.Kind {
	case types.UniverKindSheet:
		_, err := m.client.WriteValues(ctx, actor, unit.UnitID, cellRange, values)
		return err
	case types.UniverKindDoc:
		lines := make([]string, 0, len(values))
		for _, row := range values {
			cells := make([]string, 0, len(row))
			for _, cell := range row {
				cells = append(cells, fmt.Sprintf("%v", cell))
			}
			lines = append(lines, strings.Join(cells, " "))
		}
		_, err := m.client.AppendText(ctx, actor, unit.UnitID, strings.Join(lines, "\n"))
		return err
	default:
		return fmt.Errorf("editing a %s entry is not supported yet; open it in the editor", unit.Kind)
	}
}

// Describe returns the structure behind one unit.
func (m *Manager) Describe(
	ctx context.Context, userID string, unit *types.UniverUnit,
) (map[string]any, error) {
	if err := m.requireEnabled(); err != nil {
		return nil, err
	}
	meta, err := m.client.Meta(ctx, actingUser(userID, unit), unit.UnitID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"id":            unit.ID,
		"name":          unit.Name,
		"kind":          unit.Kind,
		"unit_id":       unit.UnitID,
		"head_revision": meta.HeadRevision,
	}
	if meta.Title != "" {
		out["title"] = meta.Title
	}
	if len(meta.Sheets) > 0 {
		out["sheets"] = meta.Sheets
	}
	if len(meta.Tables) > 0 {
		out["tables"] = meta.Tables
	}
	if len(meta.Slides) > 0 {
		out["slides"] = meta.Slides
	}
	return out, nil
}

// CheckUnitAccess answers the collaboration server's ACL question. Any member
// of the owning workspace may read; contributors and above may write.
func (m *Manager) CheckUnitAccess(ctx context.Context, userID, unitID, action string) (bool, error) {
	if m == nil || !m.enabled {
		return false, nil
	}
	if m.members == nil {
		return false, errors.New("member lookup unavailable")
	}
	var unit types.UniverUnit
	err := m.db.WithContext(ctx).Where("unit_id = ?", unitID).First(&unit).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	member, err := m.members.GetMembership(ctx, userID, unit.TenantID)
	if err != nil {
		return false, err
	}
	if member == nil {
		return false, nil
	}
	if action != "write" {
		return true, nil
	}
	switch member.Role {
	case types.TenantRoleOwner, types.TenantRoleAdmin, types.TenantRoleContributor:
		return true, nil
	}
	return false, nil
}
