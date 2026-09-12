package service

import (
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/store"
)

// NewTestService returns a Service on a fresh temp database with fast KDF params.
// Task 8 extends this to also return a *simplefin.Fake.
func NewTestService(t testing.TB) *Service {
	t.Helper()
	st := store.OpenTest(t)
	return New(st, nil, Options{KDF: crypto.TestParams, Pepper: []byte("test-pepper"), Location: time.UTC})
}
