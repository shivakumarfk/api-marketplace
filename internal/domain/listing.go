package domain

import (
	"math"
	"net/mail"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	"time"

	"apimarket/internal/domain/money"
)

// Rules for listing APIs in the catalog: providers, APIs and plans. The
// database enforces the same core rules as a second line of defence.

// ---------- providers ----------

type Provider struct {
	ID                string
	Name              string
	ContactEmail      string
	AgreementSignedAt *time.Time // resale agreement; required before publishing
	Status            string
	CreatedAt         time.Time
}

func NewProvider(name, email string, agreementSigned bool, now time.Time) (Provider, error) {
	p := Provider{Name: strings.TrimSpace(name), ContactEmail: strings.TrimSpace(email), Status: "active"}
	if p.Name == "" {
		return Provider{}, Invalid("NAME_REQUIRED", "name is required")
	}
	if p.ContactEmail != "" {
		if a, err := mail.ParseAddress(p.ContactEmail); err != nil || a.Address != p.ContactEmail {
			return Provider{}, Invalid("INVALID_EMAIL", "contact_email is not a valid email address")
		}
	}
	if agreementSigned {
		t := now.UTC()
		p.AgreementSignedAt = &t
	}
	return p, nil
}

// ---------- APIs ----------

const (
	AuthNone   = "none"
	AuthAPIKey = "api_key" // secret in a header of the provider's choosing
	AuthBearer = "bearer"  // Authorization: Bearer <secret>
	AuthBasic  = "basic"   // Authorization: Basic base64(<user:password>)
	AuthQuery  = "query"   // secret in a query parameter of the provider's choosing
	AuthOAuth  = "oauth_cc"

	APIDraft     = "draft"
	APIPublished = "published"
	APISuspended = "suspended"
	APIRetired   = "retired"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

// Slugs are the first path segment on the gateway, so they must not shadow
// gateway routes or names we may want for platform paths later.
var reservedSlugs = map[string]bool{
	"healthz": true, "readyz": true, "metrics": true, "status": true, "v1": true, "v2": true,
	"api": true, "admin": true, "console": true, "app": true, "docs": true, "www": true,
}

var headerNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,63}$`)

// Headers a provider credential must not be injected into: transport-level
// headers, and ones the gateway strips or controls for other reasons.
var forbiddenAuthHeaders = map[string]bool{
	"Host": true, "Content-Length": true, "Content-Type": true, "Transfer-Encoding": true, "Connection": true,
	"Keep-Alive": true, "Upgrade": true, "Te": true, "Trailer": true, "Cookie": true,
	"Proxy-Authorization": true, "Proxy-Authenticate": true, "X-Forwarded-For": true, "Idempotency-Key": true,
}

func ValidateSlug(s string) error {
	if !slugRe.MatchString(s) {
		return Invalid("INVALID_SLUG", "API path must be 2-63 characters: lowercase letters, digits and hyphens, not starting with a hyphen")
	}
	if reservedSlugs[s] {
		return Invalid("RESERVED_SLUG", "API path /"+s+" is reserved")
	}
	return nil
}

// ValidateUpstreamURL checks a provider base or mock URL. The gateway appends
// the customer's path and query, so the URL itself must not carry either a
// query or a fragment, and credentials belong in the sealed secret, not here.
func ValidateUpstreamURL(field, raw string) error {
	code := "INVALID_" + strings.ToUpper(field)
	u, err := url.Parse(raw)
	switch {
	case err != nil, u.Scheme != "http" && u.Scheme != "https", u.Host == "", u.Hostname() == "":
		return Invalid(code, field+" must be an absolute http(s) URL")
	case u.User != nil:
		return Invalid(code, field+" must not contain credentials; use the auth secret")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return Invalid(code, field+" must not have a query string or fragment")
	}
	return nil
}

// Header is a fixed name/value pair (e.g. a provider's client id).
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

var queryParamRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,64}$`)

// NormalizeAuth validates how the gateway authenticates to the provider and
// returns the canonical header (api_key) or query parameter (query) name; it
// is empty for the other types. hasSecret says whether a credential is (or
// will be) stored for the API.
func NormalizeAuth(typ, name string, hasSecret bool) (string, error) {
	name = strings.TrimSpace(name)
	needSecret := func(n string) (string, error) {
		if !hasSecret {
			return "", Invalid("SECRET_REQUIRED", typ+" auth needs the provider's secret")
		}
		return n, nil
	}
	switch typ {
	case AuthNone:
		if name != "" || hasSecret {
			return "", Invalid("AUTH_UNUSED", "auth type none takes no header or secret")
		}
		return "", nil
	case AuthAPIKey:
		if !headerNameRe.MatchString(name) {
			return "", Invalid("INVALID_AUTH_HEADER", "api_key auth needs a header name such as X-Api-Key")
		}
		name = textproto.CanonicalMIMEHeaderKey(name)
		if forbiddenAuthHeaders[name] {
			return "", Invalid("INVALID_AUTH_HEADER", "the credential cannot be sent in the "+name+" header")
		}
		return needSecret(name)
	case AuthBearer, AuthBasic:
		if name != "" {
			return "", Invalid("AUTH_UNUSED", typ+" auth always uses the Authorization header; leave the name empty")
		}
		return needSecret("")
	case AuthQuery:
		if !queryParamRe.MatchString(name) {
			return "", Invalid("INVALID_AUTH_PARAM", "query auth needs a parameter name such as api_key")
		}
		return needSecret(name)
	case AuthOAuth:
		return "", Invalid("AUTH_NOT_SUPPORTED", "oauth_cc is not supported by the gateway yet")
	}
	return "", Invalid("INVALID_AUTH_TYPE", "auth type must be none, api_key, bearer, basic or query")
}

// ValidateBasicSecret checks the plaintext of a basic-auth credential.
func ValidateBasicSecret(secret string) error {
	if user, _, ok := strings.Cut(secret, ":"); !ok || user == "" {
		return Invalid("INVALID_BASIC_SECRET", "basic auth secret must be user:password")
	}
	return nil
}

const maxUpstreamHeaders = 20

// NormalizeUpstreamHeaders validates the fixed headers sent to the provider.
// They must not be transport headers or collide with the credential's header.
func NormalizeUpstreamHeaders(hs []Header, authType, authName string) ([]Header, error) {
	if len(hs) > maxUpstreamHeaders {
		return nil, Invalid("TOO_MANY_HEADERS", "at most 20 fixed upstream headers")
	}
	authHeader := ""
	switch authType {
	case AuthAPIKey:
		authHeader = authName
	case AuthBearer, AuthBasic:
		authHeader = "Authorization"
	}
	out := make([]Header, 0, len(hs))
	seen := map[string]bool{}
	for _, h := range hs {
		name := strings.TrimSpace(h.Name)
		if !headerNameRe.MatchString(name) {
			return nil, Invalid("INVALID_UPSTREAM_HEADER", "header name "+quote(name)+" is not valid")
		}
		name = textproto.CanonicalMIMEHeaderKey(name)
		switch {
		case forbiddenAuthHeaders[name]:
			return nil, Invalid("INVALID_UPSTREAM_HEADER", "the gateway controls the "+name+" header")
		case name == authHeader:
			return nil, Invalid("INVALID_UPSTREAM_HEADER", name+" already carries the credential")
		case seen[name]:
			return nil, Invalid("INVALID_UPSTREAM_HEADER", name+" is listed twice")
		case len(h.Value) > 1024 || strings.ContainsAny(h.Value, "\r\n\x00"):
			return nil, Invalid("INVALID_UPSTREAM_HEADER", name+" has an invalid value")
		}
		seen[name] = true
		out = append(out, Header{Name: name, Value: strings.TrimSpace(h.Value)})
	}
	return out, nil
}

func quote(s string) string { return "\"" + s + "\"" }

// ValidateListing checks every provider-facing field of an API (not its
// status). It normalises the text fields and the auth header in place.
func (a *API) ValidateListing() error {
	a.Name, a.Description, a.Category = strings.TrimSpace(a.Name), strings.TrimSpace(a.Description), strings.TrimSpace(a.Category)
	if a.Category == "" {
		a.Category = "other"
	}
	if err := ValidateSlug(a.Slug); err != nil {
		return err
	}
	if a.Name == "" {
		return Invalid("NAME_REQUIRED", "name is required")
	}
	if len(a.Name) > 200 || len(a.Description) > 5000 || len(a.Category) > 64 {
		return Invalid("TOO_LONG", "name (200), description (5000) or category (64) is too long")
	}
	if err := ValidateUpstreamURL("base_url", a.BaseURL); err != nil {
		return err
	}
	if a.MockURL != "" {
		if err := ValidateUpstreamURL("mock_url", a.MockURL); err != nil {
			return err
		}
	}
	if math.IsNaN(a.UpstreamRPS) || math.IsInf(a.UpstreamRPS, 0) || a.UpstreamRPS < 0 {
		return Invalid("INVALID_RATE_LIMIT", "upstream_rate_limit_rps must be 0 (unlimited) or positive")
	}
	h, err := NormalizeAuth(a.AuthType, a.AuthHeader, a.SecretSealed != "")
	if err != nil {
		return err
	}
	a.AuthHeader = h
	hs, err := NormalizeUpstreamHeaders(a.UpstreamHeaders, a.AuthType, a.AuthHeader)
	if err != nil {
		return err
	}
	a.UpstreamHeaders = hs
	return nil
}

// CheckPublish reports whether an API may go live. activePlans is the number
// of plans customers can subscribe to.
func CheckPublish(a API, p Provider, activePlans int) error {
	switch {
	case a.Status == APIRetired:
		return Conflict("API_RETIRED", "a retired API cannot be published")
	case p.AgreementSignedAt == nil:
		return Conflict("AGREEMENT_REQUIRED", "the provider has not signed the resale agreement")
	case p.Status != "active":
		return Conflict("PROVIDER_INACTIVE", "the provider is not active")
	case activePlans == 0:
		return Conflict("NO_ACTIVE_PLAN", "add at least one plan before publishing")
	}
	return nil
}

// ---------- plans ----------

type PlanInput struct {
	Name                string
	Kind                PlanKind
	MonthlyFee          money.Micro
	IncludedCalls       int64
	PricePerCall        money.Micro
	ProviderCostPerCall money.Micro
	RateLimitRPS        float64
}

// NewPlan applies the pricing rules:
//   - free: no fee and no per-call price, and a monthly call quota
//   - tier: a monthly fee with a call quota (an optional per-call price is charged on top)
//   - payg: a per-call price and no monthly fee
//
// Paid plans must not sell below the provider's per-call cost.
func NewPlan(apiID string, in PlanInput) (Plan, error) {
	p := Plan{APIID: apiID, Name: strings.TrimSpace(in.Name), Kind: in.Kind, MonthlyFee: in.MonthlyFee,
		IncludedCalls: in.IncludedCalls, PricePerCall: in.PricePerCall, ProviderCostPerCall: in.ProviderCostPerCall,
		RateLimitRPS: in.RateLimitRPS, Status: "active"}
	switch {
	case p.Name == "" || len(p.Name) > 100:
		return Plan{}, Invalid("NAME_REQUIRED", "plan name is required (up to 100 characters)")
	case p.MonthlyFee < 0 || p.PricePerCall < 0 || p.ProviderCostPerCall < 0 || p.IncludedCalls < 0:
		return Plan{}, Invalid("NEGATIVE_AMOUNT", "amounts and included_calls cannot be negative")
	case math.IsNaN(p.RateLimitRPS) || math.IsInf(p.RateLimitRPS, 0) || p.RateLimitRPS < 0:
		return Plan{}, Invalid("INVALID_RATE_LIMIT", "rate_limit_rps must be 0 (unlimited) or positive")
	}
	switch p.Kind {
	case PlanFree:
		if p.MonthlyFee != 0 || p.PricePerCall != 0 {
			return Plan{}, Invalid("INVALID_PLAN", "free plans have no monthly fee or per-call price")
		}
		if p.IncludedCalls == 0 {
			return Plan{}, Invalid("INVALID_PLAN", "free plans need a monthly call quota (included_calls)")
		}
	case PlanTier:
		if p.MonthlyFee == 0 || p.IncludedCalls == 0 {
			return Plan{}, Invalid("INVALID_PLAN", "tier plans need a monthly fee and included_calls")
		}
	case PlanPAYG:
		if p.PricePerCall == 0 || p.MonthlyFee != 0 {
			return Plan{}, Invalid("INVALID_PLAN", "pay-per-call plans need a per-call price and no monthly fee")
		}
	default:
		return Plan{}, Invalid("INVALID_PLAN_KIND", "kind must be free, tier or payg")
	}
	if p.Kind != PlanFree && p.PricePerCall < p.ProviderCostPerCall {
		return Plan{}, Invalid("NEGATIVE_MARGIN", "price_per_call cannot be below provider_cost_per_call")
	}
	return p, nil
}
