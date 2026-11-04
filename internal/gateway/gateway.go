package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"math"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"apimarket/internal/domain"
	"apimarket/internal/domain/apikey"
	"apimarket/internal/domain/billing"
	"apimarket/internal/domain/catalog"
	"apimarket/internal/domain/limits"
	"apimarket/internal/domain/usage"
	"apimarket/internal/platform/breaker"
	mw "apimarket/internal/transport/http/middleware"
)

type Config struct {
	AttemptTimeout time.Duration // per upstream attempt
	MaxRetries     int           // extra attempts for retryable failures
	MaxBodyBytes   int64         // request bodies are buffered up to this size
	TrustProxy     bool          // take the client IP from X-Forwarded-For
	EventQueue     int           // buffered usage events awaiting publish
}

func DefaultConfig() Config {
	return Config{AttemptTimeout: 10 * time.Second, MaxRetries: 2, MaxBodyBytes: 10 << 20, EventQueue: 10_000}
}

// Deps are the gateway's collaborators, all behind interfaces.
type Deps struct {
	Catalog  CatalogView
	Limiter  Limiter
	Usage    UsagePublisher
	Breakers Breakers
	Log      *slog.Logger
	Client   *http.Client // optional; a tuned default is used if nil
}

type Gateway struct {
	cfg    Config
	d      Deps
	now    func() time.Time
	events chan usage.Event
}

func New(cfg Config, d Deps) *Gateway {
	if d.Client == nil {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.MaxIdleConnsPerHost = 256
		d.Client = &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Gateway{cfg: cfg, d: d, now: time.Now, events: make(chan usage.Event, cfg.EventQueue)}
}

// BreakersFrom adapts the platform breaker set to the Breakers port.
func BreakersFrom(s *breaker.Set) Breakers { return breakerSet{s} }

type breakerSet struct{ s *breaker.Set }

func (b breakerSet) For(apiID string) Breaker { return b.s.For(apiID) }

// RunPublisher drains usage events to the bus off the hot path.
func (g *Gateway) RunPublisher(ctx context.Context, workers int) {
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case e := <-g.events:
					g.publish(context.WithoutCancel(ctx), e)
				}
			}
		}()
	}
}

func (g *Gateway) publish(ctx context.Context, e usage.Event) {
	if err := g.d.Usage.Publish(ctx, e); err != nil {
		g.d.Log.Error("usage event lost", "err", err, "request_id", e.RequestID)
	}
}

// Router builds the Gin engine. Everything except /healthz is proxied:
// /{api-slug}/{path...}.
func (g *Gateway) Router() *gin.Engine {
	r := gin.New()
	r.Use(mw.Recovery(g.d.Log), mw.RequestID())
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.NoRoute(g.proxy)
	r.NoMethod(g.proxy)
	r.HandleMethodNotAllowed = false
	return r
}

func splitPath(p string) (slug, rest string) {
	p = strings.TrimPrefix(p, "/")
	slug, rest, _ = strings.Cut(p, "/")
	return slug, rest
}

func (g *Gateway) proxy(c *gin.Context) {
	start := g.now()
	reqID := mw.GetRequestID(c)
	snap := g.d.Catalog.Current()
	r := c.Request

	// 1. Authenticate.
	plain := apikey.FromRequestHeaders(r.Header.Get("X-API-Key"), r.Header.Get("Authorization"))
	if plain == "" {
		mw.AbortWithError(c, http.StatusUnauthorized, "MISSING_KEY", "provide your platform key in the X-API-Key header")
		return
	}
	key, ok := snap.KeysByHash[apikey.Hash(plain)]
	if !ok || !key.Usable(start) {
		mw.AbortWithError(c, http.StatusUnauthorized, "INVALID_KEY", "the key is invalid, revoked or expired")
		return
	}
	clientIP := g.clientIP(r)
	if !key.AllowsIP(clientIP) {
		mw.AbortWithError(c, http.StatusForbidden, "IP_NOT_ALLOWED", "this key is not allowed from your IP address")
		return
	}
	org, ok := snap.Orgs[key.OrgID]
	if !ok || !org.Active() {
		mw.AbortWithError(c, http.StatusForbidden, "ORG_INACTIVE", "the account is not active")
		return
	}

	// 2. Route and check the subscription.
	slug, rest := splitPath(r.URL.Path)
	api, ok := snap.APIsBySlug[slug]
	if !ok {
		mw.AbortWithError(c, http.StatusNotFound, "API_NOT_FOUND", "no published API with this name")
		return
	}
	sub, ok := snap.Subs[catalog.SubKey(org.ID, api.ID)]
	plan, planOK := snap.Plans[sub.PlanID]
	if !ok || !planOK {
		mw.AbortWithError(c, http.StatusForbidden, "NOT_SUBSCRIBED", "your account is not subscribed to this API")
		return
	}

	sandbox := key.Env == domain.EnvSandbox
	target, secret := api.BaseURL, api.Secret
	if sandbox {
		// Sandbox keys hit the mock server: free, no quota, still rate limited.
		target, secret = api.MockURL, ""
		if target == "" {
			mw.AbortWithError(c, http.StatusNotFound, "NO_MOCK", "this API has no mock server for sandbox keys")
			return
		}
	}

	// 3. Circuit breaker before reserving anything.
	cb := g.d.Breakers.For(api.ID)
	if !sandbox && !cb.Allow() {
		mw.AbortWithError(c, http.StatusServiceUnavailable, "PROVIDER_UNAVAILABLE", "the provider is currently unavailable; you are not charged")
		return
	}
	release := func() {
		if !sandbox {
			cb.Release()
		}
	}

	// 4. Limits, quota, balance — one atomic call.
	lr := limits.Request{
		KeyID: key.ID, APIID: api.ID, SubscriptionID: sub.ID, OrgID: org.ID,
		PeriodStart: sub.PeriodStart, PeriodEnd: sub.PeriodEnd, RateRPS: plan.RateLimitRPS,
	}
	if !sandbox {
		lr.QuotaLimit = plan.IncludedCalls
		lr.UpstreamRPS = api.UpstreamRPS
		lr.PriceMicro = int64(plan.PricePerCall)
		lr.Prepaid = org.BillingMode == domain.Prepaid
	}
	out, err := g.d.Limiter.Check(r.Context(), lr)
	if err != nil {
		g.d.Log.Error("limiter unavailable", "err", err)
		release()
		mw.AbortWithError(c, http.StatusServiceUnavailable, "LIMITER_UNAVAILABLE", "temporarily unable to check limits; retry shortly")
		return
	}
	if out.Result != limits.OK {
		release()
		g.reject(c, out)
		return
	}
	if out.QuotaRemaining >= 0 {
		c.Header("X-Quota-Remaining", strconv.FormatInt(out.QuotaRemaining, 10))
	}

	// 5. Forward.
	res := g.forward(r, target, rest, api, secret)
	if res.tooLarge {
		release()
		if !sandbox {
			g.refund(r.Context(), lr, reqID)
		}
		mw.AbortWithError(c, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "request body exceeds the gateway limit")
		return
	}
	if !sandbox {
		cb.Record(!res.failed && res.status < 500)
	}
	billable := !sandbox && billing.Billable(res.status, res.failed)
	if !sandbox && !billable {
		g.refund(r.Context(), lr, reqID)
	}

	// 6. Respond.
	var respBytes int64
	errCode := ""
	if res.failed {
		errCode = "UPSTREAM_UNREACHABLE"
		mw.AbortWithError(c, http.StatusBadGateway, errCode, "the provider did not respond; you are not charged")
	} else {
		respBytes = g.copyResponse(c.Writer, res.resp)
	}

	// 7. Meter.
	var cost, provCost int64
	if billable {
		cost, provCost = int64(plan.PricePerCall), int64(plan.ProviderCostPerCall)
	}
	e := usage.Event{
		TS: start.UTC(), RequestID: reqID, OrgID: org.ID, KeyID: key.ID, Env: string(key.Env),
		APIID: api.ID, SubscriptionID: sub.ID, Method: r.Method, Path: "/" + rest,
		Status: res.status, LatencyMS: g.now().Sub(start).Milliseconds(), UpstreamMS: res.upstream.Milliseconds(),
		ReqBytes: res.reqBytes, RespBytes: respBytes, Billable: billable, CostMicro: cost, ProviderMicro: provCost,
		ErrorCode: errCode, ClientIP: clientIP.String(), RetryCount: res.retries,
	}
	select {
	case g.events <- e:
	default: // queue full: publish inline rather than drop
		g.publish(context.WithoutCancel(r.Context()), e)
	}
}

func (g *Gateway) refund(ctx context.Context, lr limits.Request, reqID string) {
	if err := g.d.Limiter.Refund(context.WithoutCancel(ctx), lr); err != nil {
		g.d.Log.Error("refund failed; reconciliation will correct it", "err", err, "request_id", reqID)
	}
}

func (g *Gateway) reject(c *gin.Context, out limits.Outcome) {
	switch out.Result {
	case limits.RateLimited:
		c.Header("Retry-After", "1")
		mw.AbortWithError(c, http.StatusTooManyRequests, "RATE_LIMITED", "plan rate limit reached; slow down")
	case limits.QuotaExceeded:
		c.Header("X-Quota-Remaining", "0")
		mw.AbortWithError(c, http.StatusTooManyRequests, "QUOTA_EXCEEDED", "monthly quota used up; upgrade your plan or wait for the next period")
	case limits.UpstreamLimited:
		c.Header("Retry-After", "1")
		mw.AbortWithError(c, http.StatusTooManyRequests, "UPSTREAM_LIMIT", "the provider's capacity is temporarily exhausted")
	case limits.InsufficientBalance:
		mw.AbortWithError(c, http.StatusPaymentRequired, "INSUFFICIENT_BALANCE", "prepaid balance too low; top up to continue")
	default:
		mw.AbortWithError(c, http.StatusServiceUnavailable, "BALANCE_UNAVAILABLE", "temporarily unable to read your balance; retry shortly")
	}
}

type fwdResult struct {
	resp     *http.Response
	status   int
	failed   bool
	tooLarge bool
	retries  int
	upstream time.Duration
	reqBytes int64
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
	url := upstreamURL(base, rest, r.URL.RawQuery, api, secret)
	hasIdem := r.Header.Get("Idempotency-Key") != ""

	res := fwdResult{reqBytes: int64(len(body))}
	t0 := g.now()
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(r.Context(), g.cfg.AttemptTimeout)
		out, err := http.NewRequestWithContext(ctx, r.Method, url, bytes.NewReader(body))
		if err != nil {
			cancel()
			res.failed = true
			return res
		}
		for k, vs := range r.Header {
			if !hopByHop[k] && !stripInbound[k] {
				out.Header[k] = vs
			}
		}
		for _, h := range api.UpstreamHeaders {
			out.Header.Set(h.Name, h.Value)
		}
		setUpstreamAuth(out.Header, api, secret)
		resp, err := g.d.Client.Do(out)
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

func (g *Gateway) copyResponse(w gin.ResponseWriter, resp *http.Response) int64 {
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if !hopByHop[k] && k != "Set-Cookie" {
			w.Header()[k] = vs
		}
	}
	w.WriteHeader(resp.StatusCode)
	n, err := io.Copy(w, resp.Body)
	if err != nil && !errors.Is(err, context.Canceled) {
		g.d.Log.Warn("response copy interrupted", "err", err)
	}
	return n
}

func (g *Gateway) clientIP(r *http.Request) netip.Addr {
	if g.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if ip, err := netip.ParseAddr(strings.TrimSpace(strings.Split(xff, ",")[0])); err == nil {
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

// upstreamURL joins the provider base URL, the customer's path and query. For
// query auth the credential replaces any same-named parameter the customer
// sent, so a customer can never choose the value. The result may contain the
// secret: it must never be logged.
func upstreamURL(base, rest, rawQuery string, api catalog.API, secret string) string {
	u := strings.TrimRight(base, "/") + "/" + rest
	if api.AuthType == domain.AuthQuery && secret != "" && api.AuthHeader != "" {
		q, err := neturl.ParseQuery(rawQuery)
		if err != nil {
			q = neturl.Values{} // unparsable customer query: drop it rather than forward it unsafely
		}
		q.Set(api.AuthHeader, secret)
		return u + "?" + q.Encode()
	}
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return u
}

// setUpstreamAuth adds the provider credential for header-based auth types.
// Sandbox calls (empty secret) send none.
func setUpstreamAuth(h http.Header, api catalog.API, secret string) {
	if secret == "" {
		return
	}
	switch api.AuthType {
	case domain.AuthAPIKey:
		if api.AuthHeader != "" {
			h.Set(api.AuthHeader, secret)
		}
	case domain.AuthBearer:
		h.Set("Authorization", "Bearer "+secret)
	case domain.AuthBasic:
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(secret)))
	}
}
