package service

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

func TestMonthReportAndExport(t *testing.T) {
	svc, _ := NewTestService(t)
	svc.now = func() time.Time { return fixedNow }
	ctx := context.Background()
	a := LoginTestUser(t, svc, "alice")
	b := LoginTestUser(t, svc, "bob")
	rent, _ := svc.AddCategory(ctx, "Rent")
	food, _ := svc.AddCategory(ctx, "Groceries")
	sep := domain.Month{Year: 2026, Mon: time.September}
	aug := sep.Prev()
	svc.SetContribution(ctx, a.UserID, aug, 200000)
	svc.SetContribution(ctx, b.UserID, aug, 150000)
	svc.AddPlanned(ctx, domain.PlannedExpense{Name: "Rent", Amount: 185000, CategoryID: rent, PayerID: a.UserID, Day: 1, Recurring: true, Active: true})
	svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: a.UserID, Date: sep.Start(), Amount: 185000, CategoryID: rent})
	svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: b.UserID, Date: sep.Start().AddDate(0, 0, 4), Amount: 12000, CategoryID: food, Note: "sobeys"})
	svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: b.UserID, Date: aug.Start().AddDate(0, 0, 4), Amount: 9000, CategoryID: food})

	r, err := svc.MonthReport(ctx, sep)
	if err != nil {
		t.Fatal(err)
	}
	if r.Margin != 350000-197000 || len(r.People) != 2 || r.People[1].Spent != 12000 {
		t.Fatalf("report %+v", r.MonthReport)
	}
	if len(r.Categories) != 2 || r.Categories[0].Name != "Rent" || r.Categories[1].This != 12000 || r.Categories[1].Previous != 9000 {
		t.Fatalf("categories %+v", r.Categories)
	}
	if len(r.Trend) != 6 || r.Trend[5].Month != sep || r.Trend[5].Total != 197000 || r.Trend[4].Total != 9000 || r.Trend[5].ByUser[b.UserID] != 12000 {
		t.Fatalf("trend %+v", r.Trend)
	}

	var buf bytes.Buffer
	if err := svc.ExportCSV(ctx, sep, &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 || lines[0] != "date,payer,category,amount,note,source" || !strings.Contains(lines[2], "bob,Groceries,120.00,sobeys,manual") {
		t.Fatalf("csv %q", buf.String())
	}
}
