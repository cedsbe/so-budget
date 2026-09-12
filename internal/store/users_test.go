package store

import (
	"context"
	"testing"

	"github.com/cedsbe/so-budget/internal/crypto"
)

func TestUsersLifecycle(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	id, err := s.CreateInvitedUser(ctx, "alice", "hash-a")
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.UserByInvite(ctx, "hash-a")
	if err != nil || u.ID != id || u.Active {
		t.Fatalf("by invite: %+v %v", u, err)
	}
	if _, err := s.UserByInvite(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	keys := UserKeys{PwSalt: []byte("s1"), PwWrapped: []byte("w1"), RcSalt: []byte("s2"), RcWrapped: []byte("w2"), KDF: crypto.TestParams}
	if err := s.SetUserKeys(ctx, id, keys); err != nil {
		t.Fatal(err)
	}
	u, err = s.UserByName(ctx, "alice")
	if err != nil || !u.Active || string(u.PwWrapped) != "w1" || u.KDF != crypto.TestParams || u.InviteHash != "" {
		t.Fatalf("by name: %+v %v", u, err)
	}
	if _, err := s.UserByInvite(ctx, "hash-a"); err != ErrNotFound {
		t.Fatal("invite should be consumed")
	}
	if _, err := s.CreateInvitedUser(ctx, "alice", "hash-b"); err == nil {
		t.Fatal("duplicate name should fail")
	}
	all, _ := s.ListUsers(ctx)
	if len(all) != 1 || all[0].Name != "alice" {
		t.Fatalf("list: %+v", all)
	}
}
