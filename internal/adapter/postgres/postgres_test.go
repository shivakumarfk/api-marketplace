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
	ddl, err := os.ReadFile("../../../migrations/postgres/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatalf("migration: %v", err)
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
