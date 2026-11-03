package im

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type fakeMembershipLookup struct {
	member *types.TenantMember
	err    error
	calls  int
}

func (f *fakeMembershipLookup) GetMembership(_ context.Context, _ string, _ uint64) (*types.TenantMember, error) {
	f.calls++
	return f.member, f.err
}

func TestResolveChannelRunAs(t *testing.T) {
	t.Run("legacy channel without bound user", func(t *testing.T) {
		lookup := &fakeMembershipLookup{}
		s := &Service{memberService: lookup}
		if got := s.resolveChannelRunAs(context.Background(), &IMChannel{ID: "c1", TenantID: 42}); got != nil {
			t.Fatalf("resolveChannelRunAs() = %#v, want nil", got)
		}
		if lookup.calls != 0 {
			t.Fatalf("membership lookup called %d times, want 0", lookup.calls)
		}
	})

	t.Run("active member", func(t *testing.T) {
		lookup := &fakeMembershipLookup{member: &types.TenantMember{
			UserID: "user-9", TenantID: 42, Role: types.TenantRoleAdmin,
		}}
		s := &Service{memberService: lookup}
		got := s.resolveChannelRunAs(context.Background(), &IMChannel{
			ID: "c1", TenantID: 42, BoundUserID: "user-9",
		})
		if got == nil || got.UserID != "user-9" || got.Role != types.TenantRoleAdmin {
			t.Fatalf("resolveChannelRunAs() = %#v, want user-9/admin", got)
		}
	})

	t.Run("membership revoked", func(t *testing.T) {
		lookup := &fakeMembershipLookup{member: nil}
		s := &Service{memberService: lookup}
		if got := s.resolveChannelRunAs(context.Background(), &IMChannel{
			ID: "c1", TenantID: 42, BoundUserID: "user-9",
		}); got != nil {
			t.Fatalf("resolveChannelRunAs() = %#v, want fallback", got)
		}
	})

	t.Run("lookup failure fails closed", func(t *testing.T) {
		lookup := &fakeMembershipLookup{err: errors.New("db down")}
		s := &Service{memberService: lookup}
		if got := s.resolveChannelRunAs(context.Background(), &IMChannel{
			ID: "c1", TenantID: 42, BoundUserID: "user-9",
		}); got != nil {
			t.Fatalf("resolveChannelRunAs() = %#v, want fallback", got)
		}
	})
}

func TestSetChannelBoundUser(t *testing.T) {
	lookup := &fakeMembershipLookup{member: &types.TenantMember{
		UserID: "user-9", TenantID: 42, Role: types.TenantRoleOwner,
	}}
	s := &Service{memberService: lookup}
	channel := &IMChannel{ID: "c1", TenantID: 42}

	if err := s.SetChannelBoundUser(context.Background(), channel, " user-9 "); err != nil {
		t.Fatalf("SetChannelBoundUser() error = %v", err)
	}
	if channel.BoundUserID != "user-9" {
		t.Fatalf("BoundUserID = %q, want user-9", channel.BoundUserID)
	}

	lookup.member = nil
	if err := s.SetChannelBoundUser(context.Background(), channel, "ghost"); err == nil {
		t.Fatal("SetChannelBoundUser() = nil, want rejection for non-member")
	}
	if channel.BoundUserID != "user-9" {
		t.Fatalf("rejected update changed BoundUserID to %q", channel.BoundUserID)
	}

	if err := s.SetChannelBoundUser(context.Background(), channel, ""); err != nil {
		t.Fatalf("clearing binding: %v", err)
	}
	if channel.BoundUserID != "" {
		t.Fatalf("BoundUserID = %q after clear, want empty", channel.BoundUserID)
	}
}
