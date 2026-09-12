package service

import (
	"context"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/domain"
)

type ruleBlob struct {
	Pattern string
}

func (s *Service) PrivateRules(ctx context.Context, p Principal) ([]domain.PrivateRule, error) {
	rows, err := s.store.ListPrivateRules(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.PrivateRule, 0, len(rows))
	for _, r := range rows {
		b, err := open[ruleBlob](p.Key, "private_rules", r.ID, r.Blob)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.PrivateRule{ID: r.ID, Pattern: b.Pattern})
	}
	return out, nil
}

func (s *Service) AddPrivateRule(ctx context.Context, p Principal, pattern string) error {
	id, err := crypto.RandomToken(8)
	if err != nil {
		return err
	}
	blob, err := seal(p.Key, "private_rules", id, ruleBlob{Pattern: pattern})
	if err != nil {
		return err
	}
	return s.store.InsertPrivateRule(ctx, p.UserID, id, blob)
}

func (s *Service) DeletePrivateRule(ctx context.Context, p Principal, id string) error {
	return s.store.DeletePrivateRule(ctx, p.UserID, id)
}

func (s *Service) HouseholdRules(ctx context.Context) ([]domain.HouseholdRule, error) {
	return s.store.ListHouseholdRules(ctx)
}

func (s *Service) AddHouseholdRule(ctx context.Context, r domain.HouseholdRule) (int64, error) {
	return s.store.CreateHouseholdRule(ctx, r)
}

func (s *Service) UpdateHouseholdRule(ctx context.Context, r domain.HouseholdRule) error {
	return s.store.UpdateHouseholdRule(ctx, r)
}

func (s *Service) DeleteHouseholdRule(ctx context.Context, id int64) error {
	return s.store.DeleteHouseholdRule(ctx, id)
}

func (s *Service) Categories(ctx context.Context, activeOnly bool) ([]domain.Category, error) {
	return s.store.ListCategories(ctx, activeOnly)
}

func (s *Service) AddCategory(ctx context.Context, name string) (int64, error) {
	return s.store.CreateCategory(ctx, name)
}

func (s *Service) UpdateCategory(ctx context.Context, c domain.Category) error {
	return s.store.UpdateCategory(ctx, c)
}

// categoryNames is a lookup used when decorating results for display.
func (s *Service) categoryNames(ctx context.Context) (map[int64]string, error) {
	cats, err := s.store.ListCategories(ctx, false)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(cats))
	for _, c := range cats {
		m[c.ID] = c.Name
	}
	return m, nil
}
