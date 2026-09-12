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

func TestLedgerPageAndManualEntry(t *testing.T) {
	a := newTestApp(t)
	pw := a.activateUser(t, "alice")
	a.login(t, "alice", pw)
	resp, _ := a.client.Get(a.srv.URL + "/ledger")
	body := readBody(resp)
	csrf := extractCSRF(t, body)

	// Add a category through settings, then a manual entry.
	resp, _ = a.client.PostForm(a.srv.URL+"/settings/categories", url.Values{"csrf": {csrf}, "name": {"Rent"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("category: %d", resp.StatusCode)
	}
	cats, _ := a.svc.Categories(context.Background(), true)
	p := principalOf(t, a)
	month := domain.MonthOf(time.Now())
	resp, _ = a.client.PostForm(a.srv.URL+"/ledger/manual", url.Values{"csrf": {csrf}, "payer": {itoa(p.UserID)}, "date": {month.Start().Format("2006-01-02")}, "amount": {"1850"}, "category": {itoa(cats[0].ID)}, "note": {"rent"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("manual: %d", resp.StatusCode)
	}
	resp, _ = a.client.Get(a.srv.URL + "/ledger?month=" + month.String())
	body = readBody(resp)
	if !strings.Contains(body, "1850.00") || !strings.Contains(body, "Rent") || !strings.Contains(body, "manual") {
		t.Fatalf("ledger page: %s", body)
	}
	l, _ := a.svc.Ledger(context.Background(), month)
	id := l.Lines[0].Entry.ID
	resp, _ = a.client.PostForm(a.srv.URL+"/ledger/"+itoa(id)+"/edit", url.Values{"csrf": {csrf}, "category": {itoa(cats[0].ID)}, "amount": {"1800"}, "note": {"x"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit: %d", resp.StatusCode)
	}
	resp, _ = a.client.PostForm(a.srv.URL+"/ledger/"+itoa(id)+"/delete", url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	l, _ = a.svc.Ledger(context.Background(), month)
	if len(l.Lines) != 0 {
		t.Fatal("entry should be deleted")
	}
}
