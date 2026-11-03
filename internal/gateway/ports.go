// Package gateway is the data-plane hot path, served with Gin. It depends
// only on the interfaces below; Redis, PostgreSQL and the event bus are
// plugged in by cmd/gateway.
package gateway

import (
	"context"

	"apimarket/internal/domain/catalog"
	"apimarket/internal/domain/limits"
	"apimarket/internal/domain/usage"
)

// CatalogView returns the latest in-memory catalog snapshot (never nil).
type CatalogView interface {
	Current() *catalog.Snapshot
}

// CatalogSource loads a full snapshot (PostgreSQL in production).
type CatalogSource interface {
	Load(ctx context.Context) (*catalog.Snapshot, error)
}

// Limiter checks and consumes limits atomically, and refunds non-billable calls.
type Limiter interface {
	Check(ctx context.Context, r limits.Request) (limits.Outcome, error)
	Refund(ctx context.Context, r limits.Request) error
}

// UsagePublisher sends metered-call events to the bus.
type UsagePublisher interface {
	Publish(ctx context.Context, e usage.Event) error
}

// Breaker is a per-provider circuit breaker.
type Breaker interface {
	Allow() bool
	Record(success bool)
	Release()
}

// Breakers hands out one Breaker per provider API.
type Breakers interface {
	For(apiID string) Breaker
}
