package simplefin

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
)

// Fake is an in-process SimpleFIN Bridge for tests and local development.
type Fake struct {
	srv      *httptest.Server
	mu       sync.Mutex
	set      AccountSet
	claimed  map[string]bool
	calls    []url.Values
	failNext bool
}

func NewFake() *Fake {
	f := &Fake{claimed: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /claim/{token}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		tok := r.PathValue("token")
		if f.claimed[tok] {
			http.Error(w, "token already claimed", http.StatusForbidden)
			return
		}
		f.claimed[tok] = true
		w.Write([]byte(f.srv.URL + "/simplefin"))
	})
	mux.HandleFunc("GET /simplefin/accounts", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.URL.Query())
		if f.failNext {
			f.failNext = false
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(f.set)
	})
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *Fake) Close()      { f.srv.Close() }
func (f *Fake) URL() string { return f.srv.URL }

// SetupToken returns the Fake's one-time setup token. It is stable across
// calls: claiming it consumes it for the life of the Fake, and claiming it
// a second time fails.
func (f *Fake) SetupToken() string {
	return base64.StdEncoding.EncodeToString([]byte(f.srv.URL + "/claim/tok"))
}

func (f *Fake) Set(set AccountSet) {
	f.mu.Lock()
	f.set = set
	f.mu.Unlock()
}

func (f *Fake) FailNext() {
	f.mu.Lock()
	f.failNext = true
	f.mu.Unlock()
}

func (f *Fake) Calls() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.calls...)
}

// SampleSet is realistic-looking data for local development.
func SampleSet(now int64) AccountSet {
	day := int64(86400)
	return AccountSet{Accounts: []Account{
		{ID: "chq", Name: "TD Chequing", Currency: "CAD", Balance: "2450.10", BalanceDate: now, Org: Org{Name: "TD Canada Trust"},
			Transactions: []Transaction{
				{ID: "c1", Posted: now - 1*day, Amount: "-1850.00", Description: "RENT E-TRANSFER"},
				{ID: "c2", Posted: now - 2*day, Amount: "-500.00", Description: "TFR-TO C/C"},
				{ID: "c3", Posted: now - 5*day, Amount: "2900.00", Description: "PAYROLL DEPOSIT"},
				{ID: "c4", Posted: now - 6*day, Amount: "-64.12", Description: "HYDRO ONE"},
			}},
		{ID: "cc", Name: "TD Visa", Currency: "CAD", Balance: "-312.44", BalanceDate: now, Org: Org{Name: "TD Canada Trust"},
			Transactions: []Transaction{
				{ID: "v1", Posted: now - 1*day, Amount: "-87.30", Description: "SOBEYS #1234 TORONTO"},
				{ID: "v2", Posted: now - 2*day, Amount: "500.00", Description: "PAYMENT - THANK YOU"},
				{ID: "v3", Posted: now - 3*day, Amount: "-15.99", Description: "NETFLIX.COM"},
				{ID: "v4", Posted: now - 4*day, Amount: "-42.00", Description: "TIM HORTONS #77"},
			}},
	}}
}
