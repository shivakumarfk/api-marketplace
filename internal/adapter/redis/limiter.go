// Package redisx holds the Redis adapters: the atomic limiter, the prepaid
// balance store, and the usage event stream.
package redisx

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"apimarket/internal/domain/limits"
)

//go:embed check.lua
var checkSrc string

//go:embed refund.lua
var refundSrc string

// Limiter enforces, in one atomic Redis round-trip per call: the plan rate
// limit, the period quota, the provider's upstream limit and (for prepaid
// orgs) reserving the call's price. An unknown balance fails closed.
type Limiter struct {
	rdb    redis.Scripter
	check  *redis.Script
	refund *redis.Script
	now    func() time.Time
}

func NewLimiter(rdb redis.Scripter) *Limiter {
	return &Limiter{rdb: rdb, check: redis.NewScript(checkSrc), refund: redis.NewScript(refundSrc), now: time.Now}
}

func rlKey(r limits.Request) string { return "rl:" + r.KeyID + ":" + r.APIID }
func quotaKey(r limits.Request) string {
	return fmt.Sprintf("q:%s:%d", r.SubscriptionID, r.PeriodStart.Unix())
}
func upKey(r limits.Request) string { return "up:" + r.APIID }

func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Check runs all limits atomically and, if all pass, consumes them.
func (l *Limiter) Check(ctx context.Context, r limits.Request) (limits.Outcome, error) {
	now := l.now()
	ttl := int64(r.PeriodEnd.Sub(now).Seconds()) + 3600
	if ttl < 60 {
		ttl = 60
	}
	res, err := l.check.Run(ctx, l.rdb,
		[]string{rlKey(r), quotaKey(r), upKey(r), BalanceKey(r.OrgID)},
		now.UnixMilli(), r.RateRPS, r.QuotaLimit, ttl, r.UpstreamRPS, r.PriceMicro, flag(r.Prepaid),
	).Int64Slice()
	if err != nil {
		return limits.Outcome{}, err
	}
	return limits.Outcome{Result: limits.Result(res[0]), QuotaRemaining: res[1], BalanceMicro: res[2]}, nil
}

// Refund returns the quota unit and reserved price for a call that turned out
// not to be billable. Rate-limit tokens are not refunded.
func (l *Limiter) Refund(ctx context.Context, r limits.Request) error {
	return l.refund.Run(ctx, l.rdb, []string{quotaKey(r), BalanceKey(r.OrgID)}, r.QuotaLimit, r.PriceMicro, flag(r.Prepaid)).Err()
}
