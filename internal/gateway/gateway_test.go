package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"apimarket/internal/apikey"
	"apimarket/internal/breaker"
	"apimarket/internal/catalog"
	"apimarket/internal/events"
	"apimarket/internal/limiter"
)

const (
	liveKey = "mk_live_" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testKey = "mk_test_" + "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
)

type staticSnap struct{ s *catalog.Snapshot }

func (s staticSnap) Current() *catalog.Snapshot { return s.s }

type fakeLimiter struct {
	mu      sync.Mutex
	result  limiter.Result
	checks  int
	refunds int
}

func (f *fakeLimiter) Check(context.Context, limiter.Request) (limiter.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	return limiter.Outcome{Result: f.result, QuotaRemaining: -1, BalanceMicro: -1}, nil
}

func (f *fakeLimiter) Refund(context.Context, limiter.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunds++
	return nil
}

type capturePub struct {
	mu  sync.Mutex
	got []events.Usage
}

func (c *capturePub) Publish(_ context.Context, u events.Usage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, u)
	return nil
}

func (c *capturePub) last(t *testing.T) events.Usage {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.got) == 0 {
		t.Fatal("no usage event published")
	}
	return c.got[len(c.got)-1]
}

type env struct {
	gw       *Gateway
	srv      *httptest.Server
	lim      *fakeLimiter
	pub      *capturePub
	provider *httptest.Server
	hits     *int64
}

func setup(t *testing.T, provider http.HandlerFunc, mutate func(*catalog.Snapshot)) *env {
	t.Helper()
	var hits int64
	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		provider(w, r)
	}))
	t.Cleanup(prov.Close)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"mock":true}`))
	}))
	t.Cleanup(mock.Close)

	now := time.Now()
	s := catalog.Empty()
	s.KeysByHash[apikey.Hash(liveKey)] = catalog.Key{ID: "key1", OrgID: "org1", Env: "production", Status: "active"}
	s.KeysByHash[apikey.Hash(testKey)] = catalog.Key{ID: "key2", OrgID: "org1", Env: "sandbox", Status: "active"}
	s.Orgs["org1"] = catalog.Org{ID: "org1", BillingMode: "prepaid", Status: "active"}
	s.APIsBySlug["weather"] = catalog.API{ID: "api1", Slug: "weather", BaseURL: prov.URL, MockURL: mock.URL,
		AuthType: "api_key", AuthHeader: "X-Provider-Key", Secret: "upstream-secret", Status: "published"}
	s.Plans["plan1"] = catalog.Plan{ID: "plan1", Kind: "payg", PricePerCallMicro: 50_000, ProviderCostMicro: 30_000}
	s.Subs[catalog.SubKey("org1", "api1")] = catalog.Subscription{ID: "sub1", OrgID: "org1", APIID: "api1", PlanID: "plan1",
		Status: "active", PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour)}
	if mutate != nil {
		mutate(s)
	}

	lim := &fakeLimiter{}
	pub := &capturePub{}
	cfg := DefaultConfig()
	cfg.AttemptTimeout = 2 * time.Second
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := New(cfg, staticSnap{s}, lim, pub, breaker.NewSet(3, time.Minute, nil), log)
	gw.eventsCh = make(chan events.Usage) // unbuffered + no workers → publishes inline, deterministic
	srv := httptest.NewServer(gw.Handler())
	t.Cleanup(srv.Close)
	return &env{gw: gw, srv: srv, lim: lim, pub: pub, provider: prov, hits: &hits}
}

func do(t *testing.T, e *env, method, path, key string, body io.Reader) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp, m
}

func errCode(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func TestAuthAndRouting(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }, func(s *catalog.Snapshot) {
		s.APIsBySlug["maps"] = catalog.API{ID: "api2", Slug: "maps", BaseURL: "http://unused", Status: "published"}
	})
	cases := []struct {
		path, key string
		status    int
		code      string
	}{
		{"/weather/today", "", 401, "MISSING_KEY"},
		{"/weather/today", "mk_live_nope", 401, "INVALID_KEY"},
		{"/nope/today", liveKey, 404, "API_NOT_FOUND"},
		{"/maps/today", liveKey, 403, "NOT_SUBSCRIBED"},
	}
	for _, c := range cases {
		resp, m := do(t, e, "GET", c.path, c.key, nil)
		if resp.StatusCode != c.status || errCode(m) != c.code {
			t.Errorf("%s key=%q: got %d %q, want %d %q", c.path, c.key, resp.StatusCode, errCode(m), c.status, c.code)
		}
		if resp.Header.Get("X-Request-Id") == "" {
			t.Error("missing X-Request-Id")
		}
	}
	if *e.hits != 0 || e.lim.checks != 0 {
		t.Fatal("rejected requests must not reach the limiter or provider")
	}
}

func TestExpiredRevokedAndIPAllowlist(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {}, func(s *catalog.Snapshot) {
		k := s.KeysByHash[apikey.Hash(liveKey)]
		k.IPAllow = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
		s.KeysByHash[apikey.Hash(liveKey)] = k
		s.KeysByHash[apikey.Hash(testKey)] = catalog.Key{ID: "k", OrgID: "org1", Env: "sandbox", Status: "active", ExpiresAt: &past}
	})
	if resp, m := do(t, e, "GET", "/weather/x", liveKey, nil); resp.StatusCode != 403 || errCode(m) != "IP_NOT_ALLOWED" {
		t.Errorf("allowlist: got %d %s", resp.StatusCode, errCode(m))
	}
	if resp, m := do(t, e, "GET", "/weather/x", testKey, nil); resp.StatusCode != 401 || errCode(m) != "INVALID_KEY" {
		t.Errorf("expired: got %d %s", resp.StatusCode, errCode(m))
	}
}

func TestCredentialInjectionAndBilling(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Provider-Key") != "upstream-secret" {
			t.Error("upstream credential not injected")
		}
		if r.Header.Get("X-API-Key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("customer's platform key leaked to the provider")
		}
		if r.URL.Path != "/today" || r.URL.RawQuery != "city=Bengaluru" {
			t.Errorf("bad upstream URL %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Write([]byte(`{"temp":27}`))
	}, nil)
	resp, m := do(t, e, "GET", "/weather/today?city=Bengaluru", liveKey, nil)
	if resp.StatusCode != 200 || m["temp"] != float64(27) {
		t.Fatalf("got %d %v", resp.StatusCode, m)
	}
	u := e.pub.last(t)
	if !u.Billable || u.CostMicro != 50_000 || u.ProviderMicro != 30_000 || u.Status != 200 {
		t.Fatalf("bad usage event: %+v", u)
	}
	if e.lim.refunds != 0 {
		t.Fatal("billable call must not be refunded")
	}
}

func TestProvider5xxIsNotBilledAndRetriedForGET(t *testing.T) {
	var n int64
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt64(&n, 1) < 3 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}, nil)
	resp, _ := do(t, e, "GET", "/weather/x", liveKey, nil)
	if resp.StatusCode != 200 || *e.hits != 3 {
		t.Fatalf("GET should be retried twice: status %d hits %d", resp.StatusCode, *e.hits)
	}
	if u := e.pub.last(t); u.RetryCount != 2 || !u.Billable {
		t.Fatalf("event: %+v", u)
	}
}

func TestPOSTIsNeverRetriedAndFailureRefunded(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }, nil)
	resp, _ := do(t, e, "POST", "/weather/x", liveKey, strings.NewReader(`{"a":1}`))
	if resp.StatusCode != 503 || *e.hits != 1 {
		t.Fatalf("POST must not be retried: status %d hits %d", resp.StatusCode, *e.hits)
	}
	if e.lim.refunds != 1 {
		t.Fatal("non-billable call must be refunded")
	}
	if u := e.pub.last(t); u.Billable || u.CostMicro != 0 {
		t.Fatalf("5xx must not be billed: %+v", u)
	}
}

func TestLimiterRejections(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
	for res, want := range map[limiter.Result]struct {
		status int
		code   string
	}{
		limiter.RateLimited:         {429, "RATE_LIMITED"},
		limiter.QuotaExceeded:       {429, "QUOTA_EXCEEDED"},
		limiter.UpstreamLimited:     {429, "UPSTREAM_LIMIT"},
		limiter.InsufficientBalance: {402, "INSUFFICIENT_BALANCE"},
	} {
		e.lim.result = res
		resp, m := do(t, e, "GET", "/weather/x", liveKey, nil)
		if resp.StatusCode != want.status || errCode(m) != want.code {
			t.Errorf("%v: got %d %s", res, resp.StatusCode, errCode(m))
		}
	}
	if *e.hits != 0 {
		t.Fatal("rejected calls must not reach the provider")
	}
}

func TestSandboxGoesToMockAndIsFree(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("sandbox must not hit the real provider") }, nil)
	resp, m := do(t, e, "GET", "/weather/x", testKey, nil)
	if resp.StatusCode != 200 || m["mock"] != true {
		t.Fatalf("got %d %v", resp.StatusCode, m)
	}
	if u := e.pub.last(t); u.Billable || u.CostMicro != 0 || u.Env != "sandbox" {
		t.Fatalf("sandbox event: %+v", u)
	}
}

func TestCircuitBreakerOpens(t *testing.T) {
	e := setup(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, nil)
	for i := 0; i < 3; i++ {
		do(t, e, "POST", "/weather/x", liveKey, nil)
	}
	resp, m := do(t, e, "POST", "/weather/x", liveKey, nil)
	if resp.StatusCode != 503 || errCode(m) != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("got %d %s", resp.StatusCode, errCode(m))
	}
	if *e.hits != 3 {
		t.Fatalf("open circuit must not call the provider: hits %d", *e.hits)
	}
}
