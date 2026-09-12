package domain

import "strings"

type HouseholdRule struct {
	ID               int64
	Pattern          string
	CategoryID       int64
	SuggestHousehold bool
}

// PrivateRule dismisses matching payees for one user.
type PrivateRule struct {
	ID      string
	Pattern string
}

type Suggestion struct {
	Dismiss    bool
	CategoryID int64
	Household  bool
}

// cardPaymentPatterns are TD payee texts for credit card payments. Adjust after
// seeing real data; they are matched case-insensitively as substrings.
var cardPaymentPatterns = []string{
	"PAYMENT - THANK YOU",
	"PAYMENT THANK YOU",
	"TD VISA PAYMENT",
	"TD VISA PREAUTH PYMT",
	"TD MASTERCARD PAYMENT",
	"TD CREDIT CARD PAYMENT",
}

func Matches(payee, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	return strings.Contains(strings.ToLower(payee), strings.ToLower(pattern))
}

func IsCardPayment(payee string) bool {
	for _, p := range cardPaymentPatterns {
		if Matches(payee, p) {
			return true
		}
	}
	return false
}

// Evaluate applies built-in, private, then household rules. Within a layer the
// longest matching pattern wins.
func Evaluate(payee string, private []PrivateRule, household []HouseholdRule) Suggestion {
	if IsCardPayment(payee) {
		return Suggestion{Dismiss: true}
	}
	best := -1
	for _, r := range private {
		if Matches(payee, r.Pattern) && len(r.Pattern) > best {
			best = len(r.Pattern)
		}
	}
	if best >= 0 {
		return Suggestion{Dismiss: true}
	}
	var out Suggestion
	for _, r := range household {
		if Matches(payee, r.Pattern) && len(r.Pattern) > best {
			best = len(r.Pattern)
			out = Suggestion{CategoryID: r.CategoryID, Household: r.SuggestHousehold}
		}
	}
	return out
}
