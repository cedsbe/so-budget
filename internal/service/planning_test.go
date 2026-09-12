package service

import (
	"context"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

func TestPlanningMatchingAndGhosts(t *testing.T) {
	svc, _ := NewTestService(t)
	svc.now = func() time.Time { return fixedNow } // 2026-09-11
	ctx := context.Background()
	a := LoginTestUser(t, svc, "alice")
	rent, _ := svc.AddCategory(ctx, "Rent")
	gym, _ := svc.AddCategory(ctx, "Gym")
	rentID, _ := svc.AddPlanned(ctx, domain.PlannedExpense{Name: "Rent", Amount: 185000, CategoryID: rent, PayerID: a.UserID, Day: 1, Recurring: true, Active: true})
	gymID, _ := svc.AddPlanned(ctx, domain.PlannedExpense{Name: "Gym", Amount: 4000, CategoryID: gym, PayerID: a.UserID, Day: 15, Recurring: true, Active: true})

	sep := domain.Month{Year: 2026, Mon: time.September}
	id, _ := svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: a.UserID, Date: sep.Start(), Amount: 185000, CategoryID: rent})
	e, _ := svc.store.Entry(ctx, id)
	if e.PlannedID != rentID {
		t.Fatalf("auto match failed: %+v", e)
	}
	// A second rent-like entry in the same month does not match (already taken).
	id2, _ := svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: a.UserID, Date: sep.Start().AddDate(0, 0, 2), Amount: 185000, CategoryID: rent})
	if e2, _ := svc.store.Entry(ctx, id2); e2.PlannedID != 0 {
		t.Fatal("planned expense must match only once per month")
	}
	// Manual match and unmatch.
	if err := svc.MatchEntry(ctx, id2, gymID); err != nil {
		t.Fatal(err)
	}
	if e2, _ := svc.store.Entry(ctx, id2); e2.PlannedID != gymID {
		t.Fatal("manual match failed")
	}
	svc.MatchEntry(ctx, id2, 0)
	if e2, _ := svc.store.Entry(ctx, id2); e2.PlannedID != 0 {
		t.Fatal("unmatch failed")
	}

	// Ghost detection: gym never matched in July or August (closed months) → MissedMonths == 2; rent matched in neither either.
	svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: a.UserID, Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Amount: 185000, CategoryID: rent})
	views, err := svc.PlannedExpenses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]PlannedView{}
	for _, v := range views {
		byName[v.Name] = v
	}
	if byName["Gym"].MissedMonths != 2 || byName["Rent"].MissedMonths != 1 || byName["Rent"].Category != "Rent" || byName["Rent"].Payer != "alice" {
		t.Fatalf("views %+v", byName)
	}

	// Contributions carry forward.
	svc.SetContribution(ctx, a.UserID, domain.Month{Year: 2026, Mon: time.August}, 200000)
	c, _ := svc.Contributions(ctx, sep)
	if c[a.UserID] != 200000 {
		t.Fatalf("contributions %+v", c)
	}
	planned, _ := svc.PlannedForMonth(ctx, sep)
	if len(planned) != 2 {
		t.Fatalf("planned for month %d", len(planned))
	}
}
