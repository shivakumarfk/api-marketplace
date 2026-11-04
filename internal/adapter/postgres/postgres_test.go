package postgres

// Integration tests against a real PostgreSQL. Set TEST_DATABASE_URL, e.g.
//   postgres://apimarket:apimarket@localhost:5432/apimarket?sslmode=disable
// Each run uses a fresh schema with the real migration applied; tests skip
// if the variable is unset.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
)

func testDB(t *testing.T) DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	b := make([]byte, 4)
	rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })

	cfg, _ := pgxpool.ParseConfig(url)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	files, err := filepath.Glob("../../../migrations/postgres/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}
	sort.Strings(files)
	for _, f := range files {
		ddl, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(ddl)); err != nil {
			t.Fatalf("migration %s: %v", filepath.Base(f), err)
		}
	}
	return DB{Pool: pool}
}

func mustOrg(t *testing.T, db DB, typ domain.OrgType, gstin string) domain.Org {
	t.Helper()
	o, err := domain.NewOrg("Test "+string(typ), typ, gstin)
	if err != nil {
		t.Fatal(err)
	}
	o, err = OrgRepo{db}.Create(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOrgRepo(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	o := mustOrg(t, db, domain.OrgInstitution, "29ABCDE1234F1Z5")
	got, err := OrgRepo{db}.Get(ctx, o.ID)
	if err != nil || got.GSTIN != "29ABCDE1234F1Z5" || got.BillingMode != domain.Postpaid {
		t.Fatalf("%+v %v", got, err)
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
		if _, err := (OrgRepo{db}).Get(ctx, id); err != domain.ErrNotFound {
			t.Errorf("Get(%q): %v", id, err)
		}
	}
	// The database enforces the account rules even if a caller bypasses domain.NewOrg.
	_, err = OrgRepo{db}.Create(ctx, domain.Org{Name: "x", Type: domain.OrgIndividual, BillingMode: domain.Postpaid, Status: "active"})
	if e, ok := domain.AsError(err); !ok || e.Code != "CONSTRAINT_VIOLATION" {
		t.Fatalf("individual+postpaid must be rejected by the DB: %v", err)
	}
}

func TestKeyRepoAndSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	o := mustOrg(t, db, domain.OrgIndividual, "")
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	k, err := KeyRepo{db}.Create(ctx, domain.APIKey{OrgID: o.ID, Env: domain.EnvProduction, Prefix: "mk_live_abcd",
		Hash: "h1", Status: "active", ExpiresAt: &exp, IPAllow: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (KeyRepo{db}).Create(ctx, domain.APIKey{OrgID: o.ID, Env: domain.EnvSandbox, Prefix: "x", Hash: "h1", Status: "active"}); err == nil {
		t.Fatal("duplicate hash must be rejected")
	}
	if _, err := (KeyRepo{db}).Create(ctx, domain.APIKey{OrgID: "00000000-0000-0000-0000-000000000000", Env: domain.EnvSandbox, Prefix: "x", Hash: "h2", Status: "active"}); err != domain.ErrNotFound {
		t.Fatalf("unknown org: %v", err)
	}

	snap, err := SnapshotSource{DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := snap.KeysByHash["h1"]
	if !ok || got.ID != k.ID || len(got.IPAllow) != 1 || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("snapshot key: %+v", got)
	}

	if org, err := (KeyRepo{db}).Revoke(ctx, k.ID, time.Now()); err != nil || org != o.ID {
		t.Fatalf("revoke: %v %v", org, err)
	}
	if _, err := (KeyRepo{db}).Revoke(ctx, k.ID, time.Now()); err != domain.ErrNotFound {
		t.Fatalf("second revoke: %v", err)
	}
	snap, _ = SnapshotSource{DB: db}.Load(ctx)
	if _, ok := snap.KeysByHash["h1"]; ok {
		t.Fatal("revoked key must not be in the snapshot")
	}
}

func TestTxManagerRollsBack(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tm := TxManager{db}
	var id string
	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		o, _ := domain.NewOrg("Rollback me", domain.OrgIndividual, "")
		o, err := OrgRepo{db}.Create(ctx, o)
		id = o.ID
		if err != nil {
			return err
		}
		return domain.Invalid("BOOM", "fail after insert")
	})
	if err == nil || id == "" {
		t.Fatalf("expected failure after insert: %v", err)
	}
	if _, err := (OrgRepo{db}).Get(ctx, id); err != domain.ErrNotFound {
		t.Fatal("insert inside a failed transaction must be rolled back")
	}
}

func TestLedgerRepo(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tm, lr := TxManager{db}, LedgerRepo{db}
	o := mustOrg(t, db, domain.OrgIndividual, "")

	post := func(txn domain.Txn) (string, error) {
		var id string
		err := tm.WithinTx(ctx, func(ctx context.Context) (err error) { id, err = lr.Post(ctx, txn); return })
		return id, err
	}
	if _, err := post(domain.TopUpTxn(o.ID, "pay_1", money.FromRupees(100))); err != nil {
		t.Fatal(err)
	}
	if _, err := post(domain.TopUpTxn(o.ID, "pay_2", money.FromRupees(25))); err != nil {
		t.Fatal(err)
	}
	_, err := post(domain.TopUpTxn(o.ID, "pay_1", money.FromRupees(100)))
	if e, ok := domain.AsError(err); !ok || e.Code != "DUPLICATE_POSTING" {
		t.Fatalf("same payment ref twice: %v", err)
	}
	bals, err := lr.PrepaidWalletBalances(ctx)
	if err != nil || bals[o.ID] != money.FromRupees(125) {
		t.Fatalf("balances %v %v", bals, err)
	}

	// The database itself rejects an unbalanced transaction and edits.
	err = tm.WithinTx(ctx, func(ctx context.Context) error {
		var txn string
		if err := db.q(ctx).QueryRow(ctx, `INSERT INTO ledger_txns (ref_type, ref_id) VALUES ('test','x') RETURNING id`).Scan(&txn); err != nil {
			return err
		}
		var acct string
		db.q(ctx).QueryRow(ctx, `SELECT id FROM ledger_accounts LIMIT 1`).Scan(&acct)
		_, err := db.q(ctx).Exec(ctx, `INSERT INTO ledger_entries (txn_id, account_id, amount_micro, direction) VALUES ($1,$2,5,'debit')`, txn, acct)
		return err
	})
	if err == nil {
		t.Fatal("unbalanced transaction must fail at commit")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE ledger_entries SET amount_micro = 1`); err == nil {
		t.Fatal("ledger must be append-only")
	}
}

func TestCatalogSubscriptionsAndAudit(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	o := mustOrg(t, db, domain.OrgIndividual, "")
	var provID, apiID, planID string
	db.Pool.QueryRow(ctx, `INSERT INTO providers (name) VALUES ('P') RETURNING id`).Scan(&provID)
	db.Pool.QueryRow(ctx, `INSERT INTO apis (provider_id, slug, name, base_url, upstream_secret_sealed, status)
		VALUES ($1, 'weather', 'Weather', 'http://p', 'sealed!', 'published') RETURNING id`, provID).Scan(&apiID)
	db.Pool.QueryRow(ctx, `INSERT INTO plans (api_id, name, kind, price_per_call_micro, provider_cost_per_call_micro)
		VALUES ($1, 'PAYG', 'payg', 50000, 30000) RETURNING id`, apiID).Scan(&planID)

	list, err := CatalogRepo{db}.ListPublished(ctx)
	if err != nil || len(list) != 1 || len(list[0].Plans) != 1 || list[0].Plans[0].PricePerCall != 50000 {
		t.Fatalf("%+v %v", list, err)
	}
	if list[0].API.SecretSealed != "" {
		t.Fatal("public listing must not carry the sealed credential")
	}
	if _, _, err := (CatalogRepo{db}).FindPlan(ctx, "weather", "not-a-uuid"); err != domain.ErrNotFound {
		t.Fatalf("bad plan id: %v", err)
	}
	_, plan, err := CatalogRepo{db}.FindPlan(ctx, "weather", planID)
	if err != nil || plan.ProviderCostPerCall != 30000 {
		t.Fatalf("%+v %v", plan, err)
	}

	start, end := domain.MonthlyPeriod(time.Now())
	s1, err := SubscriptionRepo{db}.Upsert(ctx, domain.Subscription{OrgID: o.ID, APIID: apiID, PlanID: planID, PeriodStart: start, PeriodEnd: end})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := SubscriptionRepo{db}.Upsert(ctx, domain.Subscription{OrgID: o.ID, APIID: apiID, PlanID: planID, PeriodStart: start, PeriodEnd: end})
	if err != nil || s2.ID != s1.ID {
		t.Fatalf("upsert must keep one active subscription: %v %v %v", s1.ID, s2.ID, err)
	}

	if err := (AuditRepo{db}).Record(ctx, domain.AuditEvent{OrgID: o.ID, ActorType: "platform_admin", ActorID: "t",
		Action: "key.created", Metadata: map[string]any{"env": "sandbox"}}); err != nil {
		t.Fatal(err)
	}
	var action, env string
	db.Pool.QueryRow(ctx, `SELECT action, metadata->>'env' FROM audit_log`).Scan(&action, &env)
	if action != "key.created" || env != "sandbox" {
		t.Fatalf("audit row: %s %s", action, env)
	}
}

func TestListingRepos(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	now := time.Now().UTC().Truncate(time.Microsecond)

	prov, err := ProviderRepo{db}.Create(ctx, domain.Provider{Name: "Razorpay", AgreementSignedAt: &now, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ProviderRepo{db}.Get(ctx, prov.ID)
	if err != nil || got.Name != "Razorpay" || got.ContactEmail != "" || !got.AgreementSignedAt.Equal(now) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := (ProviderRepo{db}).Get(ctx, "not-a-uuid"); err != domain.ErrNotFound {
		t.Fatal(err)
	}
	unsigned, _ := ProviderRepo{db}.Create(ctx, domain.Provider{Name: "Later", Status: "active"})
	s1, err := ProviderRepo{db}.SignAgreement(ctx, unsigned.ID, now)
	if err != nil || s1.AgreementSignedAt == nil || !s1.AgreementSignedAt.Equal(now) {
		t.Fatalf("%+v %v", s1, err)
	}
	if s2, _ := (ProviderRepo{db}).SignAgreement(ctx, unsigned.ID, now.Add(time.Hour)); !s2.AgreementSignedAt.Equal(now) {
		t.Fatal("re-signing keeps the first date")
	}
	if _, err := (ProviderRepo{db}).SignAgreement(ctx, "00000000-0000-0000-0000-000000000000", now); err != domain.ErrNotFound {
		t.Fatal(err)
	}
	if list, _ := (ProviderRepo{db}).List(ctx); len(list) != 2 {
		t.Fatal(list)
	}

	apis := APIRepo{db}
	a, err := apis.Create(ctx, domain.API{ProviderID: prov.ID, Slug: "ifsc", Name: "IFSC", Category: "finance",
		BaseURL: "https://ifsc.razorpay.com", AuthType: "none", Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apis.Create(ctx, domain.API{ProviderID: prov.ID, Slug: "ifsc", Name: "dup", BaseURL: "http://x", AuthType: "none", Status: "draft"}); err == nil {
		t.Fatal("duplicate slug")
	} else if e, ok := domain.AsError(err); !ok || e.Kind != domain.KindConflict {
		t.Fatalf("duplicate slug must be a conflict: %v", err)
	}
	if _, err := apis.Create(ctx, domain.API{ProviderID: "00000000-0000-0000-0000-000000000000", Slug: "orphan", Name: "o", BaseURL: "http://x", AuthType: "none", Status: "draft"}); err != domain.ErrNotFound {
		t.Fatalf("unknown provider: %v", err)
	}

	a.AuthType, a.AuthHeader, a.SecretSealed, a.UpstreamRPS, a.MockURL = "api_key", "X-Key", "sealed!", 5, "http://mock"
	if err := apis.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := apis.SetStatus(ctx, a.ID, "published"); err != nil {
		t.Fatal(err)
	}
	g, err := apis.GetBySlug(ctx, "ifsc")
	if err != nil || g.SecretSealed != "sealed!" || g.AuthHeader != "X-Key" || g.UpstreamRPS != 5 || g.MockURL != "http://mock" || g.Status != "published" {
		t.Fatalf("%+v %v", g, err)
	}
	if g2, err := apis.GetByID(ctx, a.ID); err != nil || g2.Slug != "ifsc" {
		t.Fatalf("%+v %v", g2, err)
	}
	a.AuthType, a.AuthHeader, a.SecretSealed, a.UpstreamRPS, a.MockURL = "none", "", "", 0, ""
	apis.Update(ctx, a)
	g, _ = apis.GetBySlug(ctx, "ifsc")
	if g.SecretSealed != "" || g.AuthHeader != "" || g.UpstreamRPS != 0 || g.MockURL != "" || g.Status != "published" {
		t.Fatalf("cleared fields, status unchanged: %+v", g)
	}
	if err := apis.Update(ctx, domain.API{ID: "00000000-0000-0000-0000-000000000000", AuthType: "none"}); err != domain.ErrNotFound {
		t.Fatal(err)
	}
	if err := apis.SetStatus(ctx, a.ID, "bogus"); err == nil {
		t.Fatal("status check constraint")
	}

	plans := PlanRepo{db}
	p1, err := plans.Create(ctx, domain.Plan{APIID: a.ID, Name: "PAYG", Kind: domain.PlanPAYG, PricePerCall: 20_000, RateLimitRPS: 2, Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plans.Create(ctx, domain.Plan{APIID: a.ID, Name: "Loss", Kind: domain.PlanPAYG, PricePerCall: 1, ProviderCostPerCall: 2, Status: "active"}); err == nil {
		t.Fatal("DB must reject negative margin")
	}
	p2, _ := plans.Create(ctx, domain.Plan{APIID: a.ID, Name: "Free", Kind: domain.PlanFree, IncludedCalls: 100, Status: "active"})
	if list, err := plans.ListByAPI(ctx, a.ID); err != nil || len(list) != 2 || list[0].ID != p2.ID {
		t.Fatalf("free plan sorts first: %+v %v", list, err)
	}

	// A retired plan stays in the gateway snapshot while someone is subscribed to it.
	o := mustOrg(t, db, domain.OrgIndividual, "")
	start, end := domain.MonthlyPeriod(time.Now())
	if _, err := (SubscriptionRepo{db}).Upsert(ctx, domain.Subscription{OrgID: o.ID, APIID: a.ID, PlanID: p1.ID, PeriodStart: start, PeriodEnd: end}); err != nil {
		t.Fatal(err)
	}
	plans.SetStatus(ctx, p1.ID, "retired")
	plans.SetStatus(ctx, p2.ID, "retired")
	if g, _ := plans.Get(ctx, p1.ID); g.Status != "retired" {
		t.Fatal(g)
	}
	snap, err := SnapshotSource{DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snap.Plans[p1.ID]; !ok {
		t.Fatal("retired plan with a subscriber must stay in the snapshot")
	}
	if _, ok := snap.Plans[p2.ID]; ok {
		t.Fatal("retired plan without subscribers must not be in the snapshot")
	}
	if _, _, err := (CatalogRepo{db}).FindPlan(ctx, "ifsc", p1.ID); err != domain.ErrNotFound {
		t.Fatal("retired plans accept no new subscriptions")
	}
}

func TestEndpointRepoAndUpstreamHeaders(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	var provID string
	db.Pool.QueryRow(ctx, `INSERT INTO providers (name) VALUES ('P') RETURNING id`).Scan(&provID)
	a, err := APIRepo{db}.Create(ctx, domain.API{ProviderID: provID, Slug: "ifsc", Name: "IFSC", BaseURL: "https://x",
		AuthType: "query", AuthHeader: "appid", SecretSealed: "s", Status: "published",
		UpstreamHeaders: []domain.Header{{Name: "X-Client-Id", Value: "acme"}}})
	if err != nil {
		t.Fatal(err)
	}
	g, err := APIRepo{db}.GetBySlug(ctx, "ifsc")
	if err != nil || len(g.UpstreamHeaders) != 1 || g.UpstreamHeaders[0].Value != "acme" || g.AuthType != "query" {
		t.Fatalf("%+v %v", g, err)
	}
	g.UpstreamHeaders = nil
	if err := (APIRepo{db}).Update(ctx, g); err != nil {
		t.Fatal(err)
	}
	var raw string
	db.Pool.QueryRow(ctx, `SELECT upstream_headers::text FROM apis WHERE id = $1`, a.ID).Scan(&raw)
	if raw != "[]" {
		t.Fatalf("nil headers must be stored as [], got %s", raw)
	}

	eps := EndpointRepo{db}
	e, err := eps.Create(ctx, domain.Endpoint{APIID: a.ID, Method: "GET", Path: "/{code}", Summary: "Branch",
		Params:  []domain.Param{{Name: "code", In: "path", Required: true, Example: "HDFC0CAGSBK"}},
		Headers: []domain.DocHeader{{Name: "Accept-Language"}}, ResponseStatus: 200, ResponseContentType: "application/json",
		ResponseExample: `{"BANK":"HDFC Bank"}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eps.Create(ctx, domain.Endpoint{APIID: a.ID, Method: "GET", Path: "/{code}", ResponseStatus: 200, ResponseContentType: "application/json"}); err == nil {
		t.Fatal("method+path must be unique per API")
	} else if de, ok := domain.AsError(err); !ok || de.Kind != domain.KindConflict {
		t.Fatalf("conflict expected: %v", err)
	}
	eps.Create(ctx, domain.Endpoint{APIID: a.ID, Method: "POST", Path: "/{code}", ResponseStatus: 201, ResponseContentType: "application/json"})
	eps.Create(ctx, domain.Endpoint{APIID: a.ID, Method: "GET", Path: "/", ResponseStatus: 200, ResponseContentType: "text/plain"})
	list, err := eps.ListByAPI(ctx, a.ID)
	if err != nil || len(list) != 3 || list[0].Path != "/" || list[1].Method != "GET" || list[2].Method != "POST" {
		t.Fatalf("ordered by path then method: %+v %v", list, err)
	}
	got, err := eps.Get(ctx, e.ID)
	if err != nil || got.Params[0].Example != "HDFC0CAGSBK" || got.Headers[0].Name != "Accept-Language" || got.ResponseExample != `{"BANK":"HDFC Bank"}` {
		t.Fatalf("%+v %v", got, err)
	}
	var idem bool
	db.Pool.QueryRow(ctx, `SELECT idempotent FROM api_endpoints WHERE id = $1`, e.ID).Scan(&idem)
	if !idem {
		t.Fatal("GET endpoints are stored as idempotent")
	}
	got.Summary, got.Params = "Updated", nil
	if err := eps.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	if g, _ := eps.Get(ctx, e.ID); g.Summary != "Updated" || len(g.Params) != 0 {
		t.Fatalf("%+v", g)
	}
	if err := eps.Delete(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := eps.Delete(ctx, e.ID); err != domain.ErrNotFound {
		t.Fatal(err)
	}

	// Public detail: published only, credential stripped.
	if _, err := db.Pool.Exec(ctx, `INSERT INTO plans (api_id, name, kind, price_per_call_micro) VALUES ($1, 'PAYG', 'payg', 20000)`, a.ID); err != nil {
		t.Fatal(err)
	}
	pub, err := CatalogRepo{db}.GetPublished(ctx, "ifsc")
	if err != nil || pub.API.SecretSealed != "" || len(pub.Plans) != 1 {
		t.Fatalf("%+v %v", pub, err)
	}
	APIRepo{db}.SetStatus(ctx, a.ID, "suspended")
	if _, err := (CatalogRepo{db}).GetPublished(ctx, "ifsc"); err != domain.ErrNotFound {
		t.Fatalf("suspended is not public: %v", err)
	}
}
