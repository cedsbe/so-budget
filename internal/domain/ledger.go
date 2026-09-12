package domain

import "time"

type Category struct {
	ID     int64
	Name   string
	Active bool
}

const (
	SourceSync   = "sync"
	SourceImport = "import"
	SourceManual = "manual"
)

// HouseholdEntry is a line of the shared ledger. Amount is positive for money
// spent on the household; a refund is negative.
type HouseholdEntry struct {
	ID         int64
	PayerID    int64
	Date       time.Time
	Amount     Cents
	CategoryID int64
	Note       string
	Source     string
	SourceRef  string // transaction id hash for SourceSync, else ""
	PlannedID  int64  // matched planned expense, 0 if none
}
