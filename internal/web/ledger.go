package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/service"
)

func (s *Server) routesLedger() {
	s.private("GET /ledger", s.ledger)
	s.private("POST /ledger/manual", s.ledgerManual)
	s.private("POST /ledger/{id}/edit", s.ledgerEdit)
	s.private("POST /ledger/{id}/delete", s.ledgerDelete)
	s.private("POST /settings/categories", s.categoryAdd)
	s.private("POST /settings/categories/{id}", s.categoryUpdate)
}

type ledgerData struct {
	service.Ledger
	Prev, Next domain.Month
	Users      []domain.User
	Categories []domain.Category
	Today      string
}

func (s *Server) ledger(w http.ResponseWriter, r *http.Request) {
	m := monthParam(r)
	l, err := s.svc.Ledger(r.Context(), m)
	if err != nil {
		httpError(w, err)
		return
	}
	users, _ := s.svc.Users(r.Context())
	cats, _ := s.svc.Categories(r.Context(), true)
	s.render(w, r, "ledger.html", ledgerData{Ledger: l, Prev: m.Prev(), Next: m.Next(), Users: users, Categories: cats, Today: time.Now().Format("2006-01-02")})
}

func (s *Server) ledgerManual(w http.ResponseWriter, r *http.Request) {
	date, err := time.Parse("2006-01-02", r.FormValue("date"))
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	amount, err := domain.ParseCents(r.FormValue("amount"))
	if err != nil {
		http.Error(w, "invalid amount", http.StatusBadRequest)
		return
	}
	e := domain.HouseholdEntry{PayerID: formID(r, "payer"), Date: date, Amount: amount, CategoryID: formID(r, "category"), Note: r.FormValue("note")}
	if e.PayerID == 0 || e.CategoryID == 0 {
		http.Error(w, "payer and category required", http.StatusBadRequest)
		return
	}
	if _, err := s.svc.AddManualEntry(r.Context(), e); err != nil {
		httpError(w, err)
		return
	}
	s.flash(r, "Entry added.")
	http.Redirect(w, r, "/ledger?month="+domain.MonthOf(date).String(), http.StatusSeeOther)
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (s *Server) ledgerEdit(w http.ResponseWriter, r *http.Request) {
	amount, err := domain.ParseCents(r.FormValue("amount"))
	if err != nil {
		http.Error(w, "invalid amount", http.StatusBadRequest)
		return
	}
	if err := s.svc.UpdateEntryFields(r.Context(), pathID(r), formID(r, "category"), amount, r.FormValue("note")); err != nil {
		httpError(w, err)
		return
	}
	done(w, r, "/ledger")
}

func (s *Server) ledgerDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteEntry(r.Context(), pathID(r)); err != nil {
		httpError(w, err)
		return
	}
	done(w, r, "/ledger")
}

func (s *Server) categoryAdd(w http.ResponseWriter, r *http.Request) {
	if _, err := s.svc.AddCategory(r.Context(), r.FormValue("name")); err != nil {
		s.renderSettings(w, r, "Could not add category: "+err.Error())
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) categoryUpdate(w http.ResponseWriter, r *http.Request) {
	c := domain.Category{ID: pathID(r), Name: r.FormValue("name"), Active: r.FormValue("active") != ""}
	if err := s.svc.UpdateCategory(r.Context(), c); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
