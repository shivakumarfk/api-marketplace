package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"time"

	"apimarket/internal/domain"
	"apimarket/internal/domain/apikey"
	"apimarket/internal/domain/money"
)

// mem is an in-memory implementation of every port. WithinTx snapshots the
// state and restores it if fn fails, so tests can check atomicity.
type mem struct {
	orgs     map[string]domain.Org
	keys     map[string]domain.APIKey
	apis     map[string]domain.API  // by slug
	plans    map[string]domain.Plan // by id
	subs     map[string]domain.Subscription
	txns     map[string]domain.Txn // by ref
	audit    []domain.AuditEvent
	balances map[string]money.Micro // live (Redis) balances
	seq      int

	failAudit    bool
	failBalances bool
}

func newMem() *mem {
	return &mem{orgs: map[string]domain.Org{}, keys: map[string]domain.APIKey{}, apis: map[string]domain.API{},
		plans: map[string]domain.Plan{}, subs: map[string]domain.Subscription{}, txns: map[string]domain.Txn{},
		balances: map[string]money.Micro{}}
}

func (m *mem) id(p string) string { m.seq++; return fmt.Sprintf("%s-%d", p, m.seq) }

func (m *mem) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	orgs, keys, subs, txns, audit := maps.Clone(m.orgs), maps.Clone(m.keys), maps.Clone(m.subs), maps.Clone(m.txns), append([]domain.AuditEvent(nil), m.audit...)
	if err := fn(ctx); err != nil {
		m.orgs, m.keys, m.subs, m.txns, m.audit = orgs, keys, subs, txns, audit
		return err
	}
	return nil
}

// OrgRepo
type memOrgs struct{ *mem }

func (r memOrgs) Create(_ context.Context, o domain.Org) (domain.Org, error) {
	o.ID = r.id("org")
	r.orgs[o.ID] = o
	return o, nil
}
func (r memOrgs) Get(_ context.Context, id string) (domain.Org, error) {
	o, ok := r.orgs[id]
	if !ok {
		return domain.Org{}, domain.ErrNotFound
	}
	return o, nil
}

// KeyRepo
type memKeys struct{ *mem }

func (r memKeys) Create(_ context.Context, k domain.APIKey) (domain.APIKey, error) {
	k.ID = r.id("key")
	r.keys[k.ID] = k
	return k, nil
}
func (r memKeys) Revoke(_ context.Context, id string, _ time.Time) (string, error) {
	k, ok := r.keys[id]
	if !ok || k.Status == "revoked" {
		return "", domain.ErrNotFound
	}
	k.Status = "revoked"
	r.keys[id] = k
	return k.OrgID, nil
}

// CatalogRepo
type memCatalog struct{ *mem }

func (r memCatalog) ListPublished(context.Context) ([]CatalogEntry, error) {
	var out []CatalogEntry
	for _, a := range r.apis {
		e := CatalogEntry{API: a}
		for _, p := range r.plans {
			if p.APIID == a.ID {
				e.Plans = append(e.Plans, p)
			}
		}
		out = append(out, e)
	}
	return out, nil
}
func (r memCatalog) FindPlan(_ context.Context, slug, planID string) (domain.API, domain.Plan, error) {
	a, ok := r.apis[slug]
	p, ok2 := r.plans[planID]
	if !ok || !ok2 || p.APIID != a.ID {
		return domain.API{}, domain.Plan{}, domain.ErrNotFound
	}
	return a, p, nil
}

// SubscriptionRepo
type memSubs struct{ *mem }

func (r memSubs) Upsert(_ context.Context, s domain.Subscription) (domain.Subscription, error) {
	key := s.OrgID + "|" + s.APIID
	if old, ok := r.subs[key]; ok {
		s.ID = old.ID
	} else {
		s.ID = r.id("sub")
	}
	r.subs[key] = s
	return s, nil
}

// LedgerRepo
type memLedger struct{ *mem }

func (r memLedger) Post(_ context.Context, t domain.Txn) (string, error) {
	if !t.Balanced() {
		return "", errors.New("unbalanced")
	}
	ref := t.RefType + "|" + t.RefID
	if _, dup := r.txns[ref]; dup {
		return "", domain.Conflict("DUPLICATE_POSTING", "already posted")
	}
	r.txns[ref] = t
	return r.id("txn"), nil
}
func (r memLedger) PrepaidWalletBalances(context.Context) (map[string]money.Micro, error) {
	out := map[string]money.Micro{}
	for _, o := range r.orgs {
		if o.BillingMode == domain.Prepaid {
			out[o.ID] = 0
		}
	}
	for _, t := range r.txns {
		for _, e := range t.Entries {
			if e.Account.Kind == "wallet" && e.Account.OwnerType == "org" {
				if e.Direction == domain.Credit {
					out[e.Account.OwnerID] += e.Amount
				} else {
					out[e.Account.OwnerID] -= e.Amount
				}
			}
		}
	}
	return out, nil
}

// AuditRepo
type memAudit struct{ *mem }

func (r memAudit) Record(_ context.Context, e domain.AuditEvent) error {
	if r.failAudit {
		return errors.New("audit store down")
	}
	r.audit = append(r.audit, e)
	return nil
}

// BalanceStore
type memBalances struct{ *mem }

func (r memBalances) Add(_ context.Context, org string, a money.Micro) (money.Micro, error) {
	if r.failBalances {
		return 0, errors.New("redis down")
	}
	r.balances[org] += a
	return r.balances[org], nil
}
func (r memBalances) Get(_ context.Context, org string) (money.Micro, error) {
	b, ok := r.balances[org]
	if !ok {
		return 0, domain.ErrNotFound
	}
	return b, nil
}
func (r memBalances) SetIfMissing(_ context.Context, org string, a money.Micro) (bool, error) {
	if r.failBalances {
		return false, errors.New("redis down")
	}
	if _, ok := r.balances[org]; ok {
		return false, nil
	}
	r.balances[org] = a
	return true, nil
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type seqGen struct{ n int }

func (g *seqGen) Generate(env apikey.Env) (apikey.Key, error) {
	g.n++
	plain := fmt.Sprintf("plain-%s-%d", env, g.n)
	return apikey.Key{Plain: plain, Prefix: plain[:8], Hash: "hash-" + plain, Env: env}, nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var admin = domain.Actor{Type: "platform_admin", ID: "test"}

func codeOf(err error) string {
	if e, ok := domain.AsError(err); ok {
		return e.Code
	}
	return ""
}
