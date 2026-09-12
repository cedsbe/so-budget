package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/cedsbe/so-budget/internal/crypto"
)

type UserRow struct {
	ID         int64
	Name       string
	InviteHash string
	PwSalt     []byte
	PwWrapped  []byte
	RcSalt     []byte
	RcWrapped  []byte
	KDF        crypto.KDFParams
	Active     bool
}

type UserKeys struct {
	PwSalt, PwWrapped, RcSalt, RcWrapped []byte
	KDF                                  crypto.KDFParams
}

func (s *Store) CreateInvitedUser(ctx context.Context, name, inviteHash string) (int64, error) {
	res, err := s.q.ExecContext(ctx, `INSERT INTO users(name, invite_hash, created_at) VALUES (?, ?, ?)`,
		name, inviteHash, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const userCols = `id, name, COALESCE(invite_hash,''), pw_salt, pw_wrapped, rc_salt, rc_wrapped,
	COALESCE(kdf_time,0), COALESCE(kdf_memory,0), COALESCE(kdf_threads,0)`

func scanUser(row *sql.Row) (UserRow, error) {
	var u UserRow
	var t, m, th int64
	err := row.Scan(&u.ID, &u.Name, &u.InviteHash, &u.PwSalt, &u.PwWrapped, &u.RcSalt, &u.RcWrapped, &t, &m, &th)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	u.KDF = crypto.KDFParams{Time: uint32(t), Memory: uint32(m), Threads: uint8(th)}
	u.Active = len(u.PwWrapped) > 0
	return u, nil
}

func (s *Store) UserByInvite(ctx context.Context, hash string) (UserRow, error) {
	return scanUser(s.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE invite_hash=?`, hash))
}

func (s *Store) UserByName(ctx context.Context, name string) (UserRow, error) {
	return scanUser(s.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE name=?`, name))
}

func (s *Store) UserByID(ctx context.Context, id int64) (UserRow, error) {
	return scanUser(s.q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *Store) ListUsers(ctx context.Context) ([]UserRow, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id, name, pw_wrapped IS NOT NULL FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Name, &u.Active); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserKeys(ctx context.Context, id int64, k UserKeys) error {
	_, err := s.q.ExecContext(ctx, `UPDATE users SET invite_hash=NULL, pw_salt=?, pw_wrapped=?, rc_salt=?, rc_wrapped=?,
		kdf_time=?, kdf_memory=?, kdf_threads=? WHERE id=?`,
		k.PwSalt, k.PwWrapped, k.RcSalt, k.RcWrapped, k.KDF.Time, k.KDF.Memory, k.KDF.Threads, id)
	return err
}
