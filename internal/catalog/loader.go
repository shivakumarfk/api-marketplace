package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"apimarket/internal/secrets"
)

// Loader builds Snapshots from PostgreSQL and keeps the latest one.
type Loader struct {
	db  *pgxpool.Pool
	box *secrets.Box
	log *slog.Logger
	cur atomic.Pointer[Snapshot]
}

func NewLoader(db *pgxpool.Pool, box *secrets.Box, log *slog.Logger) *Loader {
	l := &Loader{db: db, box: box, log: log}
	l.cur.Store(Empty())
	return l
}

// Current returns the latest snapshot (never nil).
func (l *Loader) Current() *Snapshot { return l.cur.Load() }

// Run refreshes the snapshot every interval until ctx ends. A failed refresh
// keeps serving the previous snapshot.
func (l *Loader) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := l.Refresh(ctx); err != nil {
				l.log.Error("catalog refresh failed; serving previous snapshot", "err", err)
			}
		}
	}
}

func (l *Loader) Refresh(ctx context.Context) error {
	s, err := l.load(ctx)
	if err != nil {
		return err
	}
	l.cur.Store(s)
	return nil
}

func (l *Loader) load(ctx context.Context) (*Snapshot, error) {
	s := Empty()
	s.LoadedAt = time.Now()

	rows, err := l.db.Query(ctx, `
		SELECT id, org_id, env, status, expires_at, COALESCE(ip_allowlist, '{}')::text[], secret_hash
		FROM api_keys WHERE status IN ('active','rotating')`)
	if err != nil {
		return nil, fmt.Errorf("keys: %w", err)
	}
	for rows.Next() {
		var k Key
		var allow []string
		var hash string
		if err := rows.Scan(&k.ID, &k.OrgID, &k.Env, &k.Status, &k.ExpiresAt, &allow, &hash); err != nil {
			rows.Close()
			return nil, err
		}
		for _, a := range allow {
			if p, err := netip.ParsePrefix(a); err == nil {
				k.IPAllow = append(k.IPAllow, p.Masked())
			}
		}
		s.KeysByHash[hash] = k
	}
	rows.Close()

	rows, err = l.db.Query(ctx, `SELECT id, type, billing_mode, status FROM organizations`)
	if err != nil {
		return nil, fmt.Errorf("orgs: %w", err)
	}
	for rows.Next() {
		var o Org
		if err := rows.Scan(&o.ID, &o.Type, &o.BillingMode, &o.Status); err != nil {
			rows.Close()
			return nil, err
		}
		s.Orgs[o.ID] = o
	}
	rows.Close()

	rows, err = l.db.Query(ctx, `
		SELECT id, slug, base_url, COALESCE(mock_url,''), upstream_auth_type,
		       COALESCE(upstream_auth_header,''), COALESCE(upstream_secret_sealed,''),
		       COALESCE(upstream_rate_limit_rps, 0), status
		FROM apis WHERE status = 'published'`)
	if err != nil {
		return nil, fmt.Errorf("apis: %w", err)
	}
	for rows.Next() {
		var a API
		var sealed string
		if err := rows.Scan(&a.ID, &a.Slug, &a.BaseURL, &a.MockURL, &a.AuthType, &a.AuthHeader, &sealed, &a.UpstreamRPS, &a.Status); err != nil {
			rows.Close()
			return nil, err
		}
		if sealed != "" {
			pt, err := l.box.Open(sealed)
			if err != nil {
				l.log.Error("cannot decrypt upstream credential; API skipped", "api", a.Slug)
				continue
			}
			a.Secret = pt
		}
		s.APIsBySlug[a.Slug] = a
	}
	rows.Close()

	rows, err = l.db.Query(ctx, `
		SELECT id, kind, included_calls, price_per_call_micro, provider_cost_per_call_micro, rate_limit_rps
		FROM plans WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("plans: %w", err)
	}
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Kind, &p.IncludedCalls, &p.PricePerCallMicro, &p.ProviderCostMicro, &p.RateLimitRPS); err != nil {
			rows.Close()
			return nil, err
		}
		s.Plans[p.ID] = p
	}
	rows.Close()

	rows, err = l.db.Query(ctx, `
		SELECT id, org_id, api_id, plan_id, status, period_start, period_end
		FROM subscriptions WHERE status = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("subscriptions: %w", err)
	}
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.OrgID, &sub.APIID, &sub.PlanID, &sub.Status, &sub.PeriodStart, &sub.PeriodEnd); err != nil {
			rows.Close()
			return nil, err
		}
		s.Subs[SubKey(sub.OrgID, sub.APIID)] = sub
	}
	rows.Close()
	return s, rows.Err()
}
