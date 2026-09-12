package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/service"
)

func (s *Server) routesPlanning() {
	s.private("GET /planning", s.planning)
	s.private("POST /planning/contribution", s.contributionSet)
	s.private("POST /planning/planned", s.plannedAdd)
	s.private("POST /planning/planned/{id}/edit", s.plannedEdit)
	s.private("POST /planning/planned/{id}/delete", s.plannedDelete)
	s.private("POST /ledger/{id}/match", s.ledgerMatch)
}

type planningData struct {
	Month         domain.Month
	Users         []domain.User
	Contributions map[int64]domain.Cents
	Planned       []service.PlannedView
	Categories    []domain.Category
	Error         string
}

func (s *Server) planning(w http.ResponseWriter, r *http.Request) {
	s.renderPlanning(w, r, "")
}

func (s *Server) renderPlanning(w http.ResponseWriter, r *http.Request, errMsg string) {
	ctx := r.Context()
	m := domain.MonthOf(time.Now())
	d := planningData{Month: m, Error: errMsg}
	var err error
	if d.Users, err = s.svc.Users(ctx); err != nil {
		httpError(w, err)
		return
	}
	if d.Contributions, err = s.svc.Contributions(ctx, m); err != nil {
		httpError(w, err)
		return
	}
	if d.Planned, err = s.svc.PlannedExpenses(ctx); err != nil {
		httpError(w, err)
		return
	}
	d.Categories, _ = s.svc.Categories(ctx, true)
	s.render(w, r, "planning.html", d)
}

func (s *Server) contributionSet(w http.ResponseWriter, r *http.Request) {
	m, err := domain.ParseMonth(r.FormValue("month"))
	if err != nil {
		s.renderPlanning(w, r, "Invalid month.")
		return
	}
	amount, err := domain.ParseCents(r.FormValue("amount"))
	if err != nil {
		s.renderPlanning(w, r, "Invalid amount.")
		return
	}
	if err := s.svc.SetContribution(r.Context(), formID(r, "user"), m, amount); err != nil {
		if errors.Is(err, service.ErrInvalidAmount) {
			s.renderPlanning(w, r, "Contribution cannot be negative.")
			return
		}
		httpError(w, err)
		return
	}
	s.flash(r, "Contribution saved from "+m.String()+" onward.")
	http.Redirect(w, r, "/planning", http.StatusSeeOther)
}

func plannedFromForm(r *http.Request) (domain.PlannedExpense, string) {
	amount, err := domain.ParseCents(r.FormValue("amount"))
	if err != nil || amount <= 0 {
		return domain.PlannedExpense{}, "Amount must be a positive number."
	}
	day, _ := strconv.Atoi(r.FormValue("day"))
	if day < 1 || day > 31 {
		return domain.PlannedExpense{}, "Day must be between 1 and 31."
	}
	p := domain.PlannedExpense{
		Name: r.FormValue("name"), Amount: amount, CategoryID: formID(r, "category"), PayerID: formID(r, "payer"), Day: day,
		Recurring: r.FormValue("recurring") != "", SingleMonth: r.FormValue("single_month"), Active: r.FormValue("active") != "",
	}
	if p.Name == "" || p.CategoryID == 0 || p.PayerID == 0 {
		return p, "Name, category and payer are required."
	}
	if !p.Recurring {
		if _, err := domain.ParseMonth(p.SingleMonth); err != nil {
			return p, "A one-off expense needs a month."
		}
	} else {
		p.SingleMonth = ""
	}
	return p, ""
}

func (s *Server) plannedAdd(w http.ResponseWriter, r *http.Request) {
	p, msg := plannedFromForm(r)
	if msg != "" {
		s.renderPlanning(w, r, msg)
		return
	}
	p.Active = true
	if _, err := s.svc.AddPlanned(r.Context(), p); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/planning", http.StatusSeeOther)
}

func (s *Server) plannedEdit(w http.ResponseWriter, r *http.Request) {
	p, msg := plannedFromForm(r)
	if msg != "" {
		s.renderPlanning(w, r, msg)
		return
	}
	p.ID = pathID(r)
	if err := s.svc.UpdatePlanned(r.Context(), p); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/planning", http.StatusSeeOther)
}

func (s *Server) plannedDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeletePlanned(r.Context(), pathID(r)); err != nil {
		httpError(w, err)
		return
	}
	http.Redirect(w, r, "/planning", http.StatusSeeOther)
}

func (s *Server) ledgerMatch(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.MatchEntry(r.Context(), pathID(r), formID(r, "planned")); err != nil {
		httpError(w, err)
		return
	}
	done(w, r, "/ledger")
}
