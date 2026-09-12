package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type PrivateRow struct {
	ID   string
	Blob []byte
}

type TxRow struct {
	IDHash string
	State  string
	Month  string
	Blob   []byte
}

type TxFilter struct {
	State string
	Month string
}

func (s *Store) SaveCredential(ctx context.Context, userID int64, blob []byte) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO credentials(user_id, blob) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET blob=excluded.blob`, userID, blob)
	return err
}

func (s *Store) Credential(ctx context.Context, userID int64) ([]byte, error) {
	var b []byte
	err := s.q.QueryRowContext(ctx, `SELECT blob FROM credentials WHERE user_id=?`, userID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *Store) UpsertAccount(ctx context.Context, userID int64, idHash string, blob []byte) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO bank_accounts(user_id, id_hash, blob) VALUES (?, ?, ?)
		ON CONFLICT(user_id, id_hash) DO UPDATE SET blob=excluded.blob`, userID, idHash, blob)
	return err
}

func (s *Store) ListAccounts(ctx context.Context, userID int64) ([]PrivateRow, error) {
	return s.privateRows(ctx, `SELECT id_hash, blob FROM bank_accounts WHERE user_id=? ORDER BY id_hash`, userID)
}

func (s *Store) privateRows(ctx context.Context, query string, args ...any) ([]PrivateRow, error) {
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PrivateRow
	for rows.Next() {
		var r PrivateRow
		if err := rows.Scan(&r.ID, &r.Blob); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ExistingTransactionHashes(ctx context.Context, userID int64, hashes []string) (map[string]bool, error) {
	out := map[string]bool{}
	const chunk = 500
	for i := 0; i < len(hashes); i += chunk {
		part := hashes[i:min(i+chunk, len(hashes))]
		args := make([]any, 0, len(part)+1)
		args = append(args, userID)
		for _, h := range part {
			args = append(args, h)
		}
		q := `SELECT id_hash FROM transactions WHERE user_id=? AND id_hash IN (?` + strings.Repeat(",?", len(part)-1) + `)`
		rows, err := s.q.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				rows.Close()
				return nil, err
			}
			out[h] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) InsertTransaction(ctx context.Context, userID int64, r TxRow) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO transactions(user_id, id_hash, state, posted_month, blob) VALUES (?, ?, ?, ?, ?)`,
		userID, r.IDHash, r.State, r.Month, r.Blob)
	return err
}

func (s *Store) UpdateTransactionBlob(ctx context.Context, userID int64, idHash string, blob []byte) error {
	return s.execOne(ctx, `UPDATE transactions SET blob=? WHERE user_id=? AND id_hash=?`, blob, userID, idHash)
}

func (s *Store) SetTransactionState(ctx context.Context, userID int64, idHash, state string) error {
	return s.execOne(ctx, `UPDATE transactions SET state=? WHERE user_id=? AND id_hash=?`, state, userID, idHash)
}

func (s *Store) execOne(ctx context.Context, q string, args ...any) error {
	res, err := s.q.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Transaction(ctx context.Context, userID int64, idHash string) (TxRow, error) {
	var r TxRow
	err := s.q.QueryRowContext(ctx, `SELECT id_hash, state, posted_month, blob FROM transactions WHERE user_id=? AND id_hash=?`, userID, idHash).
		Scan(&r.IDHash, &r.State, &r.Month, &r.Blob)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) ListTransactions(ctx context.Context, userID int64, f TxFilter) ([]TxRow, error) {
	q := `SELECT id_hash, state, posted_month, blob FROM transactions WHERE user_id=?`
	args := []any{userID}
	if f.State != "" {
		q += ` AND state=?`
		args = append(args, f.State)
	}
	if f.Month != "" {
		q += ` AND posted_month=?`
		args = append(args, f.Month)
	}
	rows, err := s.q.QueryContext(ctx, q+` ORDER BY posted_month DESC, id_hash`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TxRow
	for rows.Next() {
		var r TxRow
		if err := rows.Scan(&r.IDHash, &r.State, &r.Month, &r.Blob); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CountTransactions(ctx context.Context, userID int64, state string) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT count(*) FROM transactions WHERE user_id=? AND state=?`, userID, state).Scan(&n)
	return n, err
}

func (s *Store) LastSync(ctx context.Context, userID int64) (time.Time, bool, error) {
	var ts string
	err := s.q.QueryRowContext(ctx, `SELECT last_success_at FROM sync_state WHERE user_id=?`, userID).Scan(&ts)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, err := time.Parse(time.RFC3339, ts)
	return t, err == nil, err
}

func (s *Store) SetLastSync(ctx context.Context, userID int64, t time.Time) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO sync_state(user_id, last_success_at) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET last_success_at=excluded.last_success_at`, userID, t.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) InsertPrivateRule(ctx context.Context, userID int64, id string, blob []byte) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO private_rules(user_id, id, blob) VALUES (?, ?, ?)`, userID, id, blob)
	return err
}

func (s *Store) ListPrivateRules(ctx context.Context, userID int64) ([]PrivateRow, error) {
	return s.privateRows(ctx, `SELECT id, blob FROM private_rules WHERE user_id=? ORDER BY id`, userID)
}

func (s *Store) DeletePrivateRule(ctx context.Context, userID int64, id string) error {
	return s.execOne(ctx, `DELETE FROM private_rules WHERE user_id=? AND id=?`, userID, id)
}
