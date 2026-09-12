package service

import (
	"context"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/simplefin"
	"github.com/cedsbe/so-budget/internal/store"
)

// NewTestService returns a Service on a fresh temp database with fast KDF params
// and a fake SimpleFIN server.
func NewTestService(t testing.TB) (*Service, *simplefin.Fake) {
	t.Helper()
	st := store.OpenTest(t)
	f := simplefin.NewFake()
	t.Cleanup(f.Close)
	svc := New(st, simplefin.NewHTTPClient(), Options{KDF: crypto.TestParams, Pepper: []byte("test-pepper"), Location: time.UTC})
	return svc, f
}

// LoginTestUser creates and activates a user and returns a logged-in principal.
func LoginTestUser(t testing.TB, svc *Service, name string) Principal {
	t.Helper()
	ctx := context.Background()
	tok, err := svc.Invite(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(ctx, tok, "password-"+name); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Login(ctx, name, "password-"+name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func storeFilter(state string) store.TxFilter { return store.TxFilter{State: state} }
