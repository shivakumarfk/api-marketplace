// Package money holds amounts as integer micro-rupees (1 INR = 1,000,000 micro).
// Per-call prices can be fractions of a paisa, so we never use floats for money.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

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

// ParseRupees parses a non-negative decimal rupee amount such as "499",
// "0.02" or "0.000150" exactly (no floats). Up to 6 decimal places.
func ParseRupees(s string) (Micro, error) {
	if s == "" {
		return 0, nil
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || !allDigits(whole) || (hasDot && (frac == "" || !allDigits(frac))) || len(frac) > 6 {
		return 0, fmt.Errorf("%q is not a rupee amount (use digits, optionally with up to 6 decimals, e.g. 0.02)", s)
	}
	if len(whole) > 12 { // ₹10^12 keeps micro-rupees well inside int64
		return 0, fmt.Errorf("%q is too large", s)
	}
	w, _ := strconv.ParseInt(whole, 10, 64)
	f := int64(0)
	if frac != "" {
		f, _ = strconv.ParseInt(frac+strings.Repeat("0", 6-len(frac)), 10, 64)
	}
	return Micro(w)*MicroPerRupee + Micro(f), nil
}

// Decimal formats the exact amount in rupees with at least 2 and at most 6
// decimals and no symbol, e.g. "0.02", "499.00", "0.000150" → "0.00015".
// It is the inverse of ParseRupees for non-negative amounts.
func (m Micro) Decimal() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	frac := strings.TrimRight(fmt.Sprintf("%06d", int64(m%MicroPerRupee)), "0")
	for len(frac) < 2 {
		frac += "0"
	}
	return fmt.Sprintf("%s%d.%s", sign, int64(m/MicroPerRupee), frac)
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
