package web

import (
	"net/http"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/service"
)

func (s *Server) routesReports() {
	s.private("GET /reports", s.reports)
	s.private("GET /reports/export.csv", s.reportsExport)
}

type reportsData struct {
	service.Report
	Prev, Next domain.Month
	MaxCat     domain.Cents
	MaxTrend   domain.Cents
}

func (s *Server) reports(w http.ResponseWriter, r *http.Request) {
	m := monthParam(r)
	rep, err := s.svc.MonthReport(r.Context(), m)
	if err != nil {
		httpError(w, err)
		return
	}
	d := reportsData{Report: rep, Prev: m.Prev(), Next: m.Next(), MaxCat: 1, MaxTrend: 1}
	for _, c := range rep.Categories {
		d.MaxCat = max(d.MaxCat, c.This, c.Previous)
	}
	for _, t := range rep.Trend {
		d.MaxTrend = max(d.MaxTrend, t.Total)
	}
	s.render(w, r, "reports.html", d)
}

func (s *Server) reportsExport(w http.ResponseWriter, r *http.Request) {
	m := monthParam(r)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ledger-`+m.String()+`.csv"`)
	if err := s.svc.ExportCSV(r.Context(), m, w); err != nil {
		httpError(w, err)
	}
}
