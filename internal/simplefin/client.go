// Package simplefin talks to a SimpleFIN Bridge server.
package simplefin

import (
	"context"
	"time"
)

type Client interface {
	// Claim exchanges a one-time setup token for an access URL.
	Claim(ctx context.Context, setupToken string) (accessURL string, err error)
	// Accounts fetches accounts and their transactions between start and end (pending excluded).
	Accounts(ctx context.Context, accessURL string, start, end time.Time) (AccountSet, error)
}

type AccountSet struct {
	Errors   []string  `json:"errors"`
	Accounts []Account `json:"accounts"`
}

type Org struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

type Account struct {
	Org              Org           `json:"org"`
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Currency         string        `json:"currency"`
	Balance          string        `json:"balance"`
	AvailableBalance string        `json:"available-balance"`
	BalanceDate      int64         `json:"balance-date"`
	Transactions     []Transaction `json:"transactions"`
}

type Transaction struct {
	ID          string `json:"id"`
	Posted      int64  `json:"posted"`
	Amount      string `json:"amount"`
	Description string `json:"description"`
	Payee       string `json:"payee"`
	Memo        string `json:"memo"`
	Pending     bool   `json:"pending"`
}
