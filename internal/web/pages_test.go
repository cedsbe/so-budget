package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

// TestAllPagesRender seeds one of everything through the service and hits every
// page-rendering route, so a broken template shows up here as a test failure
// instead of a truncated page in production.
func TestAllPagesRender(t *testing.T) {
	a := newTestApp(t)
	a.fake.Set(sampleSet())
	pw := a.activateUser(t, "alice")
	a.login(t, "alice", pw)

	resp, _ := a.client.Get(a.srv.URL + "/")
	body := readBody(resp)
	csrf := extractCSRF(t, body)

	resp, _ = a.client.PostForm(a.srv.URL+"/settings/link", url.Values{"csrf": {csrf}, "token": {a.fake.SetupToken()}})
	resp.Body.Close()

	ctx := context.Background()
	p := principalOf(t, a)

	cat, err := a.svc.AddCategory(ctx, "Groceries")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.svc.SetContribution(ctx, p.UserID, domain.MonthOf(time.Now()), 100000); err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.AddPlanned(ctx, domain.PlannedExpense{Name: "Rent", Amount: 185000, CategoryID: cat, PayerID: p.UserID, Day: 1, Recurring: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.AddManualEntry(ctx, domain.HouseholdEntry{PayerID: p.UserID, Date: time.Now(), Amount: 2000, CategoryID: cat, Note: "cash"}); err != nil {
		t.Fatal(err)
	}

	inbox, err := a.svc.Inbox(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox.Items) == 0 {
		t.Fatal("expected at least one inbox item to flag")
	}
	if _, err := a.svc.Flag(ctx, p, inbox.Items[0].IDHash, cat, 0, "flagged"); err != nil {
		t.Fatal(err)
	}

	htmlPages := []string{
		"/", "/history", "/ledger", "/ledger?category=" + itoa(cat), "/planning",
		"/reports", "/settings", "/settings/import",
	}
	for _, path := range htmlPages {
		resp, err := a.client.Get(a.srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		got := readBody(resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, resp.StatusCode, got)
		}
		if !strings.Contains(got, "</html>") {
			t.Fatalf("%s: response missing </html>: %s", path, got)
		}
	}

	csvResp, err := a.client.Get(a.srv.URL + "/reports/export.csv")
	if err != nil {
		t.Fatal(err)
	}
	csvBody := readBody(csvResp)
	if csvResp.StatusCode != http.StatusOK {
		t.Fatalf("export.csv: status %d: %s", csvResp.StatusCode, csvBody)
	}
	header, _, _ := strings.Cut(csvBody, "\n")
	if header != "date,payer,category,amount,note,source" {
		t.Fatalf("export.csv header: %q", header)
	}
}
