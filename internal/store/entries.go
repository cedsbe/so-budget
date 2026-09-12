package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
)

const dateLayout = "2006-01-02"

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Store) CreateEntry(ctx context.Context, e domain.HouseholdEntry) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.q.ExecContext(ctx, `INSERT INTO household_entries
		(payer_user_id, date, amount, category_id, note, source, source_ref, planned_expense_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.PayerID, e.Date.Format(dateLayout), e.Amount, nullID(e.CategoryID), e.Note, e.Source, nullStr(e.SourceRef), nullID(e.PlannedID), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const entryCols = `id, payer_user_id, date, amount, COALESCE(category_id,0), note, source, COALESCE(source_ref,''), COALESCE(planned_expense_id,0)`

func scanEntry(sc interface{ Scan(...any) error }) (domain.HouseholdEntry, error) {
	var e domain.HouseholdEntry
	var date string
	err := sc.Scan(&e.ID, &e.PayerID, &date, &e.Amount, &e.CategoryID, &e.Note, &e.Source, &e.SourceRef, &e.PlannedID)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	e.Date, err = time.Parse(dateLayout, date)
	return e, err
}

func (s *Store) Entry(ctx context.Context, id int64) (domain.HouseholdEntry, error) {
	return scanEntry(s.q.QueryRowContext(ctx, `SELECT `+entryCols+` FROM household_entries WHERE id=?`, id))
}

func (s *Store) EntryBySourceRef(ctx context.Context, payerID int64, ref string) (domain.HouseholdEntry, error) {
	return scanEntry(s.q.QueryRowContext(ctx, `SELECT `+entryCols+` FROM household_entries WHERE payer_user_id=? AND source_ref=?`, payerID, ref))
}

func (s *Store) UpdateEntry(ctx context.Context, e domain.HouseholdEntry) error {
	return s.execOne(ctx, `UPDATE household_entries SET payer_user_id=?, date=?, amount=?, category_id=?, note=?, planned_expense_id=?, updated_at=? WHERE id=?`,
		e.PayerID, e.Date.Format(dateLayout), e.Amount, nullID(e.CategoryID), e.Note, nullID(e.PlannedID), time.Now().UTC().Format(time.RFC3339), e.ID)
}

func (s *Store) DeleteEntry(ctx context.Context, id int64) error {
	return s.execOne(ctx, `DELETE FROM household_entries WHERE id=?`, id)
}

func (s *Store) ListEntries(ctx context.Context, m domain.Month) ([]domain.HouseholdEntry, error) {
	return s.ListEntriesBetween(ctx, m.Start(), m.End())
}

func (s *Store) ListEntriesBetween(ctx context.Context, from, to time.Time) ([]domain.HouseholdEntry, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+entryCols+` FROM household_entries WHERE date >= ? AND date < ? ORDER BY date, id`,
		from.Format(dateLayout), to.Format(dateLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HouseholdEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
