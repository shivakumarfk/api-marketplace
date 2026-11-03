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
