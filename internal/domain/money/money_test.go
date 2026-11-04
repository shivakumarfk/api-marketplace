package money

import (
	"math/rand"
	"testing"
)

func TestToPaise(t *testing.T) {
	cases := []struct {
		in   Micro
		want int64
	}{
		{0, 0},
		{4_999, 0},
		{5_000, 1}, // half rounds away from zero
		{10_000, 1},
		{14_999, 1},
		{15_000, 2},
		{FromRupees(12) + 345_000, 1235},
		{-5_000, -1},
		{-4_999, 0},
	}
	for _, c := range cases {
		if got := c.in.ToPaise(); got != c.want {
			t.Errorf("Micro(%d).ToPaise() = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestString(t *testing.T) {
	if got := (FromRupees(12) + 345_000).String(); got != "₹12.35" {
		t.Errorf("got %q", got)
	}
	if got := Micro(-15_000).String(); got != "-₹0.02" {
		t.Errorf("got %q", got)
	}
}

// Property: rounding error is never more than half a paisa.
func TestToPaiseErrorBound(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 100_000; i++ {
		m := Micro(r.Int63n(2_000_000_000) - 1_000_000_000)
		diff := int64(m) - m.ToPaise()*int64(MicroPerPaisa)
		if diff < 0 {
			diff = -diff
		}
		if diff > int64(MicroPerPaisa/2) {
			t.Fatalf("Micro(%d): rounding error %d exceeds half a paisa", m, diff)
		}
	}
}

func TestParseRupeesAndDecimal(t *testing.T) {
	ok := []struct {
		in   string
		want Micro
		dec  string
	}{
		{"", 0, "0.00"},
		{"0", 0, "0.00"},
		{"499", 499_000_000, "499.00"},
		{"0.02", 20_000, "0.02"},
		{"0.000001", 1, "0.000001"},
		{"12.5", 12_500_000, "12.50"},
		{"007.10", 7_100_000, "7.10"},
	}
	for _, c := range ok {
		got, err := ParseRupees(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseRupees(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
		if d := got.Decimal(); d != c.dec {
			t.Errorf("Micro(%d).Decimal() = %q, want %q", got, d, c.dec)
		}
	}
	for _, bad := range []string{"-1", "1.", ".5", "0.0000001", "1e3", "1,000", "₹5", " 5", "abc", "1.2.3", "9999999999999"} {
		if _, err := ParseRupees(bad); err == nil {
			t.Errorf("ParseRupees(%q) must fail", bad)
		}
	}
	if Micro(-20_000).Decimal() != "-0.02" {
		t.Error("negative decimal")
	}
}

func TestParseRupeesRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for range 1000 {
		m := Micro(r.Int63n(int64(FromRupees(1_000_000))))
		got, err := ParseRupees(m.Decimal())
		if err != nil || got != m {
			t.Fatalf("round trip %d → %q → %d %v", m, m.Decimal(), got, err)
		}
	}
}
