package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

func TestImportFlow(t *testing.T) {
	a := newTestApp(t)
	pw := a.activateUser(t, "alice")
	a.login(t, "alice", pw)
	resp, _ := a.client.Get(a.srv.URL + "/settings/import")
	body := readBody(resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "file") {
		t.Fatalf("import page: %d", resp.StatusCode)
	}
	csrf := extractCSRF(t, body)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("csrf", csrf)
	fw, _ := mw.CreateFormFile("file", "history.csv")
	fw.Write([]byte("Payer,Date,Payee,Amount,Description,Type\nMe,2026-08-01,Landlord,1850,,Rent\n"))
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, a.srv.URL+"/settings/import/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, _ = a.client.Do(req)
	body = readBody(resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Landlord") || !strings.Contains(body, `name="payer.Me"`) {
		t.Fatalf("preview: %d %s", resp.StatusCode, body)
	}
	p := principalOf(t, a)
	resp, _ = a.client.PostForm(a.srv.URL+"/settings/import/commit", url.Values{"csrf": {csrf}, "payer.Me": {itoa(p.UserID)}, "type.Rent": {"0"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("commit: %d", resp.StatusCode)
	}
	l, _ := a.svc.Ledger(context.Background(), domain.Month{Year: 2026, Mon: time.August})
	if len(l.Lines) != 1 || l.Lines[0].Category != "Rent" {
		t.Fatalf("ledger %+v", l)
	}
}
