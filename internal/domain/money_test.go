package domain

import "testing"

func TestCentsString(t *testing.T) {
	cases := map[Cents]string{0: "0.00", 5: "0.05", 1234: "12.34", -1234: "-12.34", -5: "-0.05", 100000: "1000.00"}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int64(in), got, want)
		}
	}
}

func TestParseCents(t *testing.T) {
	ok := map[string]Cents{"12.34": 1234, "-12.34": -1234, "7": 700, "0.5": 50, "$1,234.5": 123450, " 3.00 ": 300, "+2": 200, "-$5": -500}
	for in, want := range ok {
		got, err := ParseCents(in)
		if err != nil || got != want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.234", "1..2", "--1"} {
		if _, err := ParseCents(bad); err == nil {
			t.Errorf("ParseCents(%q) expected error", bad)
		}
	}
}

func TestCentsAbs(t *testing.T) {
	if Cents(-5).Abs() != 5 || Cents(5).Abs() != 5 {
		t.Fatal("Abs wrong")
	}
}
