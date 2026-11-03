package im

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestWithIMIdentity(t *testing.T) {
	const tenantID uint64 = 42
	msg := &IncomingMessage{Platform: PlatformFeishu, UserID: "open-id-1"}
	channel := &IMChannel{ID: "channel-1", TenantID: tenantID, Locale: "ja-JP"}
	ctx := withIMIdentity(context.Background(), channel, msg)

	gotTenant, ok := types.TenantIDFromContext(ctx)
	if !ok || gotTenant != tenantID {
		t.Fatalf("TenantID = %d (ok=%v), want %d", gotTenant, ok, tenantID)
	}

	userID, ok := types.UserIDFromContext(ctx)
	if !ok || userID == "" {
		t.Fatalf("UserID = %q (ok=%v), want non-empty synthetic user", userID, ok)
	}
	if want := "system-42"; userID != want {
		t.Fatalf("UserID = %q, want %q", userID, want)
	}

	// The synthetic shape must be recognised so RBAC code does not record it
	// as a resource creator.
	if !types.IsSyntheticUserID(userID) {
		t.Fatalf("IsSyntheticUserID(%q) = false, want true", userID)
	}

	// Non-empty UserID is the gate the shared-KB resolution relies on; without
	// it Organization-shared KBs are silently skipped on the IM path.
	if role := types.TenantRoleFromContext(ctx); role != types.TenantRoleViewer {
		t.Fatalf("TenantRole = %v, want %v", role, types.TenantRoleViewer)
	}

	principal, ok := types.PrincipalFromContext(ctx)
	if !ok {
		t.Fatalf("Principal missing")
	}
	if principal.Type != types.PrincipalIMUser || principal.ID != "42:channel-1:feishu:open-id-1" {
		t.Fatalf("Principal = %#v, want im_user for the external IM user", principal)
	}

	if !types.IsMCPOAuthNonInteractive(ctx) {
		t.Fatal("IM context should mark MCP OAuth as non-interactive")
	}
	if got, ok := types.LanguageFromContext(ctx); !ok || got != "ja-JP" {
		t.Fatalf("Language = %q, want %q", got, "ja-JP")
	}
}

func TestWithIMIdentityUsesDeploymentDefaultWithoutChannelLocale(t *testing.T) {
	t.Setenv("WEKNORA_LANGUAGE", "ko-KR")
	ctx := context.WithValue(context.Background(), types.LanguageContextKey, "en-US")
	ctx = withIMIdentity(ctx, &IMChannel{ID: "channel-1", TenantID: 42}, nil)

	if got, ok := types.LanguageFromContext(ctx); !ok || got != "ko-KR" {
		t.Fatalf("Language = %q, want deployment default %q", got, "ko-KR")
	}
}

func TestWithIMIdentityAppliesChannelLocaleAcrossTransports(t *testing.T) {
	for _, mode := range []string{"webhook", "websocket", "longpoll"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), types.LanguageContextKey, "en-US")
			channel := &IMChannel{ID: "channel-1", TenantID: 42, Mode: mode, Locale: "ru-RU"}
			ctx = withIMIdentity(ctx, channel, nil)

			if got, ok := types.LanguageFromContext(ctx); !ok || got != "ru-RU" {
				t.Fatalf("Language = %q, want channel locale %q", got, "ru-RU")
			}
		})
	}
}

func TestWithIMIdentityRunAsBoundUser(t *testing.T) {
	const tenantID uint64 = 42
	channel := &IMChannel{ID: "channel-1", TenantID: tenantID, Locale: "zh-CN"}
	msg := &IncomingMessage{Platform: PlatformWeChat, UserID: "open-id-1"}

	ctx := withIMIdentityRunAs(context.Background(), channel, msg, &imRunAs{
		UserID: "user-9",
		Role:   types.TenantRoleAdmin,
	})

	if got, _ := types.TenantIDFromContext(ctx); got != tenantID {
		t.Fatalf("TenantID = %d, want %d", got, tenantID)
	}
	if got, _ := types.UserIDFromContext(ctx); got != "user-9" {
		t.Fatalf("UserID = %q, want %q", got, "user-9")
	}
	if got := types.TenantRoleFromContext(ctx); got != types.TenantRoleAdmin {
		t.Fatalf("TenantRole = %v, want %v", got, types.TenantRoleAdmin)
	}
	principal, ok := types.PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("Principal missing")
	}
	if principal.Type != types.PrincipalWebUser || principal.ID != "user-9" {
		t.Fatalf("Principal = %#v, want web_user:user-9", principal)
	}
	if want := types.IMActingVia("channel-1"); principal.ActingVia != want {
		t.Fatalf("ActingVia = %q, want %q", principal.ActingVia, want)
	}
	if got := types.PrincipalAuthSourceFromContext(ctx); got != types.AuthSourceIM {
		t.Fatalf("PrincipalAuthSource = %q, want %q", got, types.AuthSourceIM)
	}
	if !types.IsMCPOAuthNonInteractive(ctx) {
		t.Fatal("bound IM runtime must still mark MCP OAuth non-interactive")
	}
	if got, _ := types.LanguageFromContext(ctx); got != "zh-CN" {
		t.Fatalf("Language = %q, want zh-CN", got)
	}
}

func TestIMSessionOwnerID(t *testing.T) {
	channel := &IMChannel{ID: "channel-1", TenantID: 42}
	msg := &IncomingMessage{Platform: PlatformWeChat, UserID: "open-id-1"}

	bound := withIMIdentityRunAs(context.Background(), channel, msg, &imRunAs{
		UserID: "user-9",
		Role:   types.TenantRoleContributor,
	})
	if got := imSessionOwnerID(bound); got != "user-9" {
		t.Fatalf("imSessionOwnerID(bound) = %q, want user-9", got)
	}

	legacy := withIMIdentity(context.Background(), channel, msg)
	if got := imSessionOwnerID(legacy); got != "" {
		t.Fatalf("imSessionOwnerID(legacy) = %q, want empty tenant-level owner", got)
	}

	web := context.WithValue(context.Background(), types.UserIDContextKey, "user-9")
	web = types.WithPrincipal(web, types.Principal{Type: types.PrincipalWebUser, ID: "user-9"})
	if got := imSessionOwnerID(web); got != "" {
		t.Fatalf("imSessionOwnerID(web chat) = %q, want empty", got)
	}
}
