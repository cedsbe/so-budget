package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/cedsbe/so-budget/internal/domain"
)

func (s *Store) CreateCategory(ctx context.Context, name string) (int64, error) {
	res, err := s.q.ExecContext(ctx, `INSERT INTO categories(name) VALUES (?)`, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListCategories(ctx context.Context, activeOnly bool) ([]domain.Category, error) {
	q := `SELECT id, name, active FROM categories`
	if activeOnly {
		q += ` WHERE active=1`
	}
	rows, err := s.q.QueryContext(ctx, q+` ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Category
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Active); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CategoryByName(ctx context.Context, name string) (domain.Category, error) {
	var c domain.Category
	err := s.q.QueryRowContext(ctx, `SELECT id, name, active FROM categories WHERE name=? COLLATE NOCASE`, name).Scan(&c.ID, &c.Name, &c.Active)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) UpdateCategory(ctx context.Context, c domain.Category) error {
	return s.execOne(ctx, `UPDATE categories SET name=?, active=? WHERE id=?`, c.Name, c.Active, c.ID)
}

func (s *Store) CreateHouseholdRule(ctx context.Context, r domain.HouseholdRule) (int64, error) {
	res, err := s.q.ExecContext(ctx, `INSERT INTO household_rules(pattern, category_id, suggest_household) VALUES (?, ?, ?)`,
		r.Pattern, r.CategoryID, r.SuggestHousehold)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListHouseholdRules(ctx context.Context) ([]domain.HouseholdRule, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id, pattern, category_id, suggest_household FROM household_rules ORDER BY pattern COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HouseholdRule
	for rows.Next() {
		var r domain.HouseholdRule
		if err := rows.Scan(&r.ID, &r.Pattern, &r.CategoryID, &r.SuggestHousehold); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpdateHouseholdRule(ctx context.Context, r domain.HouseholdRule) error {
	return s.execOne(ctx, `UPDATE household_rules SET pattern=?, category_id=?, suggest_household=? WHERE id=?`,
		r.Pattern, r.CategoryID, r.SuggestHousehold, r.ID)
}

func (s *Store) DeleteHouseholdRule(ctx context.Context, id int64) error {
	return s.execOne(ctx, `DELETE FROM household_rules WHERE id=?`, id)
}
