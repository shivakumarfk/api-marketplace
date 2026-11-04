package domain

import (
	"testing"
	"time"

	"apimarket/internal/domain/money"
)

func wantCode(t *testing.T, label string, err error, code string) {
	t.Helper()
	e, ok := AsError(err)
	if code == "" {
		if err != nil {
			t.Errorf("%s: unexpected error %v", label, err)
		}
		return
	}
	if !ok || e.Code != code {
		t.Errorf("%s: got %v, want %s", label, err, code)
	}
}

func TestNewProvider(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.FixedZone("IST", 19800))
	p, err := NewProvider(" Acme ", "ops@acme.example", true, now)
	if err != nil || p.Name != "Acme" || p.AgreementSignedAt == nil || !p.AgreementSignedAt.Equal(now) || p.Status != "active" {
		t.Fatalf("%+v %v", p, err)
	}
	if p, _ := NewProvider("Acme", "", false, now); p.AgreementSignedAt != nil {
		t.Fatal("unsigned agreement must stay nil")
	}
	_, err = NewProvider(" ", "", false, now)
	wantCode(t, "blank name", err, "NAME_REQUIRED")
	for _, e := range []string{"nope", "Ops <ops@acme.example>", "a@"} {
		_, err = NewProvider("Acme", e, false, now)
		wantCode(t, e, err, "INVALID_EMAIL")
	}
}

func TestValidateSlug(t *testing.T) {
	for s, code := range map[string]string{
		"weather": "", "ifsc-v2": "", "a1": "",
		"a": "INVALID_SLUG", "-x": "INVALID_SLUG", "Weather": "INVALID_SLUG", "a_b": "INVALID_SLUG", "a/b": "INVALID_SLUG",
		"healthz": "RESERVED_SLUG", "v1": "RESERVED_SLUG", "console": "RESERVED_SLUG",
	} {
		wantCode(t, s, ValidateSlug(s), code)
	}
}

func TestValidateUpstreamURL(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://api.open-meteo.com/v1": true, "http://mockprovider:9000": true, "https://x.example/": true,
		"ftp://x.example": false, "x.example": false, "/relative": false, "https://": false,
		"https://user:pw@x.example": false, "https://x.example?key=1": false, "https://x.example/?": false,
		"https://x.example/#frag": false, "": false,
	} {
		err := ValidateUpstreamURL("base_url", u)
		if (err == nil) != ok {
			t.Errorf("%q: ok=%v, err=%v", u, ok, err)
		}
		if err != nil {
			wantCode(t, u, err, "INVALID_BASE_URL")
		}
	}
}

func TestNormalizeAuth(t *testing.T) {
	cases := []struct {
		typ, header string
		secret      bool
		wantHeader  string
		code        string
	}{
		{AuthNone, "", false, "", ""},
		{AuthNone, "X-Key", false, "", "AUTH_UNUSED"},
		{AuthNone, "", true, "", "AUTH_UNUSED"},
		{AuthAPIKey, "x-api-key", true, "X-Api-Key", ""},
		{AuthAPIKey, "authorization", true, "Authorization", ""},
		{AuthAPIKey, "", true, "", "INVALID_AUTH_HEADER"},
		{AuthAPIKey, "X Key", true, "", "INVALID_AUTH_HEADER"},
		{AuthAPIKey, "host", true, "", "INVALID_AUTH_HEADER"},
		{AuthAPIKey, "Cookie", true, "", "INVALID_AUTH_HEADER"},
		{AuthAPIKey, "X-Key", false, "", "SECRET_REQUIRED"},
		{AuthOAuth, "", true, "", "AUTH_NOT_SUPPORTED"},
		{"basic", "", true, "", "INVALID_AUTH_TYPE"},
	}
	for _, c := range cases {
		h, err := NormalizeAuth(c.typ, c.header, c.secret)
		wantCode(t, c.typ+"/"+c.header, err, c.code)
		if err == nil && h != c.wantHeader {
			t.Errorf("%s/%s: header %q, want %q", c.typ, c.header, h, c.wantHeader)
		}
	}
}

func TestValidateListing(t *testing.T) {
	good := func() API {
		return API{Slug: "ifsc", Name: " IFSC ", BaseURL: "https://ifsc.razorpay.com", AuthType: AuthNone}
	}
	a := good()
	if err := a.ValidateListing(); err != nil || a.Name != "IFSC" || a.Category != "other" {
		t.Fatalf("%+v %v", a, err)
	}
	for label, c := range map[string]struct {
		mut  func(*API)
		code string
	}{
		"slug":     {func(a *API) { a.Slug = "Bad" }, "INVALID_SLUG"},
		"name":     {func(a *API) { a.Name = "  " }, "NAME_REQUIRED"},
		"base":     {func(a *API) { a.BaseURL = "ifsc.razorpay.com" }, "INVALID_BASE_URL"},
		"mock":     {func(a *API) { a.MockURL = "nope" }, "INVALID_MOCK_URL"},
		"rps":      {func(a *API) { a.UpstreamRPS = -1 }, "INVALID_RATE_LIMIT"},
		"auth":     {func(a *API) { a.AuthType = AuthAPIKey; a.AuthHeader = "X-Key" }, "SECRET_REQUIRED"},
		"category": {func(a *API) { a.Category = string(make([]byte, 65)) }, "TOO_LONG"},
	} {
		a := good()
		c.mut(&a)
		wantCode(t, label, a.ValidateListing(), c.code)
	}
	a = good()
	a.AuthType, a.AuthHeader, a.SecretSealed = AuthAPIKey, "x-provider-key", "sealed"
	if err := a.ValidateListing(); err != nil || a.AuthHeader != "X-Provider-Key" {
		t.Fatalf("api_key: %+v %v", a, err)
	}
}

func TestCheckPublish(t *testing.T) {
	signed := time.Now()
	prov := Provider{Status: "active", AgreementSignedAt: &signed}
	api := API{Status: APIDraft}
	wantCode(t, "ok", CheckPublish(api, prov, 1), "")
	wantCode(t, "no plan", CheckPublish(api, prov, 0), "NO_ACTIVE_PLAN")
	wantCode(t, "unsigned", CheckPublish(api, Provider{Status: "active"}, 1), "AGREEMENT_REQUIRED")
	wantCode(t, "inactive", CheckPublish(api, Provider{Status: "suspended", AgreementSignedAt: &signed}, 1), "PROVIDER_INACTIVE")
	wantCode(t, "retired", CheckPublish(API{Status: APIRetired}, prov, 1), "API_RETIRED")
	if e, _ := AsError(CheckPublish(api, prov, 0)); e.Kind != KindConflict {
		t.Fatal("publish preconditions are conflicts (409), not bad input")
	}
}

func TestNewPlan(t *testing.T) {
	r := money.FromRupees
	cases := []struct {
		label string
		in    PlanInput
		code  string
	}{
		{"free", PlanInput{Name: "Free", Kind: PlanFree, IncludedCalls: 100, RateLimitRPS: 2}, ""},
		{"free with fee", PlanInput{Name: "Free", Kind: PlanFree, MonthlyFee: r(1), IncludedCalls: 100}, "INVALID_PLAN"},
		{"free no quota", PlanInput{Name: "Free", Kind: PlanFree}, "INVALID_PLAN"},
		{"free with provider cost", PlanInput{Name: "Free", Kind: PlanFree, IncludedCalls: 100, ProviderCostPerCall: 10}, ""},
		{"tier", PlanInput{Name: "Starter", Kind: PlanTier, MonthlyFee: r(499), IncludedCalls: 10_000}, ""},
		{"tier no quota", PlanInput{Name: "Starter", Kind: PlanTier, MonthlyFee: r(499)}, "INVALID_PLAN"},
		{"tier below cost", PlanInput{Name: "S", Kind: PlanTier, MonthlyFee: r(1), IncludedCalls: 1, ProviderCostPerCall: 5}, "NEGATIVE_MARGIN"},
		{"payg", PlanInput{Name: "PAYG", Kind: PlanPAYG, PricePerCall: 20_000, ProviderCostPerCall: 10_000}, ""},
		{"payg free", PlanInput{Name: "PAYG", Kind: PlanPAYG}, "INVALID_PLAN"},
		{"payg fee", PlanInput{Name: "PAYG", Kind: PlanPAYG, PricePerCall: 1, MonthlyFee: 1}, "INVALID_PLAN"},
		{"payg below cost", PlanInput{Name: "PAYG", Kind: PlanPAYG, PricePerCall: 1, ProviderCostPerCall: 2}, "NEGATIVE_MARGIN"},
		{"negative", PlanInput{Name: "X", Kind: PlanPAYG, PricePerCall: -1}, "NEGATIVE_AMOUNT"},
		{"rps", PlanInput{Name: "X", Kind: PlanPAYG, PricePerCall: 1, RateLimitRPS: -1}, "INVALID_RATE_LIMIT"},
		{"no name", PlanInput{Kind: PlanPAYG, PricePerCall: 1}, "NAME_REQUIRED"},
		{"kind", PlanInput{Name: "X", Kind: "annual"}, "INVALID_PLAN_KIND"},
	}
	for _, c := range cases {
		p, err := NewPlan("api-1", c.in)
		wantCode(t, c.label, err, c.code)
		if err == nil && (p.APIID != "api-1" || p.Status != "active") {
			t.Errorf("%s: %+v", c.label, p)
		}
	}
}
