package domain

import (
	"testing"
	"time"
)

func TestAmountWithinTolerance(t *testing.T) {
	cases := []struct {
		actual, expected Cents
		want             bool
	}{
		{185000, 185000, true}, {190000, 185000, true}, {166500, 185000, true}, {166000, 185000, false},
		{1500, 1200, true}, {1701, 1200, false}, {1000, 1000, true},
	}
	for _, c := range cases {
		if got := AmountWithinTolerance(c.actual, c.expected); got != c.want {
			t.Errorf("tolerance(%d,%d)=%v want %v", c.actual, c.expected, got, c.want)
		}
	}
}

func TestMatchPlanned(t *testing.T) {
	sep := Month{2026, time.September}
	rent := PlannedExpense{ID: 1, Amount: 185000, CategoryID: 10, PayerID: 1, Recurring: true, Active: true}
	rent2 := PlannedExpense{ID: 2, Amount: 186000, CategoryID: 10, PayerID: 1, Recurring: true, Active: true}
	phone := PlannedExpense{ID: 3, Amount: 6000, CategoryID: 20, PayerID: 2, Recurring: true, Active: true}
	octOnly := PlannedExpense{ID: 4, Amount: 5000, CategoryID: 30, PayerID: 1, SingleMonth: "2026-10", Active: true}
	inactive := PlannedExpense{ID: 5, Amount: 5000, CategoryID: 30, PayerID: 1, Recurring: true, Active: false}
	entry := func(payer, cat int64, amt Cents) HouseholdEntry {
		return HouseholdEntry{PayerID: payer, CategoryID: cat, Amount: amt, Date: sep.Start().AddDate(0, 0, 2)}
	}
	if id, amb := MatchPlanned(entry(1, 10, 185000), []PlannedExpense{rent, phone}, nil); id != 1 || amb {
		t.Fatalf("simple match: %d %v", id, amb)
	}
	if id, amb := MatchPlanned(entry(1, 10, 185000), []PlannedExpense{rent, rent2}, nil); id != 0 || !amb {
		t.Fatalf("ambiguous: %d %v", id, amb)
	}
	if id, _ := MatchPlanned(entry(1, 10, 185000), []PlannedExpense{rent, rent2}, map[int64]bool{2: true}); id != 1 {
		t.Fatalf("taken should disambiguate: %d", id)
	}
	if id, _ := MatchPlanned(entry(2, 10, 185000), []PlannedExpense{rent}, nil); id != 0 {
		t.Fatal("payer must match")
	}
	if id, _ := MatchPlanned(entry(1, 30, 5000), []PlannedExpense{octOnly, inactive}, nil); id != 0 {
		t.Fatal("single-month and inactive must not apply to September")
	}
	if !octOnly.AppliesTo(Month{2026, time.October}) || octOnly.AppliesTo(sep) || inactive.AppliesTo(sep) || !rent.AppliesTo(sep) {
		t.Fatal("AppliesTo wrong")
	}
}
