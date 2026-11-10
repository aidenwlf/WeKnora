package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/types"
)

// stubUniverService implements just enough of the interface for the proxy.
type stubUniverService struct {
	userID string
}

func (s *stubUniverService) Enabled() bool      { return true }
func (s *stubUniverService) PublicPath() string { return "/api/v1/univer/universer-api" }
func (s *stubUniverService) CookieName() string { return "weknora_univer" }
func (s *stubUniverService) SessionCookie(string) (string, string, int, error) {
	return "weknora_univer", "token", 60, nil
}

func (s *stubUniverService) SessionUser(value string) (string, bool) {
	if value != "token" {
		return "", false
	}
	return s.userID, true
}

func (s *stubUniverService) ForwardHeaders(userID string) map[string]string {
	return map[string]string{
		"X-WeKnora-User":   userID,
		"X-WeKnora-Secret": "shared-secret",
	}
}

func (s *stubUniverService) CreateUnit(context.Context, uint64, string, string, string, string) (*types.UniverUnit, error) {
	return nil, nil
}
func (s *stubUniverService) ListUnits(context.Context, uint64, string) ([]*types.UniverUnit, error) {
	return nil, nil
}
func (s *stubUniverService) GetUnit(context.Context, uint64, string, string) (*types.UniverUnit, error) {
	return nil, nil
}
func (s *stubUniverService) DeleteUnit(context.Context, uint64, string, string) error { return nil }
func (s *stubUniverService) ResolveUnit(context.Context, uint64, []string, string) (*types.UniverUnit, error) {
	return nil, nil
}
func (s *stubUniverService) ReadContent(context.Context, string, *types.UniverUnit, string) (string, error) {
	return "", nil
}
func (s *stubUniverService) WriteContent(context.Context, string, *types.UniverUnit, string, [][]any) error {
	return nil
}
func (s *stubUniverService) Describe(context.Context, string, *types.UniverUnit) (map[string]any, error) {
	return nil, nil
}
func (s *stubUniverService) CheckUnitAccess(context.Context, string, string, string) (bool, error) {
	return true, nil
}

func newProxyServer(t *testing.T) (*httptest.Server, *probeUpstream) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	upstream := &probeUpstream{}
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)

	proxy, err := NewUniverProxy(&stubUniverService{userID: "user-1"}, server.URL)
	if err != nil {
		t.Fatalf("NewUniverProxy: %v", err)
	}
	// The proxy dials the collaboration server with its own scheme; httptest
	// hands back a plain http URL, so rewriting the target is unnecessary.
	engine := gin.New()
	engine.Any("/api/v1/univer/universer-api/*path", proxy.Handle)
	// A real server is required: gin's writer asserts http.CloseNotifier,
	// which httptest.ResponseRecorder does not implement.
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)
	return gateway, upstream
}

type probeUpstream struct {
	path   string
	query  string
	header http.Header
	called bool
}

func (p *probeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.called = true
	p.path = r.URL.Path
	p.query = r.URL.RawQuery
	p.header = r.Header.Clone()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"error":{"code":1,"message":""},"objectActions":[]}`))
}

func TestUniverProxyRequiresCookie(t *testing.T) {
	gateway, upstream := newProxyServer(t)
	resp, err := http.Post(gateway.URL+"/api/v1/univer/universer-api/authz/-/object/-/batch_allowed", "application/json", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if upstream.called {
		t.Fatal("unauthenticated request reached the collaboration server")
	}
}

func TestUniverProxyForwardsIdentityAndStripsPrefix(t *testing.T) {
	gateway, upstream := newProxyServer(t)
	req, err := http.NewRequest(http.MethodPost,
		gateway.URL+"/api/v1/univer/universer-api/authz/-/object/-/batch_allowed?x=1", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "weknora_univer", Value: "token"})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if upstream.path != "/universer-api/authz/-/object/-/batch_allowed" {
		t.Fatalf("upstream path = %q", upstream.path)
	}
	if upstream.query != "x=1" {
		t.Fatalf("upstream query = %q", upstream.query)
	}
	if got := upstream.header.Get("X-WeKnora-User"); got != "user-1" {
		t.Fatalf("identity header = %q", got)
	}
	if got := upstream.header.Get("X-WeKnora-Secret"); got != "shared-secret" {
		t.Fatalf("secret header = %q", got)
	}
	if got := upstream.header.Get("Cookie"); got != "" {
		t.Fatalf("browser cookie leaked upstream: %q", got)
	}
}
