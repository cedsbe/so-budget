package service

import (
	"context"
	"errors"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/store"
)

type PlannedView struct {
	domain.PlannedExpense
	Category     string
	Payer        string
	MissedMonths int
}

func (s *Service) AddPlanned(ctx context.Context, p domain.PlannedExpense) (int64, error) {
	return s.store.CreatePlanned(ctx, p)
}

func (s *Service) UpdatePlanned(ctx context.Context, p domain.PlannedExpense) error {
	return s.store.UpdatePlanned(ctx, p)
}

func (s *Service) DeletePlanned(ctx context.Context, id int64) error {
	return s.store.DeletePlanned(ctx, id)
}

func (s *Service) PlannedForMonth(ctx context.Context, m domain.Month) ([]domain.PlannedExpense, error) {
	all, err := s.store.ListPlanned(ctx, true)
	if err != nil {
		return nil, err
	}
	var out []domain.PlannedExpense
	for _, p := range all {
		if p.AppliesTo(m) {
			out = append(out, p)
		}
	}
	return out, nil
}

// takenPlanned returns the planned ids already matched by entries in m.
func takenPlanned(entries []domain.HouseholdEntry) map[int64]bool {
	taken := map[int64]bool{}
	for _, e := range entries {
		if e.PlannedID != 0 {
			taken[e.PlannedID] = true
		}
	}
	return taken
}

// matchPlanned sets e.PlannedID when exactly one planned expense fits.
func (s *Service) matchPlanned(ctx context.Context, tx *store.Store, e *domain.HouseholdEntry) error {
	if e.CategoryID == 0 {
		return nil
	}
	m := domain.MonthOf(e.Date)
	candidates, err := tx.ListPlanned(ctx, true)
	if err != nil {
		return err
	}
	entries, err := tx.ListEntries(ctx, m)
	if err != nil {
		return err
	}
	id, _ := domain.MatchPlanned(*e, candidates, takenPlanned(entries))
	e.PlannedID = id
	return nil
}

// MatchEntry links an entry to a planned expense by hand; plannedID 0 unlinks.
func (s *Service) MatchEntry(ctx context.Context, entryID, plannedID int64) error {
	e, err := s.store.Entry(ctx, entryID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if plannedID != 0 {
		if _, err := s.store.Planned(ctx, plannedID); err != nil {
			return ErrNotFound
		}
	}
	e.PlannedID = plannedID
	return s.store.UpdateEntry(ctx, e)
}

// PlannedExpenses lists all planned expenses with display names and the ghost counter.
func (s *Service) PlannedExpenses(ctx context.Context) ([]PlannedView, error) {
	all, err := s.store.ListPlanned(ctx, false)
	if err != nil {
		return nil, err
	}
	cats, err := s.categoryNames(ctx)
	if err != nil {
		return nil, err
	}
	users, err := s.Users(ctx)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, u := range users {
		names[u.ID] = u.Name
	}
	current := domain.MonthOf(s.now())
	closed := []domain.Month{current.Prev(), current.Prev().Prev()}
	matched := map[domain.Month]map[int64]bool{}
	for _, m := range closed {
		entries, err := s.store.ListEntries(ctx, m)
		if err != nil {
			return nil, err
		}
		matched[m] = takenPlanned(entries)
	}
	out := make([]PlannedView, 0, len(all))
	for _, p := range all {
		v := PlannedView{PlannedExpense: p, Category: cats[p.CategoryID], Payer: names[p.PayerID]}
		for _, m := range closed {
			if p.AppliesTo(m) && !matched[m][p.ID] {
				v.MissedMonths++
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) Contributions(ctx context.Context, m domain.Month) (map[int64]domain.Cents, error) {
	users, err := s.Users(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]domain.Cents{}
	for _, u := range users {
		c, err := s.store.ContributionFor(ctx, u.ID, m)
		if err != nil {
			return nil, err
		}
		out[u.ID] = c
	}
	return out, nil
}

func (s *Service) SetContribution(ctx context.Context, userID int64, from domain.Month, amount domain.Cents) error {
	if amount < 0 {
		return ErrInvalidAmount
	}
	return s.store.SetContribution(ctx, userID, from, amount)
}

func (s *Service) ContributionHistory(ctx context.Context, userID int64) ([]domain.Contribution, error) {
	return s.store.ListContributions(ctx, userID)
}
