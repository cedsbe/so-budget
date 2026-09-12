package domain

import (
	"fmt"
	"time"
)

// Month is a calendar month. All Month times are UTC civil dates.
type Month struct {
	Year int
	Mon  time.Month
}

func MonthOf(t time.Time) Month { return Month{t.Year(), t.Month()} }

func ParseMonth(s string) (Month, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return Month{}, fmt.Errorf("invalid month %q", s)
	}
	return MonthOf(t), nil
}

func (m Month) String() string   { return fmt.Sprintf("%04d-%02d", m.Year, int(m.Mon)) }
func (m Month) Start() time.Time { return time.Date(m.Year, m.Mon, 1, 0, 0, 0, 0, time.UTC) }
func (m Month) End() time.Time   { return m.Start().AddDate(0, 1, 0) }
func (m Month) Next() Month      { return MonthOf(m.End()) }
func (m Month) Prev() Month      { return MonthOf(m.Start().AddDate(0, -1, 0)) }
func (m Month) Contains(t time.Time) bool {
	return MonthOf(t) == m
}
func (m Month) Before(o Month) bool {
	return m.Year < o.Year || (m.Year == o.Year && m.Mon < o.Mon)
}
