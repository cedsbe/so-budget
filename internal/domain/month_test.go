package domain

import (
	"testing"
	"time"
)

func TestMonthRoundTrip(t *testing.T) {
	m, err := ParseMonth("2026-09")
	if err != nil {
		t.Fatal(err)
	}
	if m.String() != "2026-09" || m.Year != 2026 || m.Mon != time.September {
		t.Fatalf("got %+v", m)
	}
	if _, err := ParseMonth("2026-13"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMonthArithmetic(t *testing.T) {
	m := Month{2026, time.December}
	if m.Next() != (Month{2027, time.January}) || m.Prev() != (Month{2026, time.November}) {
		t.Fatalf("next/prev wrong: %v %v", m.Next(), m.Prev())
	}
	if !m.Start().Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("start wrong")
	}
	if !m.End().Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("end wrong")
	}
	if !m.Contains(time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC)) || m.Contains(m.End()) {
		t.Fatal("contains wrong")
	}
	if !m.Prev().Before(m) || m.Before(m) {
		t.Fatal("before wrong")
	}
	if MonthOf(time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)) != (Month{2026, time.March}) {
		t.Fatal("MonthOf wrong")
	}
}
