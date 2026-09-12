package domain

// PlannedExpense is an expected household expense. Amount is positive.
type PlannedExpense struct {
	ID          int64
	Name        string
	Amount      Cents
	CategoryID  int64
	PayerID     int64
	Day         int    // day of month it usually happens
	Recurring   bool   // every month
	SingleMonth string // "YYYY-MM" when not recurring
	Active      bool
}

func (p PlannedExpense) AppliesTo(m Month) bool {
	return p.Active && (p.Recurring || p.SingleMonth == m.String())
}

type Contribution struct {
	UserID int64
	Month  Month
	Amount Cents
}

// AmountWithinTolerance accepts actual within 10% of expected or $5, whichever is larger.
func AmountWithinTolerance(actual, expected Cents) bool {
	tol := expected.Abs() / 10
	if tol < 500 {
		tol = 500
	}
	return (actual - expected).Abs() <= tol
}

// MatchPlanned finds the planned expense an entry pays for. It returns the id
// when exactly one candidate fits, or ambiguous=true when several do.
func MatchPlanned(e HouseholdEntry, candidates []PlannedExpense, taken map[int64]bool) (int64, bool) {
	m := MonthOf(e.Date)
	var found []int64
	for _, p := range candidates {
		if taken[p.ID] || p.PayerID != e.PayerID || p.CategoryID != e.CategoryID || !p.AppliesTo(m) {
			continue
		}
		if AmountWithinTolerance(e.Amount, p.Amount) {
			found = append(found, p.ID)
		}
	}
	switch len(found) {
	case 0:
		return 0, false
	case 1:
		return found[0], false
	default:
		return 0, true
	}
}
