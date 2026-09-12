package domain

import "time"

type TxState string

const (
	TxNew       TxState = "new"
	TxFlagged   TxState = "flagged"
	TxDismissed TxState = "dismissed"
)

// Transaction is a bank transaction as seen by its owner. Amount keeps the bank
// sign: negative is money out.
type Transaction struct {
	ID                  string
	AccountID           string
	Posted              time.Time
	Amount              Cents
	Payee               string
	State               TxState
	SuggestedCategoryID int64
	SuggestHousehold    bool
}

type BankAccount struct {
	ID          string
	Name        string
	Institution string
	Currency    string
	Balance     Cents
	BalanceDate time.Time
}
