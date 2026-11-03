// Package money holds amounts as integer micro-rupees (1 INR = 1,000,000 micro).
// Per-call prices can be fractions of a paisa, so we never use floats for money.
package money

import "fmt"

// Micro is an amount in micro-rupees.
type Micro int64

const (
	MicroPerRupee Micro = 1_000_000
	MicroPerPaisa Micro = 10_000
)

// FromRupees converts whole rupees to micro-rupees.
func FromRupees(r int64) Micro { return Micro(r) * MicroPerRupee }

// ToPaise rounds to the nearest paisa, half away from zero. Used only on invoices.
func (m Micro) ToPaise() int64 {
	q := int64(m / MicroPerPaisa)
	r := int64(m % MicroPerPaisa)
	half := int64(MicroPerPaisa / 2)
	switch {
	case r >= half:
		q++
	case r <= -half:
		q--
	}
	return q
}

// String formats the amount in rupees with paise, e.g. "₹12.35" (rounded).
func (m Micro) String() string {
	p := m.ToPaise()
	sign := ""
	if p < 0 {
		sign = "-"
		p = -p
	}
	return fmt.Sprintf("%s₹%d.%02d", sign, p/100, p%100)
}
