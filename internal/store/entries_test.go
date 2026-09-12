package store

import (
	"context"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

func TestEntries(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	u := newUser(t, s, "a")
	cat, _ := s.CreateCategory(ctx, "Groceries")
	d := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	id, err := s.CreateEntry(ctx, domain.HouseholdEntry{PayerID: u, Date: d(5), Amount: 4321, CategoryID: cat, Note: "n", Source: domain.SourceSync, SourceRef: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	s.CreateEntry(ctx, domain.HouseholdEntry{PayerID: u, Date: d(1), Amount: 100, CategoryID: cat, Source: domain.SourceManual})
	s.CreateEntry(ctx, domain.HouseholdEntry{PayerID: u, Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Amount: 100, CategoryID: cat, Source: domain.SourceManual})
	if _, err := s.CreateEntry(ctx, domain.HouseholdEntry{PayerID: u, Date: d(5), Amount: 1, Source: domain.SourceSync, SourceRef: "h1"}); err == nil {
		t.Fatal("same source ref twice must fail")
	}
	e, err := s.Entry(ctx, id)
	if err != nil || e.Amount != 4321 || e.SourceRef != "h1" || !e.Date.Equal(d(5)) {
		t.Fatalf("entry %+v %v", e, err)
	}
	list, _ := s.ListEntries(ctx, domain.Month{Year: 2026, Mon: time.September})
	if len(list) != 2 || !list[0].Date.Equal(d(1)) {
		t.Fatalf("list %+v", list)
	}
	between, _ := s.ListEntriesBetween(ctx, d(2), d(6))
	if len(between) != 1 {
		t.Fatalf("between %+v", between)
	}
	e.Amount = 4000
	e.Note = "edited"
	e.PlannedID = 7
	if err := s.UpdateEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	e2, _ := s.EntryBySourceRef(ctx, u, "h1")
	if e2.Amount != 4000 || e2.Note != "edited" || e2.PlannedID != 7 {
		t.Fatalf("updated %+v", e2)
	}
	if err := s.DeleteEntry(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Entry(ctx, id); err != ErrNotFound {
		t.Fatal("should be gone")
	}
	if err := s.DeleteEntry(ctx, id); err != ErrNotFound {
		t.Fatal("double delete should be not found")
	}
}
