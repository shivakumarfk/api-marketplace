// Package limiter enforces, in one atomic Redis round-trip per call:
// the plan rate limit, the period quota, the provider's upstream limit,
// and (for prepaid orgs) reserving the call's price from the balance.
//
// Redis is never the source of truth for money: balances are rebuilt from
// the PostgreSQL ledger when missing. If a prepaid balance is unknown the
// call is rejected (fail closed) rather than allowed for free.
package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed check.lua
var checkSrc string

//go:embed refund.lua
var refundSrc string

// Result codes returned by the check script.
type Result int

const (
	OK Result = iota
	RateLimited
	QuotaExceeded
	UpstreamLimited
	InsufficientBalance
	BalanceUnknown
)

func (r Result) String() string {
	return [...]string{"OK", "RATE_LIMITED", "QUOTA_EXCEEDED", "UPSTREAM_LIMIT", "INSUFFICIENT_BALANCE", "BALANCE_UNAVAILABLE"}[r]
}

// Request describes one call to check.
type Request struct {
	KeyID          string
	APIID          string
	SubscriptionID string
	OrgID          string
	PeriodEnd      time.Time // quota counter expires at the end of the billing period
	PeriodStart    time.Time

	RateRPS     float64 // plan rate limit per key per API; 0 = unlimited
	QuotaLimit  int64   // included calls per period; 0 = no quota
	UpstreamRPS float64 // provider limit across all customers; 0 = unlimited
	PriceMicro  int64   // reserved from the balance if Prepaid
	Prepaid     bool
}

// Outcome is the result of a check.
type Outcome struct {
	Result         Result
	QuotaRemaining int64 // -1 when no quota applies
	BalanceMicro   int64 // -1 when not prepaid
}

type Limiter struct {
	rdb    redis.Scripter
	check  *redis.Script
	refund *redis.Script
	now    func() time.Time
}

func New(rdb redis.Scripter) *Limiter {
	return &Limiter{rdb: rdb, check: redis.NewScript(checkSrc), refund: redis.NewScript(refundSrc), now: time.Now}
}

func rlKey(r Request) string { return "rl:" + r.KeyID + ":" + r.APIID }
func quotaKey(r Request) string {
	return fmt.Sprintf("q:%s:%d", r.SubscriptionID, r.PeriodStart.Unix())
}
func upKey(r Request) string { return "up:" + r.APIID }

// BalanceKey is the Redis key holding an org's prepaid balance in micro-rupees.
func BalanceKey(orgID string) string { return "bal:" + orgID }

// Check runs all limits atomically and, if all pass, consumes them.
func (l *Limiter) Check(ctx context.Context, r Request) (Outcome, error) {
	now := l.now()
	ttl := int64(r.PeriodEnd.Sub(now).Seconds()) + 3600
	if ttl < 60 {
		ttl = 60
	}
	prepaid := 0
	if r.Prepaid {
		prepaid = 1
	}
	res, err := l.check.Run(ctx, l.rdb,
		[]string{rlKey(r), quotaKey(r), upKey(r), BalanceKey(r.OrgID)},
		now.UnixMilli(), r.RateRPS, r.QuotaLimit, ttl, r.UpstreamRPS, r.PriceMicro, prepaid,
	).Int64Slice()
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Result: Result(res[0]), QuotaRemaining: res[1], BalanceMicro: res[2]}, nil
}

// Refund returns the quota unit and reserved price for a call that turned out
// not to be billable (provider 5xx, timeout). Rate-limit tokens are not refunded.
func (l *Limiter) Refund(ctx context.Context, r Request) error {
	prepaid := 0
	if r.Prepaid {
		prepaid = 1
	}
	return l.refund.Run(ctx, l.rdb, []string{quotaKey(r), BalanceKey(r.OrgID)}, r.QuotaLimit, r.PriceMicro, prepaid).Err()
}
