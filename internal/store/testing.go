package store

import "testing"

// OpenTest opens a fresh database in a temp dir and closes it when the test ends.
func OpenTest(t testing.TB) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
