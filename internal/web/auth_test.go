package web

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/service"
)

type testApp struct {
	svc    *service.Service
	server *Server
	srv    *httptest.Server
	client *http.Client
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	svc, _ := service.NewTestService(t)
	s, err := New(svc, Config{BaseURL: "http://example", Dev: true, IdleTimeout: 30 * time.Minute, AbsoluteTimeout: 12 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &testApp{svc: svc, server: s, srv: srv, client: client}
}

func testServer(t *testing.T, a *testApp) *Server { return a.server }

// activateUser runs invite+activate through the service and returns the password.
func (a *testApp) activateUser(t *testing.T, name string) string {
	t.Helper()
	tok, err := a.svc.Invite(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Activate(context.Background(), tok, "password-"+name); err != nil {
		t.Fatal(err)
	}
	return "password-" + name
}

func (a *testApp) login(t *testing.T, name, pw string) {
	t.Helper()
	resp, err := a.client.PostForm(a.srv.URL+"/login", url.Values{"name": {name}, "password": {pw}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("login status %d", resp.StatusCode)
	}
}

func TestPrivateRoutesRedirectWithoutSession(t *testing.T) {
	a := newTestApp(t)
	resp, _ := a.client.Get(a.srv.URL + "/")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login" {
		t.Fatalf("got %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func TestLoginFlow(t *testing.T) {
	a := newTestApp(t)
	pw := a.activateUser(t, "alice")

	resp, _ := a.client.PostForm(a.srv.URL+"/login", url.Values{"name": {"alice"}, "password": {"wrong"}})
	body := readBody(resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Invalid") {
		t.Fatalf("wrong password: %d %s", resp.StatusCode, body)
	}

	a.login(t, "alice", pw)
	resp, _ = a.client.Get(a.srv.URL + "/")
	body = readBody(resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "alice") {
		t.Fatalf("home: %d %s", resp.StatusCode, body)
	}

	// CSRF: a POST without the token is rejected.
	resp, _ = a.client.PostForm(a.srv.URL+"/logout", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without csrf: %d", resp.StatusCode)
	}
	csrf := extractCSRF(t, body)
	resp, _ = a.client.PostForm(a.srv.URL+"/logout", url.Values{"csrf": {csrf}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	resp, _ = a.client.Get(a.srv.URL + "/")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatal("session should be gone")
	}
}

func TestInviteFlow(t *testing.T) {
	a := newTestApp(t)
	tok, _ := a.svc.Invite(context.Background(), "bob")
	resp, _ := a.client.Get(a.srv.URL + "/invite/" + tok)
	if body := readBody(resp); resp.StatusCode != http.StatusOK || !strings.Contains(body, "bob") {
		t.Fatalf("invite page: %d %s", resp.StatusCode, body)
	}
	resp, _ = a.client.PostForm(a.srv.URL+"/invite/"+tok, url.Values{"password": {"a long enough password"}, "confirm": {"a long enough password"}})
	body := readBody(resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "recovery") {
		t.Fatalf("activate: %d %s", resp.StatusCode, body)
	}
	resp, _ = a.client.Get(a.srv.URL + "/invite/" + tok)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("used invite should 404, got %d", resp.StatusCode)
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	a := newTestApp(t)
	pw := a.activateUser(t, "alice")
	a.login(t, "alice", pw)
	// Reach into the server: find the session and age it.
	u, _ := url.Parse(a.srv.URL)
	for _, c := range a.client.Jar.Cookies(u) {
		if c.Name == "sb_session" {
			sess, ok := testServer(t, a).sessions.Get(c.Value)
			if !ok {
				t.Fatal("session missing")
			}
			sess.LastSeen = time.Now().Add(-time.Hour)
		}
	}
	resp, _ := a.client.Get(a.srv.URL + "/")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expired session should redirect, got %d", resp.StatusCode)
	}
}

func readBody(resp *http.Response) string {
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func extractCSRF(t *testing.T, body string) string {
	t.Helper()
	const marker = `name="csrf" value="`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatal("no csrf field in page")
	}
	rest := body[i+len(marker):]
	return rest[:strings.Index(rest, `"`)]
}
