package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/service"
)

func (s *Server) routesInbox() {
	s.private("GET /{$}", s.inbox)
	s.private("POST /inbox/accept-all", s.acceptAll)
	s.private("POST /inbox/reapply", s.reapply)
	s.private("POST /sync", s.syncNow)
	s.private("POST /tx/{hash}/flag", s.txFlag)
	s.private("POST /tx/{hash}/dismiss", s.txAction(s.svc.Dismiss))
	s.private("POST /tx/{hash}/restore", s.txAction(s.svc.Restore))
	s.private("POST /tx/{hash}/unflag", s.txAction(s.svc.Unflag))
	s.private("POST /tx/{hash}/edit", s.txEdit)
	s.private("POST /tx/{hash}/rule/private", s.txRulePrivate)
	s.private("POST /tx/{hash}/rule/household", s.txRuleHousehold)
	s.private("GET /history", s.history)
	s.private("POST /history", s.historySearch)
	s.private("GET /settings", s.settings)
	s.private("POST /settings/link", s.settingsLink)
	s.private("POST /settings/start", s.settingsStart)
	s.private("POST /settings/account", s.settingsAccount)
	s.private("POST /settings/password", s.settingsPassword)
	s.private("POST /settings/rules/private", s.rulePrivateAdd)
	s.private("POST /settings/rules/private/{id}/delete", s.rulePrivateDelete)
	s.private("POST /settings/rules/household", s.ruleHouseholdAdd)
	s.private("POST /settings/rules/household/{id}/delete", s.ruleHouseholdDelete)
}

const syncInterval = time.Hour

// maybeSync runs a sync for the session if it is linked and the last one is older than syncInterval.
func (s *Server) maybeSync(r *http.Request, sess *Session) bool {
	if !sess.ShouldSync(syncInterval) {
		return false
	}
	linked, err := s.svc.Linked(r.Context(), sess.P)
	if err != nil || !linked {
		return false
	}
	res, err := s.svc.Sync(r.Context(), sess.P)
	if err != nil {
		sess.SetSyncErrors([]string{"Sync failed: " + err.Error()})
		return true
	}
	sess.SetSyncErrors(res.Errors)
	return true
}

// done finishes an htmx action: 204 for htmx (row removed client-side), redirect otherwise.
func done(w http.ResponseWriter, r *http.Request, fallback string) {
	if r.Header.Get("HX-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if ref := r.Referer(); ref != "" {
		fallback = ref
	}
	http.Redirect(w, r, fallback, http.StatusSeeOther)
}

type inboxData struct {
	service.Inbox
	Categories []domain.Category
	SyncErrors []string
}

func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	sess := s.session(r)
	s.maybeSync(r, sess)
	inbox, err := s.svc.Inbox(r.Context(), sess.P)
	if err != nil {
		httpError(w, err)
		return
	}
	cats, err := s.svc.Categories(r.Context(), true)
	if err != nil {
		httpError(w, err)
		return
	}
	s.render(w, r, "inbox.html", inboxData{Inbox: inbox, Categories: cats, SyncErrors: sess.SyncErrors()})
}

func (s *Server) syncNow(w http.ResponseWriter, r *http.Request) {
	sess := s.session(r)
	if s.maybeSync(r, sess) {
		s.flash(r, "Synced.")
	} else {
		s.flash(r, "Already synced within the last hour; the bank data only refreshes daily.")
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) acceptAll(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.AcceptAllSuggestions(r.Context(), s.session(r).P)
	if err != nil {
		httpError(w, err)
		return
	}
	s.flash(r, strconv.Itoa(n)+" transaction(s) flagged.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) reapply(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.ReapplyRules(r.Context(), s.session(r).P); err != nil {
		httpError(w, err)
		return
	}
	s.flash(r, "Rules re-applied.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func formCents(r *http.Request, field string) (domain.Cents, error) {
	v := r.FormValue(field)
	if v == "" {
		return 0, nil
	}
	return domain.ParseCents(v)
}

func formID(r *http.Request, field string) int64 {
	id, _ := strconv.ParseInt(r.FormValue(field), 10, 64)
	return id
}

func (s *Server) txFlag(w http.ResponseWriter, r *http.Request) {
	amount, err := formCents(r, "amount")
	if err != nil {
		http.Error(w, "invalid amount", http.StatusBadRequest)
		return
	}
	if _, err := s.svc.Flag(r.Context(), s.session(r).P, r.PathValue("hash"), formID(r, "category"), amount, r.FormValue("note")); err != nil {
		httpError(w, err)
		return
	}
	done(w, r, "/")
}

func (s *Server) txEdit(w http.ResponseWriter, r *http.Request) {
	amount, err := formCents(r, "amount")
	if err != nil {
		http.Error(w, "invalid amount", http.StatusBadRequest)
		return
	}
	if err := s.svc.EditFlagged(r.Context(), s.session(r).P, r.PathValue("hash"), formID(r, "category"), amount, r.FormValue("note")); err != nil {
		httpError(w, err)
		return
	}
	s.flash(r, "Updated.")
	http.Redirect(w, r, "/history", http.StatusSeeOther)
}

func (s *Server) txAction(fn func(ctx context.Context, p service.Principal, hash string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(r.Context(), s.session(r).P, r.PathValue("hash")); err != nil {
			httpError(w, err)
			return
		}
		done(w, r, "/")
	}
}

// payeeOf finds the payee text of one of the principal's transactions by scanning the inbox and current month history.
func (s *Server) payeeOf(r *http.Request, hash string) (string, error) {
	sess := s.session(r)
	inbox, err := s.svc.Inbox(r.Context(), sess.P)
	if err != nil {
		return "", err
	}
	for _, it := range inbox.Items {
		if it.IDHash == hash {
			return it.Tx.Payee, nil
		}
	}
	items, err := s.svc.History(r.Context(), sess.P, domain.MonthOf(time.Now()), "")
	if err != nil {
		return "", err
	}
	for _, it := range items {
		if it.IDHash == hash {
			return it.Tx.Payee, nil
		}
	}
	return "", service.ErrNotFound
}

func (s *Server) txRulePrivate(w http.ResponseWriter, r *http.Request) {
	payee, err := s.payeeOf(r, r.PathValue("hash"))
	if err != nil {
		httpError(w, err)
		return
	}
	p := s.session(r).P
	if err := s.svc.AddPrivateRule(r.Context(), p, payee); err != nil {
		httpError(w, err)
		return
	}
	_ = s.svc.Dismiss(r.Context(), p, r.PathValue("hash"))
	_ = s.svc.ReapplyRules(r.Context(), p)
	done(w, r, "/")
}

func (s *Server) txRuleHousehold(w http.ResponseWriter, r *http.Request) {
	payee, err := s.payeeOf(r, r.PathValue("hash"))
	if err != nil {
		httpError(w, err)
		return
	}
	cat := formID(r, "category")
	if cat == 0 {
		http.Error(w, "category required", http.StatusBadRequest)
		return
	}
	if _, err := s.svc.AddHouseholdRule(r.Context(), domain.HouseholdRule{Pattern: payee, CategoryID: cat, SuggestHousehold: r.FormValue("household") != ""}); err != nil {
		httpError(w, err)
		return
	}
	_ = s.svc.ReapplyRules(r.Context(), s.session(r).P)
	s.flash(r, "Rule added.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type historyData struct {
	Month      domain.Month
	Query      string
	Items      []service.HistoryItem
	Categories []domain.Category
	CatNames   map[int64]string
}

func monthParam(r *http.Request) domain.Month {
	if m, err := domain.ParseMonth(r.URL.Query().Get("month")); err == nil {
		return m
	}
	return domain.MonthOf(time.Now())
}

// history serves GET /history?month= for plain navigation. Any ?q= is ignored on
// purpose: a payee in a URL would reach proxy logs and browser history.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	s.renderHistory(w, r, monthParam(r), "")
}

// historySearch serves POST /history so a payee search term never lands in a URL
// (proxy logs, browser history), reading month and q from the form body instead.
func (s *Server) historySearch(w http.ResponseWriter, r *http.Request) {
	m, err := domain.ParseMonth(r.FormValue("month"))
	if err != nil {
		m = domain.MonthOf(time.Now())
	}
	s.renderHistory(w, r, m, r.FormValue("q"))
}

func (s *Server) renderHistory(w http.ResponseWriter, r *http.Request, m domain.Month, q string) {
	sess := s.session(r)
	s.maybeSync(r, sess)
	items, err := s.svc.History(r.Context(), sess.P, m, q)
	if err != nil {
		httpError(w, err)
		return
	}
	cats, _ := s.svc.Categories(r.Context(), true)
	names := map[int64]string{}
	for _, c := range cats {
		names[c.ID] = c.Name
	}
	s.render(w, r, "history.html", historyData{Month: m, Query: q, Items: items, Categories: cats, CatNames: names})
}

type settingsData struct {
	Linked         bool
	Accounts       []domain.BankAccount
	StartDate      string
	PrivateRules   []domain.PrivateRule
	HouseholdRules []domain.HouseholdRule
	Categories     []domain.Category
	CatNames       map[int64]string
	Error          string
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "")
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, errMsg string) {
	ctx := r.Context()
	p := s.session(r).P
	d := settingsData{Error: errMsg, CatNames: map[int64]string{}}
	var err error
	if d.Linked, err = s.svc.Linked(ctx, p); err != nil {
		httpError(w, err)
		return
	}
	if d.Linked {
		d.Accounts, _ = s.svc.Accounts(ctx, p)
		d.StartDate, _ = s.svc.SyncStart(ctx, p)
	}
	d.PrivateRules, _ = s.svc.PrivateRules(ctx, p)
	d.HouseholdRules, _ = s.svc.HouseholdRules(ctx)
	d.Categories, _ = s.svc.Categories(ctx, false)
	for _, c := range d.Categories {
		d.CatNames[c.ID] = c.Name
	}
	s.render(w, r, "settings.html", d)
}

func (s *Server) settingsLink(w http.ResponseWriter, r *http.Request) {
	sess := s.session(r)
	if err := s.svc.LinkSimpleFIN(r.Context(), sess.P, r.FormValue("token")); err != nil {
		s.renderSettings(w, r, "Could not link: "+err.Error())
		return
	}
	sess.MarkSynced()
	s.flash(r, "Bank linked and first sync done.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) settingsStart(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.SetSyncStart(r.Context(), s.session(r).P, r.FormValue("date")); err != nil {
		s.renderSettings(w, r, err.Error())
		return
	}
	s.flash(r, "Sync start date saved.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) settingsAccount(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.RenameAccount(r.Context(), s.session(r).P, r.FormValue("account_id"), r.FormValue("name")); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) settingsPassword(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("new") != r.FormValue("confirm") {
		s.renderSettings(w, r, "New passwords do not match.")
		return
	}
	if err := s.svc.ChangePassword(r.Context(), s.session(r).P, r.FormValue("old"), r.FormValue("new")); err != nil {
		s.renderSettings(w, r, err.Error())
		return
	}
	s.flash(r, "Password changed.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) rulePrivateAdd(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.AddPrivateRule(r.Context(), s.session(r).P, r.FormValue("pattern")); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) rulePrivateDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeletePrivateRule(r.Context(), s.session(r).P, r.PathValue("id")); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) ruleHouseholdAdd(w http.ResponseWriter, r *http.Request) {
	rule := domain.HouseholdRule{Pattern: r.FormValue("pattern"), CategoryID: formID(r, "category"), SuggestHousehold: r.FormValue("household") != ""}
	if rule.Pattern == "" || rule.CategoryID == 0 {
		s.renderSettings(w, r, "Pattern and category are required.")
		return
	}
	if _, err := s.svc.AddHouseholdRule(r.Context(), rule); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) ruleHouseholdDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := s.svc.DeleteHouseholdRule(r.Context(), id); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
