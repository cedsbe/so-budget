package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/service"
	"github.com/cedsbe/so-budget/internal/simplefin"
)

func sampleSet() simplefin.AccountSet {
	now := time.Now().Unix()
	return simplefin.AccountSet{Accounts: []simplefin.Account{{
		ID: "chq", Name: "Chequing", Currency: "CAD", Balance: "10.00", BalanceDate: now, Org: simplefin.Org{Name: "TD"},
		Transactions: []simplefin.Transaction{
			{ID: "t1", Posted: now, Amount: "-43.21", Description: "SOBEYS #1234"},
			{ID: "t2", Posted: now - 3600, Amount: "-12.00", Description: "TIM HORTONS"},
		},
	}}}
}

func (a *testApp) postHX(t *testing.T, path string, form url.Values, csrf string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, a.srv.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestInboxLinkFlagAndThrottle(t *testing.T) {
	a := newTestApp(t)
	a.fake.Set(sampleSet())
	pw := a.activateUser(t, "alice")
	a.login(t, "alice", pw)

	// Not linked yet: inbox says so.
	resp, _ := a.client.Get(a.srv.URL + "/")
	body := readBody(resp)
	if !strings.Contains(body, "Settings") || strings.Contains(body, "SOBEYS") {
		t.Fatalf("unlinked inbox: %s", body)
	}
	csrf := extractCSRF(t, body)

	// Link via settings.
	resp, _ = a.client.PostForm(a.srv.URL+"/settings/link", url.Values{"csrf": {csrf}, "token": {a.fake.SetupToken()}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("link: %d", resp.StatusCode)
	}
	resp, _ = a.client.Get(a.srv.URL + "/")
	body = readBody(resp)
	if !strings.Contains(body, "SOBEYS #1234") || !strings.Contains(body, "TIM HORTONS") {
		t.Fatalf("inbox after link: %s", body)
	}
	calls := len(a.fake.Calls())

	// Throttle: another page load within the hour does not hit SimpleFIN.
	resp, _ = a.client.Get(a.srv.URL + "/")
	resp.Body.Close()
	if len(a.fake.Calls()) != calls {
		t.Fatal("page load should not sync again within an hour")
	}
	// Sync now respects the same limit.
	resp, _ = a.client.PostForm(a.srv.URL+"/sync", url.Values{"csrf": {csrf}})
	resp.Body.Close()
	if len(a.fake.Calls()) != calls {
		t.Fatal("sync now should be throttled")
	}
	// Age the session's last sync: now it syncs.
	u, _ := url.Parse(a.srv.URL)
	for _, c := range a.client.Jar.Cookies(u) {
		if c.Name == "sb_session" {
			sess, _ := a.server.sessions.Get(c.Value)
			sess.LastSync = time.Now().Add(-2 * time.Hour)
		}
	}
	resp, _ = a.client.Get(a.srv.URL + "/")
	resp.Body.Close()
	if len(a.fake.Calls()) != calls+1 {
		t.Fatal("stale session should sync on page load")
	}

	// Flag SOBEYS with a category via htmx.
	cat, _ := a.svc.AddCategory(context.Background(), "Groceries")
	inbox, _ := a.svc.Inbox(context.Background(), principalOf(t, a))
	var hash string
	for _, it := range inbox.Items {
		if it.Tx.Payee == "SOBEYS #1234" {
			hash = it.IDHash
		}
	}
	resp = a.postHX(t, "/tx/"+hash+"/flag", url.Values{"category": {itoa(cat)}, "amount": {"40"}, "note": {"partial"}}, csrf)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("flag: %d", resp.StatusCode)
	}
	resp, _ = a.client.Get(a.srv.URL + "/")
	if body = readBody(resp); strings.Contains(body, "SOBEYS") {
		t.Fatal("flagged row should be gone")
	}
	entries, _ := a.svc.Ledger(context.Background(), domain.MonthOf(time.Now()))
	if len(entries.Lines) != 1 || entries.Lines[0].Entry.Amount != 4000 {
		t.Fatalf("ledger %+v", entries)
	}

	// History shows both, one flagged.
	resp, _ = a.client.Get(a.srv.URL + "/history")
	body = readBody(resp)
	if !strings.Contains(body, "SOBEYS") || !strings.Contains(body, "TIM HORTONS") || !strings.Contains(body, "flagged") {
		t.Fatalf("history: %s", body)
	}

	// Rule from transaction: always dismiss TIM HORTONS.
	inbox, _ = a.svc.Inbox(context.Background(), principalOf(t, a))
	resp = a.postHX(t, "/tx/"+inbox.Items[0].IDHash+"/rule/private", nil, csrf)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("private rule: %d", resp.StatusCode)
	}
	rules, _ := a.svc.PrivateRules(context.Background(), principalOf(t, a))
	if len(rules) != 1 || rules[0].Pattern != "TIM HORTONS" {
		t.Fatalf("rules %+v", rules)
	}
}

// principalOf returns the logged-in principal of the test client's session.
func principalOf(t *testing.T, a *testApp) service.Principal {
	t.Helper()
	u, _ := url.Parse(a.srv.URL)
	for _, c := range a.client.Jar.Cookies(u) {
		if c.Name == "sb_session" {
			if sess, ok := a.server.sessions.Get(c.Value); ok {
				return sess.P
			}
		}
	}
	t.Fatal("no session")
	return service.Principal{}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
