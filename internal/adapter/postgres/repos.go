package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
	"apimarket/internal/service"
)

// Compile-time checks that the adapters satisfy the service ports.
var (
	_ service.TxManager        = TxManager{}
	_ service.OrgRepo          = OrgRepo{}
	_ service.KeyRepo          = KeyRepo{}
	_ service.CatalogRepo      = CatalogRepo{}
	_ service.SubscriptionRepo = SubscriptionRepo{}
	_ service.LedgerRepo       = LedgerRepo{}
	_ service.AuditRepo        = AuditRepo{}
)

// ---------- orgs ----------

type OrgRepo struct{ DB DB }

func (r OrgRepo) Create(ctx context.Context, o domain.Org) (domain.Org, error) {
	err := r.DB.q(ctx).QueryRow(ctx, `
		INSERT INTO organizations (name, type, billing_mode, gstin, status)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5) RETURNING id, created_at`,
		o.Name, o.Type, o.BillingMode, o.GSTIN, o.Status).Scan(&o.ID, &o.CreatedAt)
	return o, mapErr(err)
}

func (r OrgRepo) Get(ctx context.Context, id string) (domain.Org, error) {
	var o domain.Org
	var gstin *string
	err := r.DB.q(ctx).QueryRow(ctx, `
		SELECT id, name, type, billing_mode, gstin, status, created_at FROM organizations WHERE id = $1`, id).
		Scan(&o.ID, &o.Name, &o.Type, &o.BillingMode, &gstin, &o.Status, &o.CreatedAt)
	if gstin != nil {
		o.GSTIN = *gstin
	}
	return o, mapErr(err)
}

// ---------- keys ----------

type KeyRepo struct{ DB DB }

func (r KeyRepo) Create(ctx context.Context, k domain.APIKey) (domain.APIKey, error) {
	var allow []string
	for _, p := range k.IPAllow {
		allow = append(allow, p.String())
	}
	err := r.DB.q(ctx).QueryRow(ctx, `
		INSERT INTO api_keys (org_id, env, name, prefix, secret_hash, ip_allowlist, expires_at, status)
		VALUES ($1, $2, $3, $4, $5, $6::cidr[], $7, $8) RETURNING id, created_at`,
		k.OrgID, k.Env, k.Name, k.Prefix, k.Hash, allow, k.ExpiresAt, k.Status).Scan(&k.ID, &k.CreatedAt)
	return k, mapErr(err)
}

func (r KeyRepo) Revoke(ctx context.Context, keyID string, at time.Time) (string, error) {
	var orgID string
	err := r.DB.q(ctx).QueryRow(ctx, `
		UPDATE api_keys SET status = 'revoked', revoked_at = $2
		WHERE id = $1 AND status <> 'revoked' RETURNING org_id`, keyID, at).Scan(&orgID)
	return orgID, mapErr(err)
}

// ---------- catalog ----------

type CatalogRepo struct{ DB DB }

const apiCols = `a.id, a.provider_id, a.slug, a.name, a.description, a.category, a.base_url,
	COALESCE(a.mock_url, ''), a.upstream_auth_type, COALESCE(a.upstream_auth_header, ''),
	COALESCE(a.upstream_secret_sealed, ''), COALESCE(a.upstream_rate_limit_rps, 0)::float8, a.status, a.upstream_headers`

const planCols = `p.id, p.api_id, p.name, p.kind, p.monthly_fee_micro, p.included_calls,
	p.price_per_call_micro, p.provider_cost_per_call_micro, p.rate_limit_rps, p.status`

type scanner interface{ Scan(dest ...any) error }

func scanAPI(s scanner, a *domain.API, extra ...any) error {
	return s.Scan(append([]any{&a.ID, &a.ProviderID, &a.Slug, &a.Name, &a.Description, &a.Category, &a.BaseURL,
		&a.MockURL, &a.AuthType, &a.AuthHeader, &a.SecretSealed, &a.UpstreamRPS, &a.Status, &a.UpstreamHeaders}, extra...)...)
}

func planDest(p *domain.Plan) []any {
	return []any{&p.ID, &p.APIID, &p.Name, &p.Kind, (*int64)(&p.MonthlyFee), &p.IncludedCalls,
		(*int64)(&p.PricePerCall), (*int64)(&p.ProviderCostPerCall), &p.RateLimitRPS, &p.Status}
}

func (r CatalogRepo) ListPublished(ctx context.Context) ([]service.CatalogEntry, error) {
	rows, err := r.DB.q(ctx).Query(ctx, `SELECT `+apiCols+`, `+planCols+`
		FROM apis a JOIN plans p ON p.api_id = a.id AND p.status = 'active'
		WHERE a.status = 'published' ORDER BY a.name, p.monthly_fee_micro, p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []service.CatalogEntry
	idx := map[string]int{}
	for rows.Next() {
		var a domain.API
		var p domain.Plan
		if err := scanAPI(rows, &a, planDest(&p)...); err != nil {
			return nil, err
		}
		i, ok := idx[a.ID]
		if !ok {
			a.SecretSealed = "" // never leaves the adapter for public listing
			out = append(out, service.CatalogEntry{API: a})
			i = len(out) - 1
			idx[a.ID] = i
		}
		out[i].Plans = append(out[i].Plans, p)
	}
	return out, rows.Err()
}

// GetPublished returns one published API (credential stripped) with its active plans.
func (r CatalogRepo) GetPublished(ctx context.Context, slug string) (service.CatalogEntry, error) {
	rows, err := r.DB.q(ctx).Query(ctx, `SELECT `+apiCols+`, `+planCols+`
		FROM apis a JOIN plans p ON p.api_id = a.id AND p.status = 'active'
		WHERE a.status = 'published' AND a.slug = $1 ORDER BY p.monthly_fee_micro, p.name`, slug)
	if err != nil {
		return service.CatalogEntry{}, err
	}
	defer rows.Close()
	var out service.CatalogEntry
	for rows.Next() {
		var a domain.API
		var p domain.Plan
		if err := scanAPI(rows, &a, planDest(&p)...); err != nil {
			return service.CatalogEntry{}, err
		}
		a.SecretSealed = "" // never leaves the adapter for public views
		out.API = a
		out.Plans = append(out.Plans, p)
	}
	if err := rows.Err(); err != nil {
		return service.CatalogEntry{}, err
	}
	if out.API.ID == "" {
		return service.CatalogEntry{}, domain.ErrNotFound
	}
	return out, nil
}

func (r CatalogRepo) FindPlan(ctx context.Context, apiSlug, planID string) (domain.API, domain.Plan, error) {
	var a domain.API
	var p domain.Plan
	row := r.DB.q(ctx).QueryRow(ctx, `SELECT `+apiCols+`, `+planCols+`
		FROM apis a JOIN plans p ON p.api_id = a.id
		WHERE a.slug = $1 AND a.status = 'published' AND p.id = $2 AND p.status = 'active'`, apiSlug, planID)
	err := scanAPI(row, &a, planDest(&p)...)
	return a, p, mapErr(err)
}

// ---------- subscriptions ----------

type SubscriptionRepo struct{ DB DB }

func (r SubscriptionRepo) Upsert(ctx context.Context, s domain.Subscription) (domain.Subscription, error) {
	err := r.DB.q(ctx).QueryRow(ctx, `
		INSERT INTO subscriptions (org_id, api_id, plan_id, status, period_start, period_end, created_by)
		VALUES ($1, $2, $3, 'active', $4, $5, $6)
		ON CONFLICT (org_id, api_id) WHERE status = 'active' DO UPDATE SET plan_id = EXCLUDED.plan_id
		RETURNING id, status, period_start, period_end`,
		s.OrgID, s.APIID, s.PlanID, s.PeriodStart, s.PeriodEnd, s.CreatedBy).
		Scan(&s.ID, &s.Status, &s.PeriodStart, &s.PeriodEnd)
	return s, mapErr(err)
}

// ---------- ledger ----------

type LedgerRepo struct{ DB DB }

// Post writes the transaction. The database also enforces that it balances
// (deferred constraint trigger) and that ledger rows are append-only.
func (r LedgerRepo) Post(ctx context.Context, t domain.Txn) (string, error) {
	if !t.Balanced() {
		return "", domain.Invalid("UNBALANCED", "ledger transaction does not balance")
	}
	q := r.DB.q(ctx)
	var txnID string
	err := q.QueryRow(ctx, `INSERT INTO ledger_txns (ref_type, ref_id) VALUES ($1, NULLIF($2, '')) RETURNING id`,
		t.RefType, t.RefID).Scan(&txnID)
	if err != nil {
		err = mapErr(err)
		if e, ok := domain.AsError(err); ok && e.Kind == domain.KindConflict {
			return "", domain.Conflict("DUPLICATE_POSTING", "this payment or event has already been posted")
		}
		return "", err
	}
	for _, e := range t.Entries {
		var acct string
		if err := q.QueryRow(ctx, `
			INSERT INTO ledger_accounts (owner_type, owner_id, kind) VALUES ($1, $2, $3)
			ON CONFLICT (owner_type, owner_id, kind) DO UPDATE SET kind = EXCLUDED.kind RETURNING id`,
			e.Account.OwnerType, e.Account.OwnerID, e.Account.Kind).Scan(&acct); err != nil {
			return "", err
		}
		if _, err := q.Exec(ctx, `INSERT INTO ledger_entries (txn_id, account_id, amount_micro, direction) VALUES ($1, $2, $3, $4)`,
			txnID, acct, int64(e.Amount), e.Direction); err != nil {
			return "", err
		}
	}
	return txnID, nil
}

func (r LedgerRepo) PrepaidWalletBalances(ctx context.Context) (map[string]money.Micro, error) {
	rows, err := r.DB.q(ctx).Query(ctx, `
		SELECT o.id, COALESCE(SUM(CASE e.direction WHEN 'credit' THEN e.amount_micro ELSE -e.amount_micro END), 0)
		FROM organizations o
		LEFT JOIN ledger_accounts a ON a.owner_type = 'org' AND a.owner_id = o.id::text AND a.kind = 'wallet'
		LEFT JOIN ledger_entries e ON e.account_id = a.id
		WHERE o.billing_mode = 'prepaid' GROUP BY o.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]money.Micro{}
	for rows.Next() {
		var id string
		var b int64
		if err := rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		out[id] = money.Micro(b)
	}
	return out, rows.Err()
}

// ---------- audit ----------

type AuditRepo struct{ DB DB }

func (r AuditRepo) Record(ctx context.Context, e domain.AuditEvent) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	if e.Metadata == nil {
		meta = []byte("{}")
	}
	_, err = r.DB.q(ctx).Exec(ctx, `
		INSERT INTO audit_log (org_id, actor_type, actor_id, action, target_type, target_id, metadata)
		VALUES (NULLIF($1, '')::uuid, $2, $3, $4, $5, $6, $7)`,
		e.OrgID, e.ActorType, e.ActorID, e.Action, e.TargetType, e.TargetID, meta)
	if errors.Is(mapErr(err), domain.ErrNotFound) {
		return errors.New("audit: invalid org id")
	}
	return err
}
