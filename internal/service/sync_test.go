package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/simplefin"
)

var fixedNow = time.Date(2026, 9, 11, 15, 0, 0, 0, time.UTC)

func fixtureSet() simplefin.AccountSet {
	d := int64(86400)
	now := fixedNow.Unix()
	return simplefin.AccountSet{Accounts: []simplefin.Account{
		{ID: "chq", Name: "Chequing", Currency: "CAD", Balance: "100.00", BalanceDate: now, Org: simplefin.Org{Name: "TD"},
			Transactions: []simplefin.Transaction{
				{ID: "c1", Posted: now - 1*d, Amount: "-43.21", Description: "SOBEYS #1234"},
				{ID: "c2", Posted: now - 2*d, Amount: "-500.00", Description: "TFR-TO C/C"},
				{ID: "c3", Posted: now - 3*d, Amount: "-9.99", Description: "PENDING", Pending: true},
			}},
		{ID: "cc", Name: "Visa", Currency: "CAD", Balance: "-50.00", BalanceDate: now, Org: simplefin.Org{Name: "TD"},
			Transactions: []simplefin.Transaction{
				{ID: "v1", Posted: now - 2*d, Amount: "500.00", Description: "PAYMENT - THANK YOU"},
				{ID: "v2", Posted: now - 4*d, Amount: "-15.99", Description: "NETFLIX.COM"},
			}},
	}}
}

func TestLinkAndSync(t *testing.T) {
	svc, fake := NewTestService(t)
	svc.now = func() time.Time { return fixedNow }
	fake.Set(fixtureSet())
	ctx := context.Background()
	p := LoginTestUser(t, svc, "alice")

	if linked, _ := svc.Linked(ctx, p); linked {
		t.Fatal("not linked yet")
	}
	if _, err := svc.Sync(ctx, p); err != ErrNotLinked {
		t.Fatalf("sync before link: %v", err)
	}
	if err := svc.LinkSimpleFIN(ctx, p, fake.SetupToken()); err != nil {
		t.Fatal(err)
	}
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("link should sync once, got %d calls", len(calls))
	}
	wantStart := fixedNow.AddDate(0, 0, -90).Unix()
	if calls[0].Get("start-date") != strconv.FormatInt(wantStart, 10) {
		t.Fatalf("first pull start %s want %d", calls[0].Get("start-date"), wantStart)
	}
	accts, _ := svc.Accounts(ctx, p)
	if len(accts) != 2 {
		t.Fatalf("accounts %+v", accts)
	}
	for _, a := range accts {
		if a.Name == "Chequing" && (a.Balance != 10000 || a.Institution != "TD") {
			t.Fatalf("chequing %+v", a)
		}
	}
	inbox, _ := svc.store.ListTransactions(ctx, p.UserID, storeFilter("new"))
	dismissed, _ := svc.store.ListTransactions(ctx, p.UserID, storeFilter("dismissed"))
	if len(inbox) != 2 || len(dismissed) != 2 { // c1,v2 new; c2,v1 dismissed (transfer pair via card payment + transfer detection); pending skipped
		t.Fatalf("new=%d dismissed=%d", len(inbox), len(dismissed))
	}

	// Second sync: overlap window, no duplicates.
	res, err := svc.Sync(ctx, p)
	if err != nil || res.New != 0 {
		t.Fatalf("second sync %+v %v", res, err)
	}
	calls = fake.Calls()
	last, _, _ := svc.LastSync(ctx, p)
	wantStart = last.AddDate(0, 0, -7).Unix()
	if calls[1].Get("start-date") != strconv.FormatInt(wantStart, 10) {
		t.Fatalf("overlap start %s want %d", calls[1].Get("start-date"), wantStart)
	}

	// Errors are surfaced, sync still succeeds.
	set := fixtureSet()
	set.Errors = []string{"TD needs re-auth"}
	fake.Set(set)
	res, err = svc.Sync(ctx, p)
	if err != nil || len(res.Errors) != 1 {
		t.Fatalf("errors %+v %v", res, err)
	}

	// Failure leaves last sync untouched.
	before, _, _ := svc.LastSync(ctx, p)
	fake.FailNext()
	if _, err := svc.Sync(ctx, p); err == nil {
		t.Fatal("expected failure")
	}
	after, _, _ := svc.LastSync(ctx, p)
	if !before.Equal(after) {
		t.Fatal("failed sync must not advance last sync")
	}

	// Start-date override wins when later than the window.
	svc.SetSyncStart(ctx, p, "2026-09-10")
	svc.Sync(ctx, p)
	calls = fake.Calls()
	if got := calls[len(calls)-1].Get("start-date"); got != strconv.FormatInt(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC).Unix(), 10) {
		t.Fatalf("override start %s", got)
	}
}

func TestSyncAppliesRules(t *testing.T) {
	svc, fake := NewTestService(t)
	svc.now = func() time.Time { return fixedNow }
	fake.Set(fixtureSet())
	ctx := context.Background()
	p := LoginTestUser(t, svc, "alice")
	cat, _ := svc.AddCategory(ctx, "Groceries")
	svc.AddHouseholdRule(ctx, domain.HouseholdRule{Pattern: "sobeys", CategoryID: cat, SuggestHousehold: true})
	svc.AddPrivateRule(ctx, p, "netflix")
	if err := svc.LinkSimpleFIN(ctx, p, fake.SetupToken()); err != nil {
		t.Fatal(err)
	}
	rows, _ := svc.store.ListTransactions(ctx, p.UserID, storeFilter("new"))
	if len(rows) != 1 {
		t.Fatalf("want only sobeys in inbox, got %d", len(rows))
	}
	tx, err := open[txBlob](p.Key, "transactions", rows[0].IDHash, rows[0].Blob)
	if err != nil || tx.SuggestedCategoryID != cat || !tx.SuggestHousehold || tx.Payee != "SOBEYS #1234" {
		t.Fatalf("suggestion %+v %v", tx, err)
	}
	rules, _ := svc.PrivateRules(ctx, p)
	if len(rules) != 1 || rules[0].Pattern != "netflix" {
		t.Fatalf("private rules %+v", rules)
	}
}
