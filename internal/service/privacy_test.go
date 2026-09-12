package service

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/simplefin"
	"github.com/cedsbe/so-budget/internal/store"
)

// TestPrivacyInvariant is the reason the design exists: another user cannot see
// A's private data through any service call, and A's payees never touch the
// disk in plain text.
func TestPrivacyInvariant(t *testing.T) {
	dbPath := t.TempDir() + "/privacy.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	fake := simplefin.NewFake()
	defer fake.Close()
	set := fixtureSet()
	set.Accounts[0].Transactions[0].Description = "ZORGLUB SECRET MARKET"
	fake.Set(set)
	svc := New(st, simplefin.NewHTTPClient(), Options{KDF: crypto.TestParams, Pepper: []byte("pepper"), Location: time.UTC})
	svc.now = func() time.Time { return fixedNow }
	ctx := context.Background()

	a := linkedUser(t, svc, fake.SetupToken(), "alice")
	b := LoginTestUser(t, svc, "bob")
	svc.AddPrivateRule(ctx, a, "SECRET PRIVATE RULE")

	inbox, _ := svc.Inbox(ctx, a)
	item := findItem(t, inbox.Items, "ZORGLUB SECRET MARKET")

	// B sees nothing of A's.
	if ib, _ := svc.Inbox(ctx, b); len(ib.Items) != 0 || ib.Linked {
		t.Fatalf("B inbox %+v", ib)
	}
	if _, err := svc.Flag(ctx, b, item.IDHash, 0, 0, ""); err != ErrNotFound {
		t.Fatalf("B flagging A's tx: %v", err)
	}
	if err := svc.Dismiss(ctx, b, item.IDHash); err != ErrNotFound {
		t.Fatalf("B dismissing A's tx: %v", err)
	}
	if h, _ := svc.History(ctx, b, domain.MonthOf(fixedNow), ""); len(h) != 0 {
		t.Fatal("B history must be empty")
	}
	if accts, _ := svc.Accounts(ctx, b); len(accts) != 0 {
		t.Fatal("B must see no accounts")
	}
	if rules, _ := svc.PrivateRules(ctx, b); len(rules) != 0 {
		t.Fatal("B must see no private rules")
	}

	// Nothing private is on disk in plain text (WAL is checkpointed by Close).
	st.Close()
	raw, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{[]byte("ZORGLUB"), []byte("SECRET PRIVATE RULE"), []byte(fake.URL()), []byte("Chequing")} {
		if bytes.Contains(raw, secret) {
			t.Fatalf("plaintext %q found in database file", secret)
		}
	}
}
