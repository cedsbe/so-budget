package service

import (
	"context"
	"errors"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/store"
)

type LedgerLine struct {
	Entry    domain.HouseholdEntry
	Payer    string
	Category string
}

type Ledger struct {
	Month   domain.Month
	Lines   []LedgerLine
	Total   domain.Cents
	ByPayer map[int64]domain.Cents
}

func (s *Service) Ledger(ctx context.Context, m domain.Month) (Ledger, error) {
	out := Ledger{Month: m, ByPayer: map[int64]domain.Cents{}}
	entries, err := s.store.ListEntries(ctx, m)
	if err != nil {
		return out, err
	}
	cats, err := s.categoryNames(ctx)
	if err != nil {
		return out, err
	}
	users, err := s.Users(ctx)
	if err != nil {
		return out, err
	}
	names := map[int64]string{}
	for _, u := range users {
		names[u.ID] = u.Name
	}
	for _, e := range entries {
		out.Lines = append(out.Lines, LedgerLine{Entry: e, Payer: names[e.PayerID], Category: cats[e.CategoryID]})
		out.Total += e.Amount
		out.ByPayer[e.PayerID] += e.Amount
	}
	return out, nil
}

func (s *Service) AddManualEntry(ctx context.Context, e domain.HouseholdEntry) (int64, error) {
	e.Source = domain.SourceManual
	e.SourceRef = ""
	var id int64
	err := s.store.WithTx(ctx, func(tx *store.Store) error {
		if err := s.matchPlanned(ctx, tx, &e); err != nil {
			return err
		}
		var err error
		id, err = tx.CreateEntry(ctx, e)
		return err
	})
	return id, err
}

// UpdateEntryFields edits the shared fields of any entry. Amount edits on synced
// entries do not touch the source transaction.
func (s *Service) UpdateEntryFields(ctx context.Context, id int64, categoryID int64, amount domain.Cents, note string) error {
	e, err := s.store.Entry(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	e.CategoryID, e.Amount, e.Note = categoryID, amount, note
	return s.store.UpdateEntry(ctx, e)
}
