package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/store"
)

type InboxItem struct {
	IDHash            string
	Tx                domain.Transaction
	AccountName       string
	SuggestedCategory string
}

type Inbox struct {
	Items       []InboxItem
	Linked      bool
	LastSync    time.Time
	HasLastSync bool
}

type HistoryItem struct {
	IDHash      string
	Tx          domain.Transaction
	AccountName string
	Entry       *domain.HouseholdEntry
}

func (s *Service) loadTx(ctx context.Context, p Principal, idHash string) (txBlob, domain.TxState, error) {
	row, err := s.store.Transaction(ctx, p.UserID, idHash)
	if errors.Is(err, store.ErrNotFound) {
		return txBlob{}, "", ErrNotFound
	}
	if err != nil {
		return txBlob{}, "", err
	}
	b, err := open[txBlob](p.Key, "transactions", idHash, row.Blob)
	return b, domain.TxState(row.State), err
}

func (s *Service) Inbox(ctx context.Context, p Principal) (Inbox, error) {
	var out Inbox
	linked, err := s.Linked(ctx, p)
	if err != nil {
		return out, err
	}
	out.Linked = linked
	out.LastSync, out.HasLastSync, err = s.store.LastSync(ctx, p.UserID)
	if err != nil {
		return out, err
	}
	rows, err := s.store.ListTransactions(ctx, p.UserID, store.TxFilter{State: string(domain.TxNew)})
	if err != nil {
		return out, err
	}
	names, err := s.accountNames(ctx, p)
	if err != nil {
		return out, err
	}
	cats, err := s.categoryNames(ctx)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		b, err := open[txBlob](p.Key, "transactions", r.IDHash, r.Blob)
		if err != nil {
			return out, err
		}
		tx := b.toDomain(domain.TxNew)
		out.Items = append(out.Items, InboxItem{IDHash: r.IDHash, Tx: tx, AccountName: names[tx.AccountID], SuggestedCategory: cats[tx.SuggestedCategoryID]})
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		as, bs := a.Tx.SuggestedCategoryID != 0, b.Tx.SuggestedCategoryID != 0
		if as != bs {
			return !as // unsuggested first
		}
		return a.Tx.Posted.After(b.Tx.Posted)
	})
	return out, nil
}

// entryAmount validates a requested amount against the transaction and returns
// the ledger amount (positive = spent).
func entryAmount(tx domain.Cents, requested domain.Cents) (domain.Cents, error) {
	full := -tx
	if requested == 0 {
		return full, nil
	}
	if full == 0 || (full > 0) != (requested > 0) || requested.Abs() > full.Abs() {
		return 0, ErrInvalidAmount
	}
	return requested, nil
}

func (s *Service) Flag(ctx context.Context, p Principal, idHash string, categoryID int64, amount domain.Cents, note string) (int64, error) {
	b, state, err := s.loadTx(ctx, p, idHash)
	if err != nil {
		return 0, err
	}
	if state == domain.TxFlagged {
		return 0, ErrInvalidState
	}
	amt, err := entryAmount(b.Amount, amount)
	if err != nil {
		return 0, err
	}
	posted, _ := time.Parse(dateLayout, b.Posted)
	e := domain.HouseholdEntry{PayerID: p.UserID, Date: posted, Amount: amt, CategoryID: categoryID, Note: note, Source: domain.SourceSync, SourceRef: idHash}
	var id int64
	err = s.store.WithTx(ctx, func(tx *store.Store) error {
		if err := s.matchPlanned(ctx, tx, &e); err != nil {
			return err
		}
		var err error
		if id, err = tx.CreateEntry(ctx, e); err != nil {
			return err
		}
		return tx.SetTransactionState(ctx, p.UserID, idHash, string(domain.TxFlagged))
	})
	return id, err
}

func (s *Service) EditFlagged(ctx context.Context, p Principal, idHash string, categoryID int64, amount domain.Cents, note string) error {
	b, state, err := s.loadTx(ctx, p, idHash)
	if err != nil {
		return err
	}
	if state != domain.TxFlagged {
		return ErrInvalidState
	}
	amt, err := entryAmount(b.Amount, amount)
	if err != nil {
		return err
	}
	e, err := s.store.EntryBySourceRef(ctx, p.UserID, idHash)
	if err != nil {
		return err
	}
	e.CategoryID, e.Amount, e.Note = categoryID, amt, note
	return s.store.UpdateEntry(ctx, e)
}

func (s *Service) setState(ctx context.Context, p Principal, idHash string, from []domain.TxState, to domain.TxState) error {
	_, state, err := s.loadTx(ctx, p, idHash)
	if err != nil {
		return err
	}
	ok := false
	for _, f := range from {
		ok = ok || f == state
	}
	if !ok {
		return ErrInvalidState
	}
	return s.store.SetTransactionState(ctx, p.UserID, idHash, string(to))
}

func (s *Service) Dismiss(ctx context.Context, p Principal, idHash string) error {
	return s.setState(ctx, p, idHash, []domain.TxState{domain.TxNew}, domain.TxDismissed)
}

func (s *Service) Restore(ctx context.Context, p Principal, idHash string) error {
	return s.setState(ctx, p, idHash, []domain.TxState{domain.TxDismissed}, domain.TxNew)
}

func (s *Service) Unflag(ctx context.Context, p Principal, idHash string) error {
	_, state, err := s.loadTx(ctx, p, idHash)
	if err != nil {
		return err
	}
	if state != domain.TxFlagged {
		return ErrInvalidState
	}
	return s.store.WithTx(ctx, func(tx *store.Store) error {
		e, err := tx.EntryBySourceRef(ctx, p.UserID, idHash)
		if err == nil {
			if err := tx.DeleteEntry(ctx, e.ID); err != nil {
				return err
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return tx.SetTransactionState(ctx, p.UserID, idHash, string(domain.TxNew))
	})
}

// DeleteEntry removes a ledger entry. Either user may call it; a synced entry's
// source transaction goes back to its owner's inbox.
func (s *Service) DeleteEntry(ctx context.Context, id int64) error {
	e, err := s.store.Entry(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteEntry(ctx, id); err != nil {
			return err
		}
		if e.Source == domain.SourceSync && e.SourceRef != "" {
			if err := tx.SetTransactionState(ctx, e.PayerID, e.SourceRef, string(domain.TxNew)); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		return nil
	})
}

// AcceptAllSuggestions flags every inbox item that has both a suggested category and a household suggestion.
func (s *Service) AcceptAllSuggestions(ctx context.Context, p Principal) (int, error) {
	inbox, err := s.Inbox(ctx, p)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range inbox.Items {
		if it.Tx.SuggestedCategoryID == 0 || !it.Tx.SuggestHousehold {
			continue
		}
		if _, err := s.Flag(ctx, p, it.IDHash, it.Tx.SuggestedCategoryID, 0, ""); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ReapplyRules re-evaluates rules over every `new` transaction.
func (s *Service) ReapplyRules(ctx context.Context, p Principal) error {
	hrules, err := s.store.ListHouseholdRules(ctx)
	if err != nil {
		return err
	}
	prules, err := s.PrivateRules(ctx, p)
	if err != nil {
		return err
	}
	rows, err := s.store.ListTransactions(ctx, p.UserID, store.TxFilter{State: string(domain.TxNew)})
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx *store.Store) error {
		for _, r := range rows {
			b, err := open[txBlob](p.Key, "transactions", r.IDHash, r.Blob)
			if err != nil {
				return err
			}
			sug := domain.Evaluate(b.Payee, prules, hrules)
			b.SuggestedCategoryID, b.SuggestHousehold = sug.CategoryID, sug.Household
			blob, err := seal(p.Key, "transactions", r.IDHash, b)
			if err != nil {
				return err
			}
			if err := tx.UpdateTransactionBlob(ctx, p.UserID, r.IDHash, blob); err != nil {
				return err
			}
			if sug.Dismiss {
				if err := tx.SetTransactionState(ctx, p.UserID, r.IDHash, string(domain.TxDismissed)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Service) History(ctx context.Context, p Principal, m domain.Month, query string) ([]HistoryItem, error) {
	rows, err := s.store.ListTransactions(ctx, p.UserID, store.TxFilter{Month: m.String()})
	if err != nil {
		return nil, err
	}
	names, err := s.accountNames(ctx, p)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var out []HistoryItem
	for _, r := range rows {
		b, err := open[txBlob](p.Key, "transactions", r.IDHash, r.Blob)
		if err != nil {
			return nil, err
		}
		if query != "" && !strings.Contains(strings.ToLower(b.Payee), query) {
			continue
		}
		item := HistoryItem{IDHash: r.IDHash, Tx: b.toDomain(domain.TxState(r.State)), AccountName: names[b.AccountID]}
		if r.State == string(domain.TxFlagged) {
			if e, err := s.store.EntryBySourceRef(ctx, p.UserID, r.IDHash); err == nil {
				item.Entry = &e
			}
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Tx.Posted.After(out[j].Tx.Posted) })
	return out, nil
}
