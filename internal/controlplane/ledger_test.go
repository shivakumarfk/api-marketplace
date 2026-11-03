package controlplane

import (
	"math/rand"
	"testing"

	"apimarket/internal/money"
)

func TestBalanced(t *testing.T) {
	ok := []Entry{{Direction: "debit", Amount: 100}, {Direction: "credit", Amount: 60}, {Direction: "credit", Amount: 40}}
	if !Balanced(ok) {
		t.Fatal("balanced txn rejected")
	}
	for name, es := range map[string][]Entry{
		"single line":   {{Direction: "debit", Amount: 1}},
		"unequal":       {{Direction: "debit", Amount: 100}, {Direction: "credit", Amount: 99}},
		"zero amount":   {{Direction: "debit", Amount: 0}, {Direction: "credit", Amount: 0}},
		"negative":      {{Direction: "debit", Amount: -5}, {Direction: "credit", Amount: -5}},
		"bad direction": {{Direction: "debit", Amount: 5}, {Direction: "sideways", Amount: 5}},
	} {
		if Balanced(es) {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

// Property: any split of an amount into credits balances the debit.
func TestBalancedProperty(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 10_000; i++ {
		total := money.Micro(r.Int63n(1e12) + 2)
		es := []Entry{{Direction: "debit", Amount: total}}
		left := total
		for left > 0 {
			part := money.Micro(r.Int63n(int64(left)) + 1)
			es = append(es, Entry{Direction: "credit", Amount: part})
			left -= part
		}
		if len(es) >= 2 && !Balanced(es) {
			t.Fatalf("split of %d not balanced", total)
		}
	}
}
