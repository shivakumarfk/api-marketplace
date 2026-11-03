package limiter

// These tests run against a real Redis (never a mock), per the testing
// expectations. Set REDIS_ADDR (default localhost:6379); they are skipped
// if Redis is not reachable.

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rdb.FlushDB(ctx); rdb.Close() })
	return rdb
}

func baseReq() Request {
	now := time.Now()
	return Request{
		KeyID: "k1", APIID: "a1", SubscriptionID: "s1", OrgID: "o1",
		PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(30 * 24 * time.Hour),
	}
}

func TestQuotaExactLimit(t *testing.T) {
	ctx := context.Background()
	l := New(testRedis(t))
	r := baseReq()
	r.QuotaLimit = 3
	for i := 1; i <= 3; i++ {
		out, err := l.Check(ctx, r)
		if err != nil || out.Result != OK {
			t.Fatalf("call %d: %v %v", i, out, err)
		}
		if out.QuotaRemaining != int64(3-i) {
			t.Fatalf("call %d: remaining %d", i, out.QuotaRemaining)
		}
	}
	out, _ := l.Check(ctx, r)
	if out.Result != QuotaExceeded {
		t.Fatalf("4th call: got %v, want QuotaExceeded", out.Result)
	}
	// Refund of a non-billable call frees one unit.
	if err := l.Refund(ctx, r); err != nil {
		t.Fatal(err)
	}
	if out, _ := l.Check(ctx, r); out.Result != OK {
		t.Fatalf("after refund: got %v", out.Result)
	}
}

func TestRateLimit(t *testing.T) {
	ctx := context.Background()
	l := New(testRedis(t))
	fixed := time.Now()
	l.now = func() time.Time { return fixed }
	r := baseReq()
	r.RateRPS = 5
	for i := 0; i < 5; i++ {
		if out, _ := l.Check(ctx, r); out.Result != OK {
			t.Fatalf("call %d rejected: %v", i, out.Result)
		}
	}
	if out, _ := l.Check(ctx, r); out.Result != RateLimited {
		t.Fatalf("6th call in same instant: got %v", out.Result)
	}
	fixed = fixed.Add(200 * time.Millisecond) // refills 1 token at 5 rps
	if out, _ := l.Check(ctx, r); out.Result != OK {
		t.Fatalf("after refill: got %v", out.Result)
	}
}

func TestUpstreamLimitSharedAcrossKeys(t *testing.T) {
	ctx := context.Background()
	l := New(testRedis(t))
	fixed := time.Now()
	l.now = func() time.Time { return fixed }
	a, b := baseReq(), baseReq()
	b.KeyID, b.SubscriptionID, b.OrgID = "k2", "s2", "o2"
	a.UpstreamRPS, b.UpstreamRPS = 2, 2
	if out, _ := l.Check(ctx, a); out.Result != OK {
		t.Fatal(out.Result)
	}
	if out, _ := l.Check(ctx, b); out.Result != OK {
		t.Fatal(out.Result)
	}
	if out, _ := l.Check(ctx, a); out.Result != UpstreamLimited {
		t.Fatalf("got %v, want UpstreamLimited", out.Result)
	}
}

func TestPrepaidBalanceFailsClosedWhenUnknown(t *testing.T) {
	ctx := context.Background()
	l := New(testRedis(t))
	r := baseReq()
	r.Prepaid, r.PriceMicro = true, 50_000
	if out, _ := l.Check(ctx, r); out.Result != BalanceUnknown {
		t.Fatalf("got %v, want BalanceUnknown", out.Result)
	}
}

func TestRejectedCallConsumesNothing(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	l := New(rdb)
	r := baseReq()
	r.Prepaid, r.PriceMicro, r.QuotaLimit = true, 100, 10
	rdb.Set(ctx, BalanceKey(r.OrgID), 50, 0) // less than the price
	if out, _ := l.Check(ctx, r); out.Result != InsufficientBalance {
		t.Fatalf("got %v", out.Result)
	}
	if v, _ := rdb.Get(ctx, quotaKey(r)).Int64(); v != 0 {
		t.Fatalf("quota consumed by a rejected call: %d", v)
	}
	if v, _ := rdb.Get(ctx, BalanceKey(r.OrgID)).Int64(); v != 50 {
		t.Fatalf("balance changed by a rejected call: %d", v)
	}
}

// Many concurrent calls against a nearly empty balance: exactly
// floor(balance/price) succeed and the balance never goes negative.
func TestConcurrentCallsNeverOverspend(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	l := New(rdb)
	r := baseReq()
	r.Prepaid, r.PriceMicro = true, 7
	const start = 100 // allows 14 calls (98), leaves 2
	rdb.Set(ctx, BalanceKey(r.OrgID), start, 0)

	var ok int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := l.Check(ctx, r)
			if err != nil {
				t.Error(err)
				return
			}
			if out.Result == OK {
				atomic.AddInt64(&ok, 1)
			}
		}()
	}
	wg.Wait()
	bal, _ := rdb.Get(ctx, BalanceKey(r.OrgID)).Int64()
	if ok != start/7 || bal != start%7 {
		t.Fatalf("succeeded=%d balance=%d; want %d and %d", ok, bal, start/7, start%7)
	}
}
