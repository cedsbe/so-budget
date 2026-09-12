package web

import (
	"net/http"
	"net/url"
	"sync"
	"testing"
)

// TestConcurrentRequestsOnSameSession fires many concurrent htmx-style requests
// against one session cookie. It exists as a probe for the race detector (run
// with `go test -race`): Session used to expose Flash/LastSync/SyncErrors/Import
// as plain fields mutated with no lock, so concurrent page loads could race.
func TestConcurrentRequestsOnSameSession(t *testing.T) {
	a := newTestApp(t)
	a.fake.Set(sampleSet())
	pw := a.activateUser(t, "alice")
	a.login(t, "alice", pw)

	resp, _ := a.client.Get(a.srv.URL + "/")
	body := readBody(resp)
	csrf := extractCSRF(t, body)
	resp, _ = a.client.PostForm(a.srv.URL+"/settings/link", url.Values{"csrf": {csrf}, "token": {a.fake.SetupToken()}})
	resp.Body.Close()

	const n = 12
	var wg sync.WaitGroup
	statuses := make([]int, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := a.client.Get(a.srv.URL + "/")
			if err != nil {
				errs[i] = err
				return
			}
			resp.Body.Close()
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	for i, code := range statuses {
		if code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, code)
		}
	}
}
