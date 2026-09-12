package service

import (
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
