package service

import (
	"context"
	"testing"
	"time"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
)

type fixture struct {
	m       *mem
	orgs    *OrgService
	keys    *KeyService
	subs    *SubscriptionService
	billing *BillingService
}

func setup() fixture {
	m := newMem()
	clock := fixedClock{time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)}
	return fixture{
		m:       m,
		orgs:    &OrgService{Tx: m, Orgs: memOrgs{m}, Audit: memAudit{m}, Balances: memBalances{m}, Log: quiet()},
		keys:    &KeyService{Tx: m, Orgs: memOrgs{m}, Keys: memKeys{m}, Audit: memAudit{m}, Gen: &seqGen{}, Clock: clock},
		subs:    &SubscriptionService{Tx: m, Orgs: memOrgs{m}, Catalog: memCatalog{m}, Subs: memSubs{m}, Audit: memAudit{m}, Clock: clock},
		billing: &BillingService{Tx: m, Orgs: memOrgs{m}, Ledger: memLedger{m}, Audit: memAudit{m}, Balances: memBalances{m}, Log: quiet()},
	}
}

func TestCreateOrg(t *testing.T) {
	ctx := context.Background()
	f := setup()
	ind, err := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual})
	if err != nil || ind.BillingMode != domain.Prepaid {
		t.Fatalf("%+v %v", ind, err)
	}
	if b, ok := f.m.balances[ind.ID]; !ok || b != 0 {
		t.Fatal("prepaid org must get a zero live balance")
	}
	inst, err := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Acme", Type: domain.OrgInstitution, GSTIN: "29abcde1234f1z5"})
	if err != nil || inst.BillingMode != domain.Postpaid || inst.GSTIN != "29ABCDE1234F1Z5" {
		t.Fatalf("%+v %v", inst, err)
	}
	if _, ok := f.m.balances[inst.ID]; ok {
		t.Fatal("postpaid org must not get a prepaid balance")
	}
	if _, err := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "X", Type: domain.OrgInstitution, GSTIN: "bad"}); codeOf(err) != "INVALID_GSTIN" {
		t.Fatalf("got %v", err)
	}
	if len(f.m.audit) != 2 || f.m.audit[0].Action != "org.created" {
		t.Fatalf("audit: %+v", f.m.audit)
	}
}

func TestCreateOrgIsAtomicWithAudit(t *testing.T) {
	f := setup()
	f.m.failAudit = true
	if _, err := f.orgs.Create(context.Background(), admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual}); err == nil {
		t.Fatal("want error")
	}
	if len(f.m.orgs) != 0 {
		t.Fatal("org must not be saved when the audit record fails")
	}
}

func TestLiveBalanceFailureDoesNotFailCreate(t *testing.T) {
	f := setup()
	f.m.failBalances = true
	if _, err := f.orgs.Create(context.Background(), admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual}); err != nil {
		t.Fatalf("Redis being down must not block sign-up: %v", err)
	}
}

func TestKeys(t *testing.T) {
	ctx := context.Background()
	f := setup()
	org, _ := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual})

	ck, err := f.keys.Create(ctx, admin, org.ID, "production", "ci")
	if err != nil || ck.Plain == "" || ck.Key.Hash != "hash-"+ck.Plain {
		t.Fatalf("%+v %v", ck, err)
	}
	if stored := f.m.keys[ck.Key.ID]; stored.Hash == ck.Plain || stored.Prefix == ck.Plain {
		t.Fatal("plain key must never be stored")
	}
	if _, err := f.keys.Create(ctx, admin, org.ID, "staging", ""); codeOf(err) != "INVALID_ENV" {
		t.Fatalf("got %v", err)
	}
	if _, err := f.keys.Create(ctx, admin, "nope", "sandbox", ""); codeOf(err) != "ORG_NOT_FOUND" {
		t.Fatalf("got %v", err)
	}

	if err := f.keys.Revoke(ctx, admin, ck.Key.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.keys.Revoke(ctx, admin, ck.Key.ID); codeOf(err) != "KEY_NOT_FOUND" {
		t.Fatalf("second revoke: %v", err)
	}
}

func TestKeysForSuspendedOrg(t *testing.T) {
	ctx := context.Background()
	f := setup()
	org, _ := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual})
	o := f.m.orgs[org.ID]
	o.Status = "suspended"
	f.m.orgs[org.ID] = o
	if _, err := f.keys.Create(ctx, admin, org.ID, "production", ""); codeOf(err) != "ORG_INACTIVE" {
		t.Fatalf("got %v", err)
	}
}

func TestSubscribe(t *testing.T) {
	ctx := context.Background()
	f := setup()
	org, _ := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual})
	f.m.apis["weather"] = domain.API{ID: "api1", Slug: "weather"}
	f.m.plans["p1"] = domain.Plan{ID: "p1", APIID: "api1"}
	f.m.plans["p2"] = domain.Plan{ID: "p2", APIID: "api1"}
	f.m.plans["other"] = domain.Plan{ID: "other", APIID: "api2"}

	s, err := f.subs.Subscribe(ctx, admin, org.ID, "weather", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if s.PeriodStart != time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) || s.PeriodEnd != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("period %v–%v", s.PeriodStart, s.PeriodEnd)
	}
	s2, _ := f.subs.Subscribe(ctx, admin, org.ID, "weather", "p2")
	if s2.ID != s.ID || len(f.m.subs) != 1 {
		t.Fatal("changing plan must update the existing subscription")
	}
	if _, err := f.subs.Subscribe(ctx, admin, org.ID, "weather", "other"); codeOf(err) != "PLAN_NOT_FOUND" {
		t.Fatalf("plan of another API: %v", err)
	}
}

func TestTopUp(t *testing.T) {
	ctx := context.Background()
	f := setup()
	ind, _ := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual})
	inst, _ := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Acme", Type: domain.OrgInstitution, GSTIN: "29ABCDE1234F1Z5"})

	_, bal, err := f.billing.TopUp(ctx, admin, ind.ID, 100, "pay_1")
	if err != nil || bal != money.FromRupees(100) {
		t.Fatalf("bal %v err %v", bal, err)
	}
	if _, _, err := f.billing.TopUp(ctx, admin, ind.ID, 100, "pay_1"); codeOf(err) != "DUPLICATE_POSTING" {
		t.Fatalf("same payment twice must be rejected: %v", err)
	}
	if f.m.balances[ind.ID] != money.FromRupees(100) {
		t.Fatal("rejected duplicate must not change the live balance")
	}
	if _, _, err := f.billing.TopUp(ctx, admin, inst.ID, 100, "pay_2"); codeOf(err) != "NOT_PREPAID" {
		t.Fatalf("postpaid top-up: %v", err)
	}
	for _, amt := range []int64{0, -5, 1_000_001} {
		if _, _, err := f.billing.TopUp(ctx, admin, ind.ID, amt, "pay_x"); codeOf(err) != "INVALID_AMOUNT" {
			t.Fatalf("amount %d: %v", amt, err)
		}
	}
	if _, _, err := f.billing.TopUp(ctx, admin, ind.ID, 5, ""); codeOf(err) != "PAYMENT_REF_REQUIRED" {
		t.Fatalf("missing ref: %v", err)
	}
}

func TestTopUpSurvivesRedisOutageAndRebuildRestores(t *testing.T) {
	ctx := context.Background()
	f := setup()
	ind, _ := f.orgs.Create(ctx, admin, CreateOrgInput{Name: "Asha", Type: domain.OrgIndividual})
	f.m.failBalances = true
	if _, _, err := f.billing.TopUp(ctx, admin, ind.ID, 50, "pay_1"); err != nil {
		t.Fatalf("ledger committed, so the top-up succeeds: %v", err)
	}
	f.m.failBalances = false
	delete(f.m.balances, ind.ID) // e.g. Redis restarted empty
	n, err := f.billing.RebuildBalances(ctx)
	if err != nil || n != 1 || f.m.balances[ind.ID] != money.FromRupees(50) {
		t.Fatalf("restored=%d bal=%v err=%v", n, f.m.balances[ind.ID], err)
	}
	if n, _ := f.billing.RebuildBalances(ctx); n != 0 {
		t.Fatal("rebuild must not overwrite an existing live balance")
	}
}

func TestBalanceNotFound(t *testing.T) {
	if _, err := setup().billing.Balance(context.Background(), "nope"); codeOf(err) != "NO_BALANCE" {
		t.Fatalf("got %v", err)
	}
}

func TestCatalogGetPublished(t *testing.T) {
	ctx := context.Background()
	m := newMem()
	lm := newListingMem(m)
	m.apis["ifsc"] = domain.API{ID: "api-1", Slug: "ifsc"}
	m.plans["p1"] = domain.Plan{ID: "p1", APIID: "api-1"}
	lm.endpoints["e1"] = domain.Endpoint{ID: "e1", APIID: "api-1", Method: "GET", Path: "/{code}"}
	lm.endpoints["e2"] = domain.Endpoint{ID: "e2", APIID: "other", Method: "GET", Path: "/"}
	s := &CatalogService{Catalog: memCatalog{m}, Endpoints: memEndpoints{lm}}
	d, err := s.GetPublished(ctx, "ifsc")
	if err != nil || len(d.Plans) != 1 || len(d.Endpoints) != 1 || d.Endpoints[0].ID != "e1" {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := s.GetPublished(ctx, "nope"); codeOf(err) != "API_NOT_FOUND" {
		t.Fatal(err)
	}
}
