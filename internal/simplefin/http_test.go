package simplefin

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestClaimAndAccountsAgainstFake(t *testing.T) {
	f := NewFake()
	defer f.Close()
	f.Set(AccountSet{
		Errors: []string{"Connection to TD needs re-authentication"},
		Accounts: []Account{{
			ID: "acc-1", Name: "Chequing", Currency: "CAD", Balance: "1234.56", BalanceDate: 1757548800,
			Org: Org{Name: "TD"},
			Transactions: []Transaction{
				{ID: "t1", Posted: 1757462400, Amount: "-43.21", Description: "SOBEYS #1234"},
				{ID: "t2", Posted: 1757462400, Amount: "-9.99", Description: "PENDING THING", Pending: true},
			},
		}},
	})
	c := NewHTTPClient()
	ctx := context.Background()
	access, err := c.Claim(ctx, f.SetupToken())
	if err != nil {
		t.Fatal(err)
	}
	if access != f.URL()+"/simplefin" {
		t.Fatalf("access url %q", access)
	}
	if _, err := c.Claim(ctx, f.SetupToken()); err == nil {
		t.Fatal("second claim of the same token must fail")
	}
	start := time.Unix(1700000000, 0)
	end := time.Unix(1757548800, 0)
	set, err := c.Accounts(ctx, access, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Accounts) != 1 || len(set.Accounts[0].Transactions) != 2 || set.Errors[0] == "" {
		t.Fatalf("got %+v", set)
	}
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Get("start-date") != strconv.FormatInt(start.Unix(), 10) || calls[0].Get("pending") != "0" {
		t.Fatalf("query %v", calls)
	}
	f.FailNext()
	if _, err := c.Accounts(ctx, access, start, end); err == nil {
		t.Fatal("expected error from failing server")
	}
	if _, err := c.Claim(ctx, "not base64!!"); err == nil {
		t.Fatal("bad token should error")
	}
}
