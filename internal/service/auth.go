package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/domain"
	"github.com/cedsbe/so-budget/internal/store"
)

const minPasswordLen = 10

func tokenHash(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// Invite creates a user with no keys and returns the one-time invitation token.
func (s *Service) Invite(ctx context.Context, name string) (string, error) {
	tok, err := crypto.RandomToken(24)
	if err != nil {
		return "", err
	}
	if _, err := s.store.CreateInvitedUser(ctx, name, tokenHash(tok)); err != nil {
		return "", err
	}
	return tok, nil
}

func (s *Service) InviteName(ctx context.Context, token string) (string, error) {
	u, err := s.store.UserByInvite(ctx, tokenHash(token))
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrInviteUsed
	}
	if err != nil {
		return "", err
	}
	return u.Name, nil
}

// Activate sets the first password, generates the data key and returns the recovery code.
func (s *Service) Activate(ctx context.Context, token, password string) (string, error) {
	u, err := s.store.UserByInvite(ctx, tokenHash(token))
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrInviteUsed
	}
	if err != nil {
		return "", err
	}
	if len(password) < minPasswordLen {
		return "", ErrWeakPassword
	}
	dek, err := crypto.NewKey()
	if err != nil {
		return "", err
	}
	keys, code, err := s.wrapAll(dek, password)
	if err != nil {
		return "", err
	}
	return code, s.store.SetUserKeys(ctx, u.ID, keys)
}

// wrapAll wraps dek under the password and under a fresh recovery code.
func (s *Service) wrapAll(dek crypto.Key, password string) (store.UserKeys, string, error) {
	pwSalt, err := crypto.NewSalt()
	if err != nil {
		return store.UserKeys{}, "", err
	}
	rcSalt, err := crypto.NewSalt()
	if err != nil {
		return store.UserKeys{}, "", err
	}
	code, err := crypto.NewRecoveryCode()
	if err != nil {
		return store.UserKeys{}, "", err
	}
	pwWrapped, err := crypto.Wrap(dek, crypto.Derive(password, pwSalt, s.kdf))
	if err != nil {
		return store.UserKeys{}, "", err
	}
	rcWrapped, err := crypto.Wrap(dek, crypto.Derive(crypto.NormalizeRecoveryCode(code), rcSalt, s.kdf))
	if err != nil {
		return store.UserKeys{}, "", err
	}
	return store.UserKeys{PwSalt: pwSalt, PwWrapped: pwWrapped, RcSalt: rcSalt, RcWrapped: rcWrapped, KDF: s.kdf}, code, nil
}

func (s *Service) Login(ctx context.Context, name, password string) (Principal, error) {
	u, err := s.store.UserByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !u.Active) {
		return Principal{}, ErrInvalidCredentials
	}
	if err != nil {
		return Principal{}, err
	}
	dek, err := crypto.Unwrap(u.PwWrapped, crypto.Derive(password, u.PwSalt, u.KDF))
	if err != nil {
		return Principal{}, ErrInvalidCredentials
	}
	return Principal{UserID: u.ID, Name: u.Name, Key: dek}, nil
}

func (s *Service) ChangePassword(ctx context.Context, p Principal, oldPw, newPw string) error {
	u, err := s.store.UserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	if _, err := crypto.Unwrap(u.PwWrapped, crypto.Derive(oldPw, u.PwSalt, u.KDF)); err != nil {
		return ErrInvalidCredentials
	}
	if len(newPw) < minPasswordLen {
		return ErrWeakPassword
	}
	salt, err := crypto.NewSalt()
	if err != nil {
		return err
	}
	wrapped, err := crypto.Wrap(p.Key, crypto.Derive(newPw, salt, u.KDF))
	if err != nil {
		return err
	}
	return s.store.SetUserKeys(ctx, u.ID, store.UserKeys{PwSalt: salt, PwWrapped: wrapped, RcSalt: u.RcSalt, RcWrapped: u.RcWrapped, KDF: u.KDF})
}

// Recover unwraps the key with the recovery code, sets a new password and returns a new code.
func (s *Service) Recover(ctx context.Context, name, code, newPw string) (string, error) {
	u, err := s.store.UserByName(ctx, name)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !u.Active) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	dek, err := crypto.Unwrap(u.RcWrapped, crypto.Derive(crypto.NormalizeRecoveryCode(code), u.RcSalt, u.KDF))
	if err != nil {
		return "", ErrInvalidCredentials
	}
	if len(newPw) < minPasswordLen {
		return "", ErrWeakPassword
	}
	keys, newCode, err := s.wrapAll(dek, newPw)
	if err != nil {
		return "", err
	}
	return newCode, s.store.SetUserKeys(ctx, u.ID, keys)
}

func (s *Service) Users(ctx context.Context) ([]domain.User, error) {
	rows, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.User, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.User{ID: r.ID, Name: r.Name})
	}
	return out, nil
}

// UserName resolves a user id for display.
func (s *Service) UserName(ctx context.Context, id int64) string {
	u, err := s.store.UserByID(ctx, id)
	if err != nil {
		return "?"
	}
	return u.Name
}
