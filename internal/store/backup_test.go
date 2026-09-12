package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAndPrune(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	s.CreateCategory(ctx, "Rent")
	dir := t.TempDir()
	for _, name := range []string{"so-budget-20260901-0000.db", "so-budget-20260902-0000.db", "so-budget-20260903-0000.db"} {
		if err := s.BackupTo(ctx, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	b, err := Open(filepath.Join(dir, "so-budget-20260903-0000.db"))
	if err != nil {
		t.Fatal(err)
	}
	cats, _ := b.ListCategories(ctx, false)
	b.Close()
	if len(cats) != 1 {
		t.Fatalf("backup should contain the category, got %+v", cats)
	}
	if err := PruneBackups(dir, 2); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 2 || left[0].Name() != "so-budget-20260902-0000.db" {
		t.Fatalf("prune left %v", left)
	}
}
