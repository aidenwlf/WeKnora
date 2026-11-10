package univer

import (
	"strings"
	"testing"
)

func testManager(t *testing.T, secret string) *Manager {
	t.Helper()
	return NewManager(nil, nil, Options{
		Enabled:    true,
		Internal:   DefaultInternalURL,
		PublicPath: DefaultPublicPath,
		CookieName: DefaultCookieName,
		Secret:     secret,
	})
}

func TestSessionCookieRoundTrip(t *testing.T) {
	m := testManager(t, "shared-secret")

	name, value, maxAge, err := m.SessionCookie("user-1")
	if err != nil {
		t.Fatalf("SessionCookie: %v", err)
	}
	if name != DefaultCookieName {
		t.Fatalf("cookie name = %q, want %q", name, DefaultCookieName)
	}
	if maxAge <= 0 {
		t.Fatalf("maxAge = %d, want > 0", maxAge)
	}
	userID, ok := m.SessionUser(value)
	if !ok || userID != "user-1" {
		t.Fatalf("SessionUser = (%q, %v), want (user-1, true)", userID, ok)
	}
}

func TestSessionUserRejectsTampering(t *testing.T) {
	m := testManager(t, "shared-secret")
	_, value, _, err := m.SessionCookie("user-1")
	if err != nil {
		t.Fatalf("SessionCookie: %v", err)
	}
	body, mac, _ := strings.Cut(value, ".")

	// A different identity with the original signature must not verify.
	other := testManager(t, "shared-secret")
	forgedBody := strings.Split(encodeBody(t, other, "user-2"), ".")[0]
	if _, ok := m.SessionUser(forgedBody + "." + mac); ok {
		t.Fatal("forged payload verified")
	}

	// A signature produced with another secret must not verify.
	otherSecret := testManager(t, "other-secret")
	_, forged, _, err := otherSecret.SessionCookie("user-1")
	if err != nil {
		t.Fatalf("SessionCookie(other): %v", err)
	}
	if _, ok := m.SessionUser(forged); ok {
		t.Fatal("signature from another secret verified")
	}

	// Garbage input is rejected rather than panicking.
	for _, bad := range []string{"", ".", body, "not-a-token", body + "."} {
		if _, ok := m.SessionUser(bad); ok {
			t.Fatalf("SessionUser(%q) verified", bad)
		}
	}
}

func encodeBody(t *testing.T, m *Manager, userID string) string {
	t.Helper()
	_, value, _, err := m.SessionCookie(userID)
	if err != nil {
		t.Fatalf("SessionCookie: %v", err)
	}
	return value
}

func TestForwardHeadersCarryIdentityAndSecret(t *testing.T) {
	m := testManager(t, "shared-secret")
	headers := m.ForwardHeaders("user-1")
	if headers["X-WeKnora-User"] != "user-1" {
		t.Fatalf("identity header = %q", headers["X-WeKnora-User"])
	}
	if headers["X-WeKnora-Secret"] != "shared-secret" {
		t.Fatalf("secret header = %q", headers["X-WeKnora-Secret"])
	}
}

func TestDisabledManagerRejectsSession(t *testing.T) {
	m := NewManager(nil, nil, Options{Enabled: false, Secret: "s"})
	if m.Enabled() {
		t.Fatal("disabled manager reports enabled")
	}
	if _, _, _, err := m.SessionCookie("user-1"); err == nil {
		t.Fatal("SessionCookie succeeded while disabled")
	}
}
