package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

const sampleCSV = `Payer,Date,Payee,Amount,Description,Type
Cedric,2026-08-01,Landlord,1850,August rent,Rent
Wife,08/03/2026,Sobeys,"84.10",,Groceries
Cedric,2026-08-05,Hydro,64.12,,Utilities
Cedric,not-a-date,Oops,1,,Rent
`

func TestParseImportCSV(t *testing.T) {
	p, err := ParseImportCSV(strings.NewReader(sampleCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 4 || p.Errors != 1 {
		t.Fatalf("rows=%d errors=%d", len(p.Rows), p.Errors)
	}
	if p.Rows[1].Date != time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC) || p.Rows[1].Amount != 8410 {
		t.Fatalf("row 1 %+v", p.Rows[1])
	}
	if strings.Join(p.Payers, ",") != "Cedric,Wife" || strings.Join(p.Types, ",") != "Groceries,Rent,Utilities" {
		t.Fatalf("payers=%v types=%v", p.Payers, p.Types)
	}
	if p.Rows[3].Err == "" {
		t.Fatal("bad date should be flagged")
	}
	if _, err := ParseImportCSV(strings.NewReader("Foo,Bar\n1,2\n")); err == nil {
		t.Fatal("missing columns should error")
	}
}

func TestCommitImport(t *testing.T) {
	svc, _ := NewTestService(t)
	ctx := context.Background()
	a := LoginTestUser(t, svc, "alice")
	b := LoginTestUser(t, svc, "bob")
	rent, _ := svc.AddCategory(ctx, "Rent")
	p, _ := ParseImportCSV(strings.NewReader(sampleCSV))
	n, err := svc.CommitImport(ctx, p, ImportMapping{
		Payers: map[string]int64{"Cedric": a.UserID, "Wife": b.UserID},
		Types:  map[string]int64{"Rent": rent, "Groceries": 0, "Utilities": 0},
	})
	if err != nil || n != 3 {
		t.Fatalf("committed %d %v", n, err)
	}
	l, _ := svc.Ledger(ctx, domain.Month{Year: 2026, Mon: time.August})
	if len(l.Lines) != 3 || l.Total != 185000+8410+6412 {
		t.Fatalf("ledger %+v", l)
	}
	if l.Lines[0].Entry.Source != domain.SourceImport || l.Lines[0].Entry.Note != "Landlord — August rent" || l.Lines[0].Category != "Rent" {
		t.Fatalf("line %+v", l.Lines[0])
	}
	if l.Lines[1].Category != "Groceries" || l.Lines[1].Payer != "bob" {
		t.Fatalf("created category / payer mapping wrong: %+v", l.Lines[1])
	}
	if _, err := svc.CommitImport(ctx, p, ImportMapping{Payers: map[string]int64{"Cedric": a.UserID}, Types: map[string]int64{}}); err == nil {
		t.Fatal("unmapped payer must fail before writing anything")
	}
}
