package service

import (
	"context"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

func TestLedgerManualEntries(t *testing.T) {
	svc, _ := NewTestService(t)
	ctx := context.Background()
	a := LoginTestUser(t, svc, "alice")
	b := LoginTestUser(t, svc, "bob")
	cat, _ := svc.AddCategory(ctx, "Rent")
	m := domain.Month{Year: 2026, Mon: time.September}
	id, err := svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: a.UserID, Date: m.Start().AddDate(0, 0, 3), Amount: 185000, CategoryID: cat, Note: "rent"})
	if err != nil {
		t.Fatal(err)
	}
	svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: b.UserID, Date: m.Start(), Amount: 2500, CategoryID: cat})
	l, err := svc.Ledger(ctx, m)
	if err != nil || len(l.Lines) != 2 || l.Total != 187500 || l.ByPayer[a.UserID] != 185000 {
		t.Fatalf("ledger %+v %v", l, err)
	}
	if l.Lines[1].Payer != "alice" || l.Lines[1].Category != "Rent" || l.Lines[1].Entry.Source != domain.SourceManual {
		t.Fatalf("line %+v", l.Lines[1])
	}
	if err := svc.UpdateEntryFields(ctx, id, cat, 180000, "negotiated"); err != nil {
		t.Fatal(err)
	}
	l, _ = svc.Ledger(ctx, m)
	if l.Total != 182500 {
		t.Fatalf("total after edit %d", l.Total)
	}
	if err := svc.DeleteEntry(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEntry(ctx, id); err != ErrNotFound {
		t.Fatalf("double delete %v", err)
	}
}
