package store

import (
	"context"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

func TestPlannedAndContributions(t *testing.T) {
	s := OpenTest(t)
	ctx := context.Background()
	u := newUser(t, s, "a")
	cat, _ := s.CreateCategory(ctx, "Rent")
	id, err := s.CreatePlanned(ctx, domain.PlannedExpense{Name: "Rent", Amount: 185000, CategoryID: cat, PayerID: u, Day: 1, Recurring: true, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	s.CreatePlanned(ctx, domain.PlannedExpense{Name: "Old", Amount: 1, CategoryID: cat, PayerID: u, Day: 1, Recurring: true, Active: false})
	all, _ := s.ListPlanned(ctx, false)
	active, _ := s.ListPlanned(ctx, true)
	if len(all) != 2 || len(active) != 1 || active[0].Name != "Rent" {
		t.Fatalf("all=%d active=%d", len(all), len(active))
	}
	p, _ := s.Planned(ctx, id)
	p.Amount = 190000
	p.SingleMonth = "2026-12"
	p.Recurring = false
	if err := s.UpdatePlanned(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Planned(ctx, id)
	if p.Amount != 190000 || p.SingleMonth != "2026-12" || p.Recurring {
		t.Fatalf("updated %+v", p)
	}
	if err := s.DeletePlanned(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Planned(ctx, id); err != ErrNotFound {
		t.Fatal("should be gone")
	}

	sep := domain.Month{Year: 2026, Mon: time.September}
	if c, _ := s.ContributionFor(ctx, u, sep); c != 0 {
		t.Fatal("no contribution yet")
	}
	s.SetContribution(ctx, u, domain.Month{Year: 2026, Mon: time.July}, 200000)
	s.SetContribution(ctx, u, domain.Month{Year: 2026, Mon: time.October}, 250000)
	s.SetContribution(ctx, u, domain.Month{Year: 2026, Mon: time.October}, 260000) // overwrite same month
	if c, _ := s.ContributionFor(ctx, u, sep); c != 200000 {
		t.Fatalf("sep %d", c)
	}
	if c, _ := s.ContributionFor(ctx, u, domain.Month{Year: 2026, Mon: time.June}); c != 0 {
		t.Fatal("before first setting should be 0")
	}
	if c, _ := s.ContributionFor(ctx, u, domain.Month{Year: 2027, Mon: time.January}); c != 260000 {
		t.Fatal("latest should carry forward")
	}
	hist, _ := s.ListContributions(ctx, u)
	if len(hist) != 2 || hist[0].Month.Mon != time.July {
		t.Fatalf("history %+v", hist)
	}
}
