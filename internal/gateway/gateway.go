// Package gateway is the data-plane hot path: authenticate the platform key,
// route to the provider, enforce limits, inject the upstream credential,
// forward with retries and a circuit breaker, and emit a usage event.
package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"apimarket/internal/apikey"
	"apimarket/internal/billing"
	"apimarket/internal/breaker"
	"apimarket/internal/catalog"
	"apimarket/internal/events"
	"apimarket/internal/limiter"
)

type SnapshotSource interface{ Current() *catalog.Snapshot }

type Limiter interface {
	Check(ctx context.Context, r limiter.Request) (limiter.Outcome, error)
	Refund(ctx context.Context, r limiter.Request) error
}

type Config struct {
	AttemptTimeout time.Duration // per upstream attempt
	MaxRetries     int           // extra attempts for retryable failures
	MaxBodyBytes   int64         // request bodies are buffered up to this size
	TrustProxy     bool          // take the client IP from X-Forwarded-For
}

func DefaultConfig() Config {
	return Config{AttemptTimeout: 10 * time.Second, MaxRetries: 2, MaxBodyBytes: 10 << 20}
}

type Gateway struct {
	cfg      Config
	snap     SnapshotSource
	lim      Limiter
	pub      events.Publisher
	breakers *breaker.Set
	client   *http.Client
	log      *slog.Logger
	now      func() time.Time
	eventsCh chan events.Usage
}

func New(cfg Config, snap SnapshotSource, lim Limiter, pub events.Publisher, breakers *breaker.Set, log *slog.Logger) *Gateway {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 256
	g := &Gateway{
		cfg: cfg, snap: snap, lim: lim, pub: pub, breakers: breakers, log: log, now: time.Now,
		client:   &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		eventsCh: make(chan events.Usage, 10_000),
	}
	return g
}

// RunPublisher drains usage events to the bus off the hot path.
func (g *Gateway) RunPublisher(ctx context.Context, workers int) {
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case u := <-g.eventsCh:
					if err := g.pub.Publish(context.WithoutCancel(ctx), u); err != nil {
						g.log.Error("usage event lost", "err", err, "request_id", u.RequestID)
					}
				}
			}
		}()
	}
}

// Handler returns the gateway HTTP handler. Routes: /{api-slug}/{path...}.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/{slug}/{rest...}", g.serve)
	return mux
}

// apiError is the stable error shape returned by the gateway.
func writeError(w http.ResponseWriter, status int, code, msg, reqID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg, "request_id": reqID}})
}

func newRequestID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	start := g.now()
	reqID := newRequestID()
	w.Header().Set("X-Request-Id", reqID)
	snap := g.snap.Current()

	// 1. Authenticate.
	plain := apikey.FromRequestHeaders(r.Header.Get("X-API-Key"), r.Header.Get("Authorization"))
	if plain == "" {
		writeError(w, http.StatusUnauthorized, "MISSING_KEY", "provide your platform key in the X-API-Key header", reqID)
		return
	}
	key, ok := snap.KeysByHash[apikey.Hash(plain)]
	if !ok || !key.Usable(start) {
		writeError(w, http.StatusUnauthorized, "INVALID_KEY", "the key is invalid, revoked or expired", reqID)
		return
	}
	clientIP := g.clientIP(r)
	if !key.AllowsIP(clientIP) {
		writeError(w, http.StatusForbidden, "IP_NOT_ALLOWED", "this key is not allowed from your IP address", reqID)
		return
	}
	org, ok := snap.Orgs[key.OrgID]
	if !ok || org.Status != "active" {
		writeError(w, http.StatusForbidden, "ORG_INACTIVE", "the account is not active", reqID)
		return
	}

	// 2. Route and check the subscription.
	api, ok := snap.APIsBySlug[r.PathValue("slug")]
	if !ok {
		writeError(w, http.StatusNotFound, "API_NOT_FOUND", "no published API with this name", reqID)
		return
	}
	sub, ok := snap.Subs[catalog.SubKey(org.ID, api.ID)]
	if !ok {
		writeError(w, http.StatusForbidden, "NOT_SUBSCRIBED", "your account is not subscribed to this API", reqID)
		return
	}
	plan, ok := snap.Plans[sub.PlanID]
	if !ok {
		writeError(w, http.StatusForbidden, "NOT_SUBSCRIBED", "the subscription's plan is not available", reqID)
		return
	}

	sandbox := key.Env == string(apikey.EnvSandbox)
	target, secret := api.BaseURL, api.Secret
	if sandbox {
		// Sandbox keys hit the mock server: free, no quota, still rate limited.
		target, secret = api.MockURL, ""
		if target == "" {
			writeError(w, http.StatusNotFound, "NO_MOCK", "this API has no mock server for sandbox keys", reqID)
			return
		}
	}

	// 3. Circuit breaker before reserving anything.
	cb := g.breakers.For(api.ID)
	if !sandbox && !cb.Allow() {
		writeError(w, http.StatusServiceUnavailable, "PROVIDER_UNAVAILABLE", "the provider is currently unavailable; you are not charged", reqID)
		return
	}

	// 4. Limits, quota, balance — one atomic Redis call.
	lr := limiter.Request{
		KeyID: key.ID, APIID: api.ID, SubscriptionID: sub.ID, OrgID: org.ID,
		PeriodStart: sub.PeriodStart, PeriodEnd: sub.PeriodEnd, RateRPS: plan.RateLimitRPS,
	}
	if !sandbox {
		lr.QuotaLimit = plan.IncludedCalls
		lr.UpstreamRPS = api.UpstreamRPS
		lr.PriceMicro = plan.PricePerCallMicro
		lr.Prepaid = org.BillingMode == "prepaid"
	}
	out, err := g.lim.Check(r.Context(), lr)
	if err != nil {
		g.log.Error("limiter unavailable", "err", err)
		if !sandbox {
			cb.Release() // not the provider's fault
		}
		writeError(w, http.StatusServiceUnavailable, "LIMITER_UNAVAILABLE", "temporarily unable to check limits; retry shortly", reqID)
		return
	}
	if out.Result != limiter.OK {
		if !sandbox {
			cb.Release()
		}
		g.reject(w, out, reqID)
		return
	}
	if out.QuotaRemaining >= 0 {
		w.Header().Set("X-Quota-Remaining", strconv.FormatInt(out.QuotaRemaining, 10))
	}

	// 5. Forward.
	res := g.forward(r, target, r.PathValue("rest"), api, secret)
	if res.tooLarge {
		if !sandbox {
			cb.Release()
			g.lim.Refund(context.WithoutCancel(r.Context()), lr)
		}
		writeError(w, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds the gateway limit", reqID)
		return
	}
	if !sandbox {
		cb.Record(!res.failed && res.status < 500)
	}
	billable := !sandbox && billing.Billable(res.status, res.failed)
	if !sandbox && !billable {
		if err := g.lim.Refund(context.WithoutCancel(r.Context()), lr); err != nil {
			g.log.Error("refund failed; reconciliation will correct it", "err", err, "request_id", reqID)
		}
	}

	var respBytes int64
	errCode := ""
	if res.failed {
		errCode = "UPSTREAM_UNREACHABLE"
		writeError(w, http.StatusBadGateway, errCode, "the provider did not respond; you are not charged", reqID)
	} else {
		respBytes = g.copyResponse(w, res.resp)
	}

	cost, provCost := int64(0), int64(0)
	if billable {
		cost, provCost = plan.PricePerCallMicro, plan.ProviderCostMicro
	}
	u := events.Usage{
		TS: start.UTC(), RequestID: reqID, OrgID: org.ID, KeyID: key.ID, Env: key.Env,
		APIID: api.ID, SubscriptionID: sub.ID, Method: r.Method, Path: "/" + r.PathValue("rest"),
		Status: res.status, LatencyMS: g.now().Sub(start).Milliseconds(), UpstreamMS: res.upstream.Milliseconds(),
		ReqBytes: res.reqBytes, RespBytes: respBytes, Billable: billable, CostMicro: cost, ProviderMicro: provCost,
		ErrorCode: errCode, ClientIP: clientIP.String(), RetryCount: res.retries,
	}
	select {
	case g.eventsCh <- u:
	default:
		// Queue full: publish inline rather than drop (the Spool handles bus outages).
		if err := g.pub.Publish(context.WithoutCancel(r.Context()), u); err != nil {
			g.log.Error("usage event lost", "err", err, "request_id", reqID)
		}
	}
}

func (g *Gateway) reject(w http.ResponseWriter, out limiter.Outcome, reqID string) {
	switch out.Result {
	case limiter.RateLimited:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "plan rate limit reached; slow down", reqID)
	case limiter.QuotaExceeded:
		w.Header().Set("X-Quota-Remaining", "0")
		writeError(w, http.StatusTooManyRequests, "QUOTA_EXCEEDED", "monthly quota used up; upgrade your plan or wait for the next period", reqID)
	case limiter.UpstreamLimited:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "UPSTREAM_LIMIT", "the provider's capacity is temporarily exhausted", reqID)
	case limiter.InsufficientBalance:
		writeError(w, http.StatusPaymentRequired, "INSUFFICIENT_BALANCE", "prepaid balance too low; top up to continue", reqID)
	default:
		writeError(w, http.StatusServiceUnavailable, "BALANCE_UNAVAILABLE", "temporarily unable to read your balance; retry shortly", reqID)
	}
}

type fwdResult struct {
	resp     *http.Response
	status   int
	failed   bool
	retries  int
	upstream time.Duration
	reqBytes int64
	tooLarge bool
}

var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true,
	"Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
}

// Headers the customer sends that must never reach the provider.
var stripInbound = map[string]bool{"X-Api-Key": true, "Authorization": true, "Cookie": true, "X-Forwarded-For": true}

func (g *Gateway) forward(r *http.Request, base, rest string, api catalog.API, secret string) fwdResult {
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(io.LimitReader(r.Body, g.cfg.MaxBodyBytes+1))
		if err != nil || int64(len(b)) > g.cfg.MaxBodyBytes {
			return fwdResult{tooLarge: true}
		}
		body = b
	}
	url := strings.TrimRight(base, "/") + "/" + rest
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	hasIdem := r.Header.Get("Idempotency-Key") != ""

	var res fwdResult
	res.reqBytes = int64(len(body))
	t0 := g.now()
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(r.Context(), g.cfg.AttemptTimeout)
		out, err := http.NewRequestWithContext(ctx, r.Method, url, bytes.NewReader(body))
		if err != nil {
			cancel()
			return fwdResult{failed: true}
		}
		for k, vs := range r.Header {
			if hopByHop[k] || stripInbound[k] {
				continue
			}
			out.Header[k] = vs
		}
		if secret != "" && api.AuthHeader != "" {
			out.Header.Set(api.AuthHeader, secret)
		}
		resp, err := g.client.Do(out)
		failed := err != nil
		status := 0
		if !failed {
			status = resp.StatusCode
		}
		if attempt < g.cfg.MaxRetries && billing.Retryable(r.Method, hasIdem, status, failed) && r.Context().Err() == nil {
			if resp != nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			cancel()
			backoff := time.Duration(50*math.Pow(2, float64(attempt))) * time.Millisecond
			time.Sleep(backoff/2 + time.Duration(mrand.Int64N(int64(backoff/2)+1)))
			res.retries++
			continue
		}
		res.upstream = g.now().Sub(t0)
		if failed {
			cancel()
			res.failed = true
			return res
		}
		resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		res.resp, res.status = resp, status
		return res
	}
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error { err := c.ReadCloser.Close(); c.cancel(); return err }

func (g *Gateway) copyResponse(w http.ResponseWriter, resp *http.Response) int64 {
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if hopByHop[k] || k == "Set-Cookie" {
			continue
		}
		w.Header()[k] = vs
	}
	w.WriteHeader(resp.StatusCode)
	n, err := io.Copy(w, resp.Body)
	if err != nil && !errors.Is(err, context.Canceled) {
		g.log.Warn("response copy interrupted", "err", err)
	}
	return n
}

func (g *Gateway) clientIP(r *http.Request) netip.Addr {
	if g.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if ip, err := netip.ParseAddr(first); err == nil {
				return ip.Unmap()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, _ := netip.ParseAddr(host)
	return ip.Unmap()
}
