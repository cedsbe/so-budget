package service

import (
	"context"
	"encoding/csv"
	"io"
	"sort"

	"github.com/cedsbe/so-budget/internal/domain"
)

type CategoryTotal struct {
	CategoryID int64
	Name       string
	This       domain.Cents
	Previous   domain.Cents
}

type TrendPoint struct {
	Month  domain.Month
	Total  domain.Cents
	ByUser map[int64]domain.Cents
}

type Report struct {
	domain.MonthReport
	Categories []CategoryTotal
	Trend      []TrendPoint
	Users      []domain.User
}

func (s *Service) computeMonth(ctx context.Context, m domain.Month, users []domain.User) (domain.MonthReport, []domain.HouseholdEntry, error) {
	contribs, err := s.Contributions(ctx, m)
	if err != nil {
		return domain.MonthReport{}, nil, err
	}
	entries, err := s.store.ListEntries(ctx, m)
	if err != nil {
		return domain.MonthReport{}, nil, err
	}
	planned, err := s.PlannedForMonth(ctx, m)
	if err != nil {
		return domain.MonthReport{}, nil, err
	}
	return domain.ComputeMonth(m, s.now(), users, contribs, entries, planned), entries, nil
}

func (s *Service) MonthReport(ctx context.Context, m domain.Month) (Report, error) {
	users, err := s.Users(ctx)
	if err != nil {
		return Report{}, err
	}
	r := Report{Users: users}
	var entries []domain.HouseholdEntry
	if r.MonthReport, entries, err = s.computeMonth(ctx, m, users); err != nil {
		return r, err
	}
	prevEntries, err := s.store.ListEntries(ctx, m.Prev())
	if err != nil {
		return r, err
	}
	cats, err := s.categoryNames(ctx)
	if err != nil {
		return r, err
	}
	totals := map[int64]*CategoryTotal{}
	get := func(id int64) *CategoryTotal {
		if t, ok := totals[id]; ok {
			return t
		}
		t := &CategoryTotal{CategoryID: id, Name: cats[id]}
		if t.Name == "" {
			t.Name = "(none)"
		}
		totals[id] = t
		return t
	}
	for _, e := range entries {
		get(e.CategoryID).This += e.Amount
	}
	for _, e := range prevEntries {
		get(e.CategoryID).Previous += e.Amount
	}
	for _, t := range totals {
		r.Categories = append(r.Categories, *t)
	}
	sort.Slice(r.Categories, func(i, j int) bool { return r.Categories[i].This > r.Categories[j].This })

	months := make([]domain.Month, 6)
	for i, mm := 5, m; i >= 0; i, mm = i-1, mm.Prev() {
		months[i] = mm
	}
	for _, mm := range months {
		var pt TrendPoint
		pt.Month = mm
		pt.ByUser = map[int64]domain.Cents{}
		var es []domain.HouseholdEntry
		if mm == m {
			es = entries
		} else if es, err = s.store.ListEntries(ctx, mm); err != nil {
			return r, err
		}
		for _, e := range es {
			pt.Total += e.Amount
			pt.ByUser[e.PayerID] += e.Amount
		}
		r.Trend = append(r.Trend, pt)
	}
	return r, nil
}

func (s *Service) ExportCSV(ctx context.Context, m domain.Month, w io.Writer) error {
	l, err := s.Ledger(ctx, m)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Write([]string{"date", "payer", "category", "amount", "note", "source"})
	for _, line := range l.Lines {
		cw.Write([]string{line.Entry.Date.Format(dateLayout), line.Payer, line.Category, line.Entry.Amount.String(), line.Entry.Note, line.Entry.Source})
	}
	cw.Flush()
	return cw.Error()
}
