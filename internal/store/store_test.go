package store

import (
	"context"
	"testing"
)

func TestOpenAppliesMigrationsIdempotently(t *testing.T) {
	path := t.TempDir() + "/test.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, table := range []string{"users", "credentials", "bank_accounts", "transactions", "private_rules", "sync_state", "categories", "household_entries", "household_rules", "planned_expenses", "contributions"} {
		var n int
		err := s.q.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("table %s missing (%v)", table, err)
		}
	}
	var applied int
	s.q.QueryRowContext(context.Background(), `SELECT count(*) FROM schema_migrations`).Scan(&applied)
	if applied != 2 {
		t.Fatalf("want 2 applied migrations, got %d", applied)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	err := s.WithTx(ctx, func(tx *Store) error {
		if _, err := tx.q.ExecContext(ctx, `INSERT INTO categories(name) VALUES ('x')`); err != nil {
			return err
		}
		return context.Canceled
	})
	if err != context.Canceled {
		t.Fatalf("want error passthrough, got %v", err)
	}
	var n int
	s.q.QueryRowContext(ctx, `SELECT count(*) FROM categories`).Scan(&n)
	if n != 0 {
		t.Fatal("insert should have rolled back")
	}
}
