package domain

import (
	"math/rand"
	"net/netip"
	"testing"
	"time"

	"apimarket/internal/domain/money"
)

func TestNewOrgRules(t *testing.T) {
	o, err := NewOrg(" Asha ", OrgIndividual, "29ABCDE1234F1Z5")
	if err != nil || o.BillingMode != Prepaid || o.GSTIN != "" || o.Name != "Asha" {
		t.Fatalf("individual: %+v %v", o, err)
	}
	o, err = NewOrg("Acme Bank", OrgInstitution, "29abcde1234f1z5")
	if err != nil || o.BillingMode != Postpaid || o.GSTIN != "29ABCDE1234F1Z5" {
		t.Fatalf("institution: %+v %v", o, err)
	}
	for _, c := range []struct {
		name  string
		typ   OrgType
		gstin string
		code  string
	}{
		{"", OrgIndividual, "", "NAME_REQUIRED"},
		{"X", OrgInstitution, "", "INVALID_GSTIN"},
		{"X", OrgInstitution, "29ABCDE1234F1X5", "INVALID_GSTIN"},
		{"X", "partnership", "", "INVALID_TYPE"},
	} {
		_, err := NewOrg(c.name, c.typ, c.gstin)
		if e, ok := AsError(err); !ok || e.Code != c.code || e.Kind != KindInvalid {
			t.Errorf("%+v: got %v, want %s", c, err, c.code)
		}
	}
}

func TestKeyUsableAndAllowlist(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	if !(APIKey{Status: "active"}).Usable(now) || !(APIKey{Status: "rotating", ExpiresAt: &future}).Usable(now) {
		t.Fatal("active / rotating keys must be usable")
	}
	if (APIKey{Status: "revoked"}).Usable(now) || (APIKey{Status: "active", ExpiresAt: &past}).Usable(now) {
		t.Fatal("revoked / expired keys must not be usable")
	}
	k := APIKey{IPAllow: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	if !k.AllowsIP(netip.MustParseAddr("10.1.2.3")) || k.AllowsIP(netip.MustParseAddr("192.168.1.1")) {
		t.Fatal("allowlist")
	}
	if !k.AllowsIP(netip.MustParseAddr("::ffff:10.1.2.3")) {
		t.Fatal("IPv4-mapped IPv6 should match")
	}
}

func TestMonthlyPeriod(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	s, e := MonthlyPeriod(time.Date(2026, 12, 31, 23, 0, 0, 0, time.UTC))
	if s != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) || e != time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("%v %v", s, e)
	}
	// 1 Nov 02:00 IST is still 31 Oct in UTC.
	s, _ = MonthlyPeriod(time.Date(2026, 11, 1, 2, 0, 0, 0, ist))
	if s.Month() != time.October {
		t.Fatalf("periods are UTC: got %v", s)
	}
}

func TestTxnBalanced(t *testing.T) {
	if !TopUpTxn("o1", "pay_1", money.FromRupees(100)).Balanced() {
		t.Fatal("top-up must balance")
	}
	for name, txn := range map[string]Txn{
		"single":    {Entries: []Entry{{Direction: Debit, Amount: 1}}},
		"unequal":   {Entries: []Entry{{Direction: Debit, Amount: 100}, {Direction: Credit, Amount: 99}}},
		"zero":      {Entries: []Entry{{Direction: Debit, Amount: 0}, {Direction: Credit, Amount: 0}}},
		"negative":  {Entries: []Entry{{Direction: Debit, Amount: -5}, {Direction: Credit, Amount: -5}}},
		"direction": {Entries: []Entry{{Direction: Debit, Amount: 5}, {Direction: "sideways", Amount: 5}}},
	} {
		if txn.Balanced() {
			t.Errorf("%s: should not balance", name)
		}
	}
	// Property: any split of an amount into credits balances the debit.
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 5000; i++ {
		total := money.Micro(r.Int63n(1e12) + 2)
		txn := Txn{Entries: []Entry{{Direction: Debit, Amount: total}}}
		for left := total; left > 0; {
			part := money.Micro(r.Int63n(int64(left)) + 1)
			txn.Entries = append(txn.Entries, Entry{Direction: Credit, Amount: part})
			left -= part
		}
		if !txn.Balanced() {
			t.Fatalf("split of %d not balanced", total)
		}
	}
}
