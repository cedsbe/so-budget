package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Cents is an amount in integer cents.
type Cents int64

func (c Cents) String() string {
	neg := c < 0
	if neg {
		c = -c
	}
	s := fmt.Sprintf("%d.%02d", int64(c)/100, int64(c)%100)
	if neg {
		return "-" + s
	}
	return s
}

func (c Cents) Abs() Cents {
	if c < 0 {
		return -c
	}
	return c
}

// ParseCents parses "12.34", "-12.34", "$1,234.5", "7". At most two decimals.
func ParseCents(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "$", "")
	if s == "" {
		return 0, errors.New("empty amount")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, fmt.Errorf("amount %q has more than two decimals", s)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseUint(whole, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	f, err := strconv.ParseUint(frac, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	c := Cents(w*100 + f)
	if neg {
		c = -c
	}
	return c, nil
}
