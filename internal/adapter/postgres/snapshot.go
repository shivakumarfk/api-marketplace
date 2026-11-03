package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"apimarket/internal/domain"
	"apimarket/internal/domain/catalog"
)

// SecretOpener decrypts sealed upstream credentials (local key or KMS).
type SecretOpener interface {
	Open(sealed string) (string, error)
}

// SnapshotSource loads the gateway's read model in one pass.
type SnapshotSource struct {
	DB      DB
	Secrets SecretOpener
	Log     *slog.Logger
}

func (s SnapshotSource) Load(ctx context.Context) (*catalog.Snapshot, error) {
	snap := catalog.Empty()
	snap.LoadedAt = time.Now()
	q := s.DB.q(ctx)

	rows, err := q.Query(ctx, `
		SELECT id, org_id, env, name, prefix, secret_hash, COALESCE(ip_allowlist, '{}')::text[], expires_at, status
		FROM api_keys WHERE status IN ('active', 'rotating')`)
	if err != nil {
		return nil, fmt.Errorf("keys: %w", err)
	}
	for rows.Next() {
		var k domain.APIKey
		var allow []string
		if err := rows.Scan(&k.ID, &k.OrgID, &k.Env, &k.Name, &k.Prefix, &k.Hash, &allow, &k.ExpiresAt, &k.Status); err != nil {
			rows.Close()
			return nil, err
		}
		for _, a := range allow {
			if p, err := netip.ParsePrefix(a); err == nil {
				k.IPAllow = append(k.IPAllow, p.Masked())
			}
		}
		snap.KeysByHash[k.Hash] = k
	}
	rows.Close()

	rows, err = q.Query(ctx, `SELECT id, name, type, billing_mode, COALESCE(gstin, ''), status FROM organizations`)
	if err != nil {
		return nil, fmt.Errorf("orgs: %w", err)
	}
	for rows.Next() {
		var o domain.Org
		if err := rows.Scan(&o.ID, &o.Name, &o.Type, &o.BillingMode, &o.GSTIN, &o.Status); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Orgs[o.ID] = o
	}
	rows.Close()

	rows, err = q.Query(ctx, `SELECT `+apiCols+` FROM apis a WHERE a.status = 'published'`)
	if err != nil {
		return nil, fmt.Errorf("apis: %w", err)
	}
	for rows.Next() {
		var a catalog.API
		if err := scanAPI(rows, &a.API); err != nil {
			rows.Close()
			return nil, err
		}
		if a.SecretSealed != "" {
			pt, err := s.Secrets.Open(a.SecretSealed)
			if err != nil {
				s.Log.Error("cannot decrypt upstream credential; API skipped", "api", a.Slug)
				continue
			}
			a.Secret = pt
		}
		snap.APIsBySlug[a.Slug] = a
	}
	rows.Close()

	rows, err = q.Query(ctx, `SELECT `+planCols+` FROM plans p WHERE p.status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("plans: %w", err)
	}
	for rows.Next() {
		var p domain.Plan
		if err := rows.Scan(planDest(&p)...); err != nil {
			rows.Close()
			return nil, err
		}
		snap.Plans[p.ID] = p
	}
	rows.Close()

	rows, err = q.Query(ctx, `
		SELECT id, org_id, api_id, plan_id, status, period_start, period_end
		FROM subscriptions WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sub domain.Subscription
		if err := rows.Scan(&sub.ID, &sub.OrgID, &sub.APIID, &sub.PlanID, &sub.Status, &sub.PeriodStart, &sub.PeriodEnd); err != nil {
			return nil, err
		}
		snap.Subs[catalog.SubKey(sub.OrgID, sub.APIID)] = sub
	}
	return snap, rows.Err()
}
