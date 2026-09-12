package service

import (
	"context"
	"testing"

	"github.com/cedsbe/so-budget/internal/crypto"
)

func TestInviteActivateLogin(t *testing.T) {
	svc := NewTestService(t)
	ctx := context.Background()
	tok, err := svc.Invite(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := svc.InviteName(ctx, tok); name != "alice" {
		t.Fatalf("invite name %q", name)
	}
	if _, err := svc.Activate(ctx, tok, "short"); err != ErrWeakPassword {
		t.Fatalf("weak password: %v", err)
	}
	code, err := svc.Activate(ctx, tok, "correct horse battery")
	if err != nil || len(code) != 39 {
		t.Fatalf("activate: %q %v", code, err)
	}
	if _, err := svc.Activate(ctx, tok, "correct horse battery"); err != ErrInviteUsed {
		t.Fatalf("reuse: %v", err)
	}
	p, err := svc.Login(ctx, "alice", "correct horse battery")
	if err != nil || p.Name != "alice" || p.Key == ([32]byte{}) {
		t.Fatalf("login: %+v %v", p, err)
	}
	if _, err := svc.Login(ctx, "alice", "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("wrong pw: %v", err)
	}
	if _, err := svc.Login(ctx, "nobody", "x"); err != ErrInvalidCredentials {
		t.Fatalf("unknown user: %v", err)
	}

	if err := svc.ChangePassword(ctx, p, "wrong", "new password here"); err != ErrInvalidCredentials {
		t.Fatalf("change with wrong old: %v", err)
	}
	if err := svc.ChangePassword(ctx, p, "correct horse battery", "new password here"); err != nil {
		t.Fatal(err)
	}
	p2, err := svc.Login(ctx, "alice", "new password here")
	if err != nil || p2.Key != p.Key {
		t.Fatalf("key must survive password change: %v", err)
	}

	newCode, err := svc.Recover(ctx, "alice", code, "recovered password")
	if err != nil || newCode == code {
		t.Fatalf("recover: %v", err)
	}
	if _, err := svc.Recover(ctx, "alice", code, "again"); err != ErrInvalidCredentials {
		t.Fatalf("old code must be dead: %v", err)
	}
	p3, err := svc.Login(ctx, "alice", "recovered password")
	if err != nil || p3.Key != p.Key {
		t.Fatalf("key must survive recovery: %v", err)
	}
	users, _ := svc.Users(ctx)
	if len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("users: %+v", users)
	}
}

// TestChangePasswordKeepsRowKDF pins a regression: ChangePassword must reuse the
// row's stored KDF params (not the service's currently configured ones) so that
// the recovery code, wrapped under the same row params, keeps working even if
// the service's KDF settings change later.
func TestChangePasswordKeepsRowKDF(t *testing.T) {
	svc := NewTestService(t)
	ctx := context.Background()

	tok, err := svc.Invite(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.Activate(ctx, tok, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Login(ctx, "bob", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the service's configured KDF params being raised after activation.
	svc.kdf = crypto.KDFParams{Time: 2, Memory: 8 * 1024, Threads: 1}

	if err := svc.ChangePassword(ctx, p, "correct horse battery", "new password here"); err != nil {
		t.Fatal(err)
	}

	p2, err := svc.Login(ctx, "bob", "new password here")
	if err != nil {
		t.Fatal(err)
	}
	if p2.Key != p.Key {
		t.Fatal("key must survive password change even after KDF params changed")
	}

	// The recovery code was wrapped under the row's original params and must
	// still work even though svc.kdf has since changed and the password was
	// re-wrapped by ChangePassword.
	newCode, err := svc.Recover(ctx, "bob", code, "recovered password")
	if err != nil || newCode == code {
		t.Fatalf("recover after password change: %q %v", newCode, err)
	}
}
