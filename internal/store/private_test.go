package store

import (
	"context"
	"testing"
	"time"
)

func newUser(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	id, err := s.CreateInvitedUser(context.Background(), name, "inv-"+name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCredentialAndAccounts(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	u := newUser(t, s, "a")
	if _, err := s.Credential(ctx, u); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	s.SaveCredential(ctx, u, []byte("one"))
	s.SaveCredential(ctx, u, []byte("two"))
	if b, _ := s.Credential(ctx, u); string(b) != "two" {
		t.Fatalf("got %q", b)
	}
	s.UpsertAccount(ctx, u, "h1", []byte("x"))
	s.UpsertAccount(ctx, u, "h1", []byte("y"))
	s.UpsertAccount(ctx, u, "h2", []byte("z"))
	rows, _ := s.ListAccounts(ctx, u)
	if len(rows) != 2 || rows[0].Blob == nil {
		t.Fatalf("accounts %+v", rows)
	}
}

func TestTransactions(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	a, b := newUser(t, s, "a"), newUser(t, s, "b")
	for _, r := range []TxRow{
		{IDHash: "t1", State: "new", Month: "2026-09", Blob: []byte("1")},
		{IDHash: "t2", State: "new", Month: "2026-08", Blob: []byte("2")},
		{IDHash: "t3", State: "dismissed", Month: "2026-09", Blob: []byte("3")},
	} {
		if err := s.InsertTransaction(ctx, a, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InsertTransaction(ctx, a, TxRow{IDHash: "t1", State: "new", Month: "2026-09", Blob: []byte("dup")}); err == nil {
		t.Fatal("duplicate must fail")
	}
	ex, _ := s.ExistingTransactionHashes(ctx, a, []string{"t1", "t9", "t3"})
	if !ex["t1"] || ex["t9"] || !ex["t3"] {
		t.Fatalf("existing %v", ex)
	}
	if ex, _ := s.ExistingTransactionHashes(ctx, b, []string{"t1"}); ex["t1"] {
		t.Fatal("hashes are per user")
	}
	rows, _ := s.ListTransactions(ctx, a, TxFilter{State: "new"})
	if len(rows) != 2 {
		t.Fatalf("new rows %d", len(rows))
	}
	rows, _ = s.ListTransactions(ctx, a, TxFilter{Month: "2026-09"})
	if len(rows) != 2 {
		t.Fatalf("month rows %d", len(rows))
	}
	rows, _ = s.ListTransactions(ctx, b, TxFilter{})
	if len(rows) != 0 {
		t.Fatal("user b must see nothing")
	}
	s.SetTransactionState(ctx, a, "t1", "flagged")
	s.UpdateTransactionBlob(ctx, a, "t1", []byte("1b"))
	r, err := s.Transaction(ctx, a, "t1")
	if err != nil || r.State != "flagged" || string(r.Blob) != "1b" {
		t.Fatalf("tx %+v %v", r, err)
	}
	if _, err := s.Transaction(ctx, b, "t1"); err != ErrNotFound {
		t.Fatal("cross-user read must be not found")
	}
	if n, _ := s.CountTransactions(ctx, a, "new"); n != 1 {
		t.Fatalf("count %d", n)
	}
}

func TestSyncStateAndPrivateRules(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	u := newUser(t, s, "a")
	if _, ok, _ := s.LastSync(ctx, u); ok {
		t.Fatal("no sync yet")
	}
	ts := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s.SetLastSync(ctx, u, ts)
	s.SetLastSync(ctx, u, ts.Add(time.Hour))
	got, ok, _ := s.LastSync(ctx, u)
	if !ok || !got.Equal(ts.Add(time.Hour)) {
		t.Fatalf("last sync %v %v", got, ok)
	}
	s.InsertPrivateRule(ctx, u, "r1", []byte("x"))
	s.InsertPrivateRule(ctx, u, "r2", []byte("y"))
	rules, _ := s.ListPrivateRules(ctx, u)
	if len(rules) != 2 || rules[0].ID != "r1" {
		t.Fatalf("rules %+v", rules)
	}
	s.DeletePrivateRule(ctx, u, "r1")
	rules, _ = s.ListPrivateRules(ctx, u)
	if len(rules) != 1 {
		t.Fatal("delete failed")
	}
}
