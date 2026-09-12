package domain

import "testing"

func TestEvaluatePrecedence(t *testing.T) {
	private := []PrivateRule{{ID: "p1", Pattern: "netflix"}}
	household := []HouseholdRule{
		{ID: 1, Pattern: "sobeys", CategoryID: 10, SuggestHousehold: true},
		{ID: 2, Pattern: "sobeys pharmacy", CategoryID: 20, SuggestHousehold: false},
		{ID: 3, Pattern: "netflix", CategoryID: 30, SuggestHousehold: true},
	}
	cases := []struct {
		payee string
		want  Suggestion
	}{
		{"SOBEYS #1234 TORONTO", Suggestion{CategoryID: 10, Household: true}},
		{"SOBEYS PHARMACY 77", Suggestion{CategoryID: 20, Household: false}}, // longest wins
		{"NETFLIX.COM", Suggestion{Dismiss: true}},                           // private beats household
		{"PAYMENT - THANK YOU", Suggestion{Dismiss: true}},                   // built-in card payment
		{"UNKNOWN SHOP", Suggestion{}},
	}
	for _, c := range cases {
		if got := Evaluate(c.payee, private, household); got != c.want {
			t.Errorf("Evaluate(%q) = %+v, want %+v", c.payee, got, c.want)
		}
	}
}

func TestMatchesIgnoresCaseAndEmptyPattern(t *testing.T) {
	if !Matches("Tim Hortons #12", "tim hortons") || Matches("anything", "") || Matches("anything", "  ") {
		t.Fatal("Matches wrong")
	}
}
