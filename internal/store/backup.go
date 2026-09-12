package store

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BackupTo writes a consistent copy of the database to dest using VACUUM INTO.
// Private data stays sealed in the copy.
func (s *Store) BackupTo(ctx context.Context, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	_ = os.Remove(dest)
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dest)
	return err
}

// PruneBackups keeps the newest `keep` files named so-budget-*.db in dir.
func PruneBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "so-budget-") && strings.HasSuffix(e.Name(), ".db") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}
