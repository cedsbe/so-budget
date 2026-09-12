package domain

import (
	"testing"
	"time"
)

func TestComputeMonthOpenAndClosed(t *testing.T) {
	sep := Month{2026, time.September}
	alice, bob := User{1, "alice"}, User{2, "bob"}
	users := []User{alice, bob}
	contribs := map[int64]Cents{1: 200000, 2: 150000}
	rent := PlannedExpense{ID: 1, Name: "Rent", Amount: 185000, CategoryID: 1, PayerID: 1, Recurring: true, Active: true}
	gym := PlannedExpense{ID: 2, Name: "Gym", Amount: 4000, CategoryID: 2, PayerID: 2, Recurring: true, Active: true}
	entries := []HouseholdEntry{
		{ID: 1, PayerID: 1, Date: sep.Start(), Amount: 185000, CategoryID: 1, PlannedID: 1},
		{ID: 2, PayerID: 2, Date: sep.Start().AddDate(0, 0, 3), Amount: 8000, CategoryID: 3},
	}
	// Open month: gym counts as committed though unmatched.
	r := ComputeMonth(sep, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC), users, contribs, entries, []PlannedExpense{rent, gym})
	if r.Closed || r.Contributions != 350000 || r.Spent != 193000 || r.PlannedUnmatched != 4000 || r.Committed != 197000 || r.Margin != 153000 {
		t.Fatalf("open %+v", r)
	}
	if r.People[0].Balance != 15000 || r.People[1].Spent != 8000 || r.People[1].PlannedUnmatched != 4000 || r.People[1].Balance != 138000 {
		t.Fatalf("people %+v", r.People)
	}
	if len(r.Planned) != 2 || r.Planned[0].Matched != 185000 || !r.Planned[0].Occurred || r.Planned[1].Occurred {
		t.Fatalf("planned %+v", r.Planned)
	}
	// Closed month: gym did not occur and is excluded.
	r = ComputeMonth(sep, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), users, contribs, entries, []PlannedExpense{rent, gym})
	if !r.Closed || r.PlannedUnmatched != 0 || r.Committed != 193000 || r.Margin != 157000 || r.Planned[1].Occurred {
		t.Fatalf("closed %+v", r)
	}
}

// A planned expense linked to an entry whose amount sums to exactly 0 must
// still be reported as occurred (presence of a match, not the sum, decides),
// so it is not double-counted into PlannedUnmatched/Committed.
func TestComputeMonthZeroAmountMatchStillOccurred(t *testing.T) {
	sep := Month{2026, time.September}
	alice := User{1, "alice"}
	users := []User{alice}
	contribs := map[int64]Cents{1: 100000}
	waived := PlannedExpense{ID: 1, Name: "Waived fee", Amount: 5000, CategoryID: 1, PayerID: 1, Recurring: true, Active: true}
	entries := []HouseholdEntry{
		{ID: 1, PayerID: 1, Date: sep.Start(), Amount: 0, CategoryID: 1, PlannedID: 1},
	}
	r := ComputeMonth(sep, time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC), users, contribs, entries, []PlannedExpense{waived})
	if len(r.Planned) != 1 || !r.Planned[0].Occurred || r.Planned[0].Matched != 0 {
		t.Fatalf("planned %+v", r.Planned)
	}
	if r.PlannedUnmatched != 0 || r.Committed != 0 || r.Margin != 100000 {
		t.Fatalf("open %+v", r)
	}
}
