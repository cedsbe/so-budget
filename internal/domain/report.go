package domain

import "time"

type PersonReport struct {
	User             User
	Contribution     Cents
	Spent            Cents
	PlannedUnmatched Cents
	Committed        Cents
	Balance          Cents
}

type PlannedStatus struct {
	Planned  PlannedExpense
	Matched  Cents
	Occurred bool
}

type MonthReport struct {
	Month            Month
	Closed           bool
	Contributions    Cents
	Spent            Cents
	PlannedUnmatched Cents
	Committed        Cents
	Margin           Cents
	People           []PersonReport
	Planned          []PlannedStatus
}

// ComputeMonth is the month arithmetic from the spec. planned must already be
// filtered to the expenses applying to m.
func ComputeMonth(m Month, today time.Time, users []User, contribs map[int64]Cents, entries []HouseholdEntry, planned []PlannedExpense) MonthReport {
	r := MonthReport{Month: m}
	todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	r.Closed = !m.End().After(todayDate)

	matched := map[int64]Cents{}
	hasMatch := map[int64]bool{}
	spentBy := map[int64]Cents{}
	for _, e := range entries {
		spentBy[e.PayerID] += e.Amount
		r.Spent += e.Amount
		if e.PlannedID != 0 {
			matched[e.PlannedID] += e.Amount
			hasMatch[e.PlannedID] = true
		}
	}
	unmatchedBy := map[int64]Cents{}
	for _, p := range planned {
		st := PlannedStatus{Planned: p, Matched: matched[p.ID], Occurred: hasMatch[p.ID]}
		if !st.Occurred && !r.Closed {
			unmatchedBy[p.PayerID] += p.Amount
			r.PlannedUnmatched += p.Amount
		}
		r.Planned = append(r.Planned, st)
	}
	for _, u := range users {
		pr := PersonReport{User: u, Contribution: contribs[u.ID], Spent: spentBy[u.ID], PlannedUnmatched: unmatchedBy[u.ID]}
		pr.Committed = pr.Spent + pr.PlannedUnmatched
		pr.Balance = pr.Contribution - pr.Committed
		r.Contributions += pr.Contribution
		r.People = append(r.People, pr)
	}
	r.Committed = r.Spent + r.PlannedUnmatched
	r.Margin = r.Contributions - r.Committed
	return r
}
