// Package billing holds the rules for what is charged.
package billing

// Billable reports whether a proxied call is charged to the customer.
//
// Rule (architecture §3): 2xx, 3xx and 4xx responses from the provider are
// billable; 5xx responses, timeouts and connection failures are not. Requests
// the platform itself rejects (bad key, quota, balance, rate limit) never reach
// the provider and are never billed.
func Billable(upstreamStatus int, upstreamFailed bool) bool {
	if upstreamFailed {
		return false
	}
	return upstreamStatus >= 200 && upstreamStatus < 500
}

// Retryable reports whether a failed attempt may be retried automatically.
// Only idempotent requests are retried, and only on connection errors,
// timeouts, 502, 503 or 504.
func Retryable(method string, hasIdempotencyKey bool, upstreamStatus int, upstreamFailed bool) bool {
	if !Idempotent(method) && !hasIdempotencyKey {
		return false
	}
	if upstreamFailed {
		return true
	}
	switch upstreamStatus {
	case 502, 503, 504:
		return true
	}
	return false
}

// Idempotent reports whether an HTTP method is safe to repeat.
func Idempotent(method string) bool {
	switch method {
	case "GET", "HEAD", "PUT", "DELETE", "OPTIONS":
		return true
	}
	return false
}
