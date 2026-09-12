// Package service implements the use cases. It owns encryption of private data;
// the store only ever sees sealed blobs.
package service

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/cedsbe/so-budget/internal/crypto"
	"github.com/cedsbe/so-budget/internal/simplefin"
	"github.com/cedsbe/so-budget/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrWeakPassword       = errors.New("password must be at least 10 characters")
	ErrInviteUsed         = errors.New("invitation already used or unknown")
	ErrNotLinked          = errors.New("no bank connection linked")
	ErrNotFound           = errors.New("not found")
	ErrInvalidState       = errors.New("transaction is not in a state that allows this action")
	ErrInvalidAmount      = errors.New("invalid amount")
)

type Options struct {
	KDF      crypto.KDFParams
	Pepper   []byte
	Location *time.Location
	Now      func() time.Time
}

type Service struct {
	store  *store.Store
	bank   simplefin.Client
	kdf    crypto.KDFParams
	pepper []byte
	loc    *time.Location
	now    func() time.Time
}

// Principal is a logged-in user together with their unwrapped data key.
// It lives in the web session's memory and is never persisted.
type Principal struct {
	UserID int64
	Name   string
	Key    crypto.Key
}

func New(st *store.Store, bank simplefin.Client, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Location == nil {
		o.Location = time.UTC
	}
	if o.KDF == (crypto.KDFParams{}) {
		o.KDF = crypto.DefaultParams
	}
	return &Service{store: st, bank: bank, kdf: o.KDF, pepper: o.Pepper, loc: o.Location, now: o.Now}
}

func aad(table, id string) []byte { return []byte(table + ":" + id) }

func seal[T any](key crypto.Key, table, id string, v T) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return crypto.Seal(key, aad(table, id), b)
}

func open[T any](key crypto.Key, table, id string, blob []byte) (T, error) {
	var v T
	b, err := crypto.Open(key, aad(table, id), blob)
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal(b, &v)
}

func (s *Service) hashID(p Principal, id string) string { return crypto.HashID(p.Key, s.pepper, id) }
