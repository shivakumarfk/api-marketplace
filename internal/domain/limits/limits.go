// Package limits defines what the gateway asks the limiter for, independent
// of how it is implemented (Redis today).
package limits

import "time"

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
	PeriodStart    time.Time
	PeriodEnd      time.Time // quota counter expires after the billing period

	RateRPS     float64 // plan rate limit per key per API; 0 = unlimited
	QuotaLimit  int64   // included calls per period; 0 = no quota
	UpstreamRPS float64 // provider limit across all customers; 0 = unlimited
	PriceMicro  int64   // reserved from the balance if Prepaid
	Prepaid     bool
}

type Outcome struct {
	Result         Result
	QuotaRemaining int64 // -1 when no quota applies
	BalanceMicro   int64 // -1 when not prepaid
}
