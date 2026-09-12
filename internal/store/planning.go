package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/cedsbe/so-budget/internal/domain"
)

const plannedCols = `id, name, amount, category_id, payer_user_id, day_of_month, recurring, COALESCE(single_month,''), active, COALESCE(created_at,'')`

func scanPlanned(sc interface{ Scan(...any) error }) (domain.PlannedExpense, error) {
	var p domain.PlannedExpense
	err := sc.Scan(&p.ID, &p.Name, &p.Amount, &p.CategoryID, &p.PayerID, &p.Day, &p.Recurring, &p.SingleMonth, &p.Active, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// CreatePlanned inserts p as given, including CreatedAt: the service is responsible
// for stamping CreatedAt from its clock before calling this when the caller didn't
// already set one, so the store never touches the wall clock itself.
func (s *Store) CreatePlanned(ctx context.Context, p domain.PlannedExpense) (int64, error) {
	res, err := s.q.ExecContext(ctx, `INSERT INTO planned_expenses(name, amount, category_id, payer_user_id, day_of_month, recurring, single_month, active, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.Name, p.Amount, p.CategoryID, p.PayerID, p.Day, p.Recurring, nullStr(p.SingleMonth), p.Active, nullStr(p.CreatedAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdatePlanned(ctx context.Context, p domain.PlannedExpense) error {
	return s.execOne(ctx, `UPDATE planned_expenses SET name=?, amount=?, category_id=?, payer_user_id=?, day_of_month=?, recurring=?, single_month=?, active=? WHERE id=?`,
		p.Name, p.Amount, p.CategoryID, p.PayerID, p.Day, p.Recurring, nullStr(p.SingleMonth), p.Active, p.ID)
}

func (s *Store) DeletePlanned(ctx context.Context, id int64) error {
	if _, err := s.q.ExecContext(ctx, `UPDATE household_entries SET planned_expense_id=NULL WHERE planned_expense_id=?`, id); err != nil {
		return err
	}
	return s.execOne(ctx, `DELETE FROM planned_expenses WHERE id=?`, id)
}

func (s *Store) Planned(ctx context.Context, id int64) (domain.PlannedExpense, error) {
	return scanPlanned(s.q.QueryRowContext(ctx, `SELECT `+plannedCols+` FROM planned_expenses WHERE id=?`, id))
}

func (s *Store) ListPlanned(ctx context.Context, activeOnly bool) ([]domain.PlannedExpense, error) {
	q := `SELECT ` + plannedCols + ` FROM planned_expenses`
	if activeOnly {
		q += ` WHERE active=1`
	}
	rows, err := s.q.QueryContext(ctx, q+` ORDER BY day_of_month, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PlannedExpense
	for rows.Next() {
		p, err := scanPlanned(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetContribution(ctx context.Context, userID int64, m domain.Month, amount domain.Cents) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO contributions(user_id, month, amount) VALUES (?, ?, ?)
		ON CONFLICT(user_id, month) DO UPDATE SET amount=excluded.amount`, userID, m.String(), amount)
	return err
}

// ContributionFor returns the amount effective for m: the latest setting at or before m.
func (s *Store) ContributionFor(ctx context.Context, userID int64, m domain.Month) (domain.Cents, error) {
	var c domain.Cents
	err := s.q.QueryRowContext(ctx, `SELECT amount FROM contributions WHERE user_id=? AND month<=? ORDER BY month DESC LIMIT 1`, userID, m.String()).Scan(&c)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return c, err
}

func (s *Store) ListContributions(ctx context.Context, userID int64) ([]domain.Contribution, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT month, amount FROM contributions WHERE user_id=? ORDER BY month`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Contribution
	for rows.Next() {
		var ms string
		c := domain.Contribution{UserID: userID}
		if err := rows.Scan(&ms, &c.Amount); err != nil {
			return nil, err
		}
		if c.Month, err = domain.ParseMonth(ms); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
