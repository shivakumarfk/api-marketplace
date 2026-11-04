// Package service holds the control-plane use cases. Each service depends
// only on the small interfaces declared here (consumer-defined ports), so it
// can be unit-tested with in-memory fakes and the storage can change without
// touching business logic.
package service

import (
	"context"
	"time"

	"apimarket/internal/domain"
	"apimarket/internal/domain/apikey"
	"apimarket/internal/domain/money"
)

// TxManager runs fn in one database transaction. Repositories called with
// the ctx passed to fn take part in that transaction.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type OrgRepo interface {
	Create(ctx context.Context, o domain.Org) (domain.Org, error)
	Get(ctx context.Context, id string) (domain.Org, error) // domain.ErrNotFound if missing
}

type KeyRepo interface {
	Create(ctx context.Context, k domain.APIKey) (domain.APIKey, error)
	// Revoke marks an active key revoked and returns its org; domain.ErrNotFound if none.
	Revoke(ctx context.Context, keyID string, at time.Time) (orgID string, err error)
}

// CatalogEntry is one published API with its active plans.
type CatalogEntry struct {
	API   domain.API
	Plans []domain.Plan
}

type CatalogRepo interface {
	ListPublished(ctx context.Context) ([]CatalogEntry, error)
	// GetPublished returns one published API with its active plans; domain.ErrNotFound otherwise.
	GetPublished(ctx context.Context, slug string) (CatalogEntry, error)
	// FindPlan returns a published API and one of its active plans; domain.ErrNotFound otherwise.
	FindPlan(ctx context.Context, apiSlug, planID string) (domain.API, domain.Plan, error)
}

type SubscriptionRepo interface {
	// Upsert creates the org's active subscription to the API, or switches its plan.
	Upsert(ctx context.Context, s domain.Subscription) (domain.Subscription, error)
}

type LedgerRepo interface {
	// Post writes a balanced transaction and returns its id. Posting the same
	// RefType+RefID twice returns a domain Conflict error.
	Post(ctx context.Context, t domain.Txn) (string, error)
	// PrepaidWalletBalances returns the ledger balance of every prepaid org.
	PrepaidWalletBalances(ctx context.Context) (map[string]money.Micro, error)
}

type AuditRepo interface {
	Record(ctx context.Context, e domain.AuditEvent) error
}

// BalanceStore is the live prepaid balance the gateway enforces (Redis).
// It is a cache of the ledger, never the source of truth.
type BalanceStore interface {
	Add(ctx context.Context, orgID string, amount money.Micro) (money.Micro, error)
	Get(ctx context.Context, orgID string) (money.Micro, error) // domain.ErrNotFound if missing
	SetIfMissing(ctx context.Context, orgID string, amount money.Micro) (bool, error)
}

type Clock interface{ Now() time.Time }

// SystemClock is the production Clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// KeyGenerator creates platform API keys.
type KeyGenerator = apikey.Generator

// ---------- catalog administration ----------

// Sealer encrypts provider credentials before they are stored (local key or KMS).
type Sealer interface {
	Seal(plaintext string) (string, error)
}

type ProviderRepo interface {
	Create(ctx context.Context, p domain.Provider) (domain.Provider, error)
	Get(ctx context.Context, id string) (domain.Provider, error) // domain.ErrNotFound if missing
	List(ctx context.Context) ([]domain.Provider, error)
	// SignAgreement records the agreement time if not already set and returns the provider.
	SignAgreement(ctx context.Context, id string, at time.Time) (domain.Provider, error)
}

// APIRepo reads and writes APIs in any status (the public catalog uses CatalogRepo).
type APIRepo interface {
	Create(ctx context.Context, a domain.API) (domain.API, error) // Conflict if the slug is taken
	GetBySlug(ctx context.Context, slug string) (domain.API, error)
	GetByID(ctx context.Context, id string) (domain.API, error)
	List(ctx context.Context) ([]domain.API, error)
	Update(ctx context.Context, a domain.API) error // listing fields and credential, by ID
	SetStatus(ctx context.Context, id, status string) error
}

type PlanRepo interface {
	Create(ctx context.Context, p domain.Plan) (domain.Plan, error)
	Get(ctx context.Context, id string) (domain.Plan, error)
	ListByAPI(ctx context.Context, apiID string) ([]domain.Plan, error) // every status
	SetStatus(ctx context.Context, id, status string) error
}

type EndpointRepo interface {
	Create(ctx context.Context, e domain.Endpoint) (domain.Endpoint, error) // Conflict if method+path exists
	Get(ctx context.Context, id string) (domain.Endpoint, error)
	ListByAPI(ctx context.Context, apiID string) ([]domain.Endpoint, error)
	Update(ctx context.Context, e domain.Endpoint) error
	Delete(ctx context.Context, id string) error
}
