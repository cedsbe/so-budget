package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/store"
)

const (
	firstPullDays  = 90
	overlapDays    = 7
	transferWindow = 3 * 24 * time.Hour
	dateLayout     = "2006-01-02"
)

type credentialBlob struct {
	AccessURL string
	StartDate string // YYYY-MM-DD or ""
}

type accountBlob struct {
	ID, Name, Institution, Currency string
	Balance                         domain.Cents
	BalanceDate                     string
}

type txBlob struct {
	ID                  string
	AccountID           string
	Posted              string // YYYY-MM-DD in the configured location
	Amount              domain.Cents
	Payee               string
	SuggestedCategoryID int64
	SuggestHousehold    bool
}

func (b txBlob) toDomain(state domain.TxState) domain.Transaction {
	posted, _ := time.Parse(dateLayout, b.Posted)
	return domain.Transaction{ID: b.ID, AccountID: b.AccountID, Posted: posted, Amount: b.Amount, Payee: b.Payee,
		State: state, SuggestedCategoryID: b.SuggestedCategoryID, SuggestHousehold: b.SuggestHousehold}
}

type SyncResult struct {
	New       int
	Dismissed int
	Errors    []string
	SyncedAt  time.Time
}

func (s *Service) credential(ctx context.Context, p Principal) (credentialBlob, error) {
	blob, err := s.store.Credential(ctx, p.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return credentialBlob{}, ErrNotLinked
	}
	if err != nil {
		return credentialBlob{}, err
	}
	return open[credentialBlob](p.Key, "credentials", "", blob)
}

func (s *Service) saveCredential(ctx context.Context, p Principal, c credentialBlob) error {
	blob, err := seal(p.Key, "credentials", "", c)
	if err != nil {
		return err
	}
	return s.store.SaveCredential(ctx, p.UserID, blob)
}

func (s *Service) Linked(ctx context.Context, p Principal) (bool, error) {
	_, err := s.credential(ctx, p)
	if errors.Is(err, ErrNotLinked) {
		return false, nil
	}
	return err == nil, err
}

func (s *Service) LinkSimpleFIN(ctx context.Context, p Principal, setupToken string) error {
	access, err := s.bank.Claim(ctx, setupToken)
	if err != nil {
		return err
	}
	if err := s.saveCredential(ctx, p, credentialBlob{AccessURL: access}); err != nil {
		return err
	}
	_, err = s.Sync(ctx, p)
	return err
}

func (s *Service) SyncStart(ctx context.Context, p Principal) (string, error) {
	c, err := s.credential(ctx, p)
	return c.StartDate, err
}

func (s *Service) SetSyncStart(ctx context.Context, p Principal, date string) error {
	if date != "" {
		if _, err := time.Parse(dateLayout, date); err != nil {
			return fmt.Errorf("invalid date %q", date)
		}
	}
	c, err := s.credential(ctx, p)
	if err != nil {
		return err
	}
	c.StartDate = date
	return s.saveCredential(ctx, p, c)
}

func (s *Service) LastSync(ctx context.Context, p Principal) (time.Time, bool, error) {
	return s.store.LastSync(ctx, p.UserID)
}

func (s *Service) Accounts(ctx context.Context, p Principal) ([]domain.BankAccount, error) {
	rows, err := s.store.ListAccounts(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.BankAccount, 0, len(rows))
	for _, r := range rows {
		b, err := open[accountBlob](p.Key, "bank_accounts", r.ID, r.Blob)
		if err != nil {
			return nil, err
		}
		bd, _ := time.Parse(dateLayout, b.BalanceDate)
		out = append(out, domain.BankAccount{ID: b.ID, Name: b.Name, Institution: b.Institution, Currency: b.Currency, Balance: b.Balance, BalanceDate: bd})
	}
	return out, nil
}

func (s *Service) RenameAccount(ctx context.Context, p Principal, accountID, name string) error {
	h := s.hashID(p, accountID)
	rows, err := s.store.ListAccounts(ctx, p.UserID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.ID != h {
			continue
		}
		b, err := open[accountBlob](p.Key, "bank_accounts", r.ID, r.Blob)
		if err != nil {
			return err
		}
		b.Name = name
		blob, err := seal(p.Key, "bank_accounts", r.ID, b)
		if err != nil {
			return err
		}
		return s.store.UpsertAccount(ctx, p.UserID, r.ID, blob)
	}
	return ErrNotFound
}

// accountNames maps account id → display name for the principal.
func (s *Service) accountNames(ctx context.Context, p Principal) (map[string]string, error) {
	accts, err := s.Accounts(ctx, p)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(accts))
	for _, a := range accts {
		m[a.ID] = a.Name
	}
	return m, nil
}

// Sync pulls new transactions for the principal. It is only ever called with a
// live principal, i.e. inside a logged-in session.
func (s *Service) Sync(ctx context.Context, p Principal) (SyncResult, error) {
	var res SyncResult
	cred, err := s.credential(ctx, p)
	if err != nil {
		return res, err
	}
	now := s.now()
	start := now.AddDate(0, 0, -firstPullDays)
	if last, ok, err := s.store.LastSync(ctx, p.UserID); err != nil {
		return res, err
	} else if ok {
		start = last.AddDate(0, 0, -overlapDays)
	}
	if cred.StartDate != "" {
		if sd, err := time.Parse(dateLayout, cred.StartDate); err == nil && sd.After(start) {
			start = sd
		}
	}
	set, err := s.bank.Accounts(ctx, cred.AccessURL, start, now)
	if err != nil {
		return res, err
	}
	res.Errors = set.Errors

	hrules, err := s.store.ListHouseholdRules(ctx)
	if err != nil {
		return res, err
	}
	prules, err := s.PrivateRules(ctx, p)
	if err != nil {
		return res, err
	}

	type incoming struct {
		hash string
		blob txBlob
	}
	var candidates []incoming
	var accountBlobs []struct {
		hash string
		blob []byte
	}
	for _, a := range set.Accounts {
		bal, _ := domain.ParseCents(a.Balance)
		ab := accountBlob{ID: a.ID, Name: a.Name, Institution: a.Org.Name, Currency: a.Currency, Balance: bal,
			BalanceDate: time.Unix(a.BalanceDate, 0).In(s.loc).Format(dateLayout)}
		h := s.hashID(p, a.ID)
		// Keep a user-chosen name if the account already exists.
		if existing, err := s.store.ListAccounts(ctx, p.UserID); err == nil {
			for _, r := range existing {
				if r.ID == h {
					if old, err := open[accountBlob](p.Key, "bank_accounts", h, r.Blob); err == nil && old.Name != "" {
						ab.Name = old.Name
					}
				}
			}
		}
		blob, err := seal(p.Key, "bank_accounts", h, ab)
		if err != nil {
			return res, err
		}
		accountBlobs = append(accountBlobs, struct {
			hash string
			blob []byte
		}{h, blob})
		for _, t := range a.Transactions {
			if t.Pending {
				continue
			}
			amt, err := domain.ParseCents(t.Amount)
			if err != nil {
				return res, fmt.Errorf("transaction %s: %w", t.ID, err)
			}
			payee := t.Description
			if payee == "" {
				payee = t.Payee
			}
			candidates = append(candidates, incoming{hash: s.hashID(p, t.ID), blob: txBlob{
				ID: t.ID, AccountID: a.ID, Posted: time.Unix(t.Posted, 0).In(s.loc).Format(dateLayout), Amount: amt, Payee: payee}})
		}
	}

	hashes := make([]string, 0, len(candidates))
	for _, c := range candidates {
		hashes = append(hashes, c.hash)
	}
	existing, err := s.store.ExistingTransactionHashes(ctx, p.UserID, hashes)
	if err != nil {
		return res, err
	}
	var fresh []incoming
	for _, c := range candidates {
		if !existing[c.hash] {
			fresh = append(fresh, c)
		}
	}

	// Transfer detection runs over new transactions plus existing `new` ones.
	pool := make([]domain.Transaction, 0, len(fresh))
	for _, c := range fresh {
		pool = append(pool, c.blob.toDomain(domain.TxNew))
	}
	existingNew, err := s.store.ListTransactions(ctx, p.UserID, store.TxFilter{State: string(domain.TxNew)})
	if err != nil {
		return res, err
	}
	existingByID := map[string]string{} // tx id → hash
	for _, r := range existingNew {
		b, err := open[txBlob](p.Key, "transactions", r.IDHash, r.Blob)
		if err != nil {
			return res, err
		}
		existingByID[b.ID] = r.IDHash
		pool = append(pool, b.toDomain(domain.TxNew))
	}
	transfers := domain.DetectTransfers(pool, transferWindow)

	err = s.store.WithTx(ctx, func(tx *store.Store) error {
		for _, ab := range accountBlobs {
			if err := tx.UpsertAccount(ctx, p.UserID, ab.hash, ab.blob); err != nil {
				return err
			}
		}
		for _, c := range fresh {
			sug := domain.Evaluate(c.blob.Payee, prules, hrules)
			c.blob.SuggestedCategoryID = sug.CategoryID
			c.blob.SuggestHousehold = sug.Household
			state := domain.TxNew
			if sug.Dismiss || transfers[c.blob.ID] {
				state = domain.TxDismissed
				res.Dismissed++
			}
			blob, err := seal(p.Key, "transactions", c.hash, c.blob)
			if err != nil {
				return err
			}
			if err := tx.InsertTransaction(ctx, p.UserID, store.TxRow{IDHash: c.hash, State: string(state), Month: c.blob.Posted[:7], Blob: blob}); err != nil {
				return err
			}
			res.New++
		}
		for id, h := range existingByID {
			if transfers[id] {
				if err := tx.SetTransactionState(ctx, p.UserID, h, string(domain.TxDismissed)); err != nil {
					return err
				}
				res.Dismissed++
			}
		}
		return tx.SetLastSync(ctx, p.UserID, now)
	})
	if err != nil {
		return res, err
	}
	res.SyncedAt = now
	return res, nil
}
