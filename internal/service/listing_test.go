package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"apimarket/internal/domain"
)

type listingFixture struct {
	m   *mem
	lm  *listingMem
	svc *ListingService
}

func setupListing() listingFixture {
	m := newMem()
	lm := newListingMem(m)
	return listingFixture{m: m, lm: lm, svc: &ListingService{Tx: m, Providers: memProviders{lm}, APIs: memAPIs{lm},
		Plans: memPlans{lm}, Endpoints: memEndpoints{lm}, Audit: memAudit{m}, Secrets: fakeSealer{lm},
		Clock: fixedClock{time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)}}}
}

func (f listingFixture) provider(t *testing.T, signed bool) domain.Provider {
	t.Helper()
	p, err := f.svc.CreateProvider(context.Background(), admin, CreateProviderInput{Name: "Razorpay", AgreementSigned: signed})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f listingFixture) api(t *testing.T, provID, slug string) APIDetail {
	t.Helper()
	d, err := f.svc.CreateAPI(context.Background(), admin, CreateAPIInput{ProviderID: provID, Slug: slug, Name: "IFSC",
		BaseURL: "https://ifsc.razorpay.com"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

var payg = domain.PlanInput{Name: "PAYG", Kind: domain.PlanPAYG, PricePerCall: 20_000, RateLimitRPS: 2}

func TestCreateProvider(t *testing.T) {
	f := setupListing()
	p := f.provider(t, true)
	if p.ID == "" || p.AgreementSignedAt == nil || !p.AgreementSignedAt.Equal(f.svc.Clock.Now()) {
		t.Fatalf("%+v", p)
	}
	if len(f.m.audit) != 1 || f.m.audit[0].Action != "provider.created" || f.m.audit[0].OrgID != "" {
		t.Fatalf("audit %+v", f.m.audit)
	}
	if _, err := f.svc.CreateProvider(context.Background(), admin, CreateProviderInput{}); codeOf(err) != "NAME_REQUIRED" {
		t.Fatal(err)
	}
	f.m.failAudit = true
	if _, err := f.svc.CreateProvider(context.Background(), admin, CreateProviderInput{Name: "X"}); err == nil {
		t.Fatal("audit failure must fail the create")
	}
}

func TestSignAgreementUnblocksPublish(t *testing.T) {
	ctx := context.Background()
	f := setupListing()
	p := f.provider(t, false)
	f.api(t, p.ID, "ifsc")
	f.svc.CreatePlan(ctx, admin, "ifsc", payg)
	if _, err := f.svc.Publish(ctx, admin, "ifsc"); codeOf(err) != "AGREEMENT_REQUIRED" {
		t.Fatal(err)
	}
	signed, err := f.svc.SignAgreement(ctx, admin, p.ID)
	if err != nil || signed.AgreementSignedAt == nil {
		t.Fatalf("%+v %v", signed, err)
	}
	n := len(f.m.audit)
	again, err := f.svc.SignAgreement(ctx, admin, p.ID)
	if err != nil || !again.AgreementSignedAt.Equal(*signed.AgreementSignedAt) || len(f.m.audit) != n {
		t.Fatal("signing twice keeps the date and writes no audit row")
	}
	if _, err := f.svc.Publish(ctx, admin, "ifsc"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SignAgreement(ctx, admin, "nope"); codeOf(err) != "PROVIDER_NOT_FOUND" {
		t.Fatal(err)
	}
}

func TestCreateAPI(t *testing.T) {
	ctx := context.Background()
	f := setupListing()
	p := f.provider(t, true)

	d := f.api(t, p.ID, "ifsc")
	if d.API.Status != domain.APIDraft || d.API.AuthType != domain.AuthNone || d.API.Category != "other" || d.Plans == nil {
		t.Fatalf("defaults: %+v", d)
	}

	// api_key: secret is sealed, header canonicalised, plaintext absent from storage and audit.
	d, err := f.svc.CreateAPI(ctx, admin, CreateAPIInput{ProviderID: p.ID, Slug: "weather", Name: "W",
		BaseURL: "https://w.example", Auth: AuthInput{Type: "api_key", Header: "x-api-key", Secret: "s3cret-value"}})
	if err != nil {
		t.Fatal(err)
	}
	stored := f.lm.apisByID[d.API.ID]
	if stored.AuthHeader != "X-Api-Key" || stored.SecretSealed == "s3cret-value" || unseal(stored.SecretSealed) != "s3cret-value" {
		t.Fatalf("stored %+v", stored)
	}
	if strings.Contains(fmt.Sprint(f.m.audit), "s3cret-value") {
		t.Fatal("secret leaked into the audit log")
	}

	cases := []struct {
		in   CreateAPIInput
		code string
	}{
		{CreateAPIInput{ProviderID: p.ID, Slug: "ifsc", Name: "dup", BaseURL: "https://x.example"}, "SLUG_TAKEN"},
		{CreateAPIInput{ProviderID: "nope", Slug: "new-one", Name: "N", BaseURL: "https://x.example"}, "PROVIDER_NOT_FOUND"},
		{CreateAPIInput{ProviderID: p.ID, Slug: "healthz", Name: "N", BaseURL: "https://x.example"}, "RESERVED_SLUG"},
		{CreateAPIInput{ProviderID: p.ID, Slug: "new-one", Name: "N", BaseURL: "https://x.example?k=1"}, "INVALID_BASE_URL"},
		{CreateAPIInput{ProviderID: p.ID, Slug: "new-one", Name: "N", BaseURL: "https://x.example",
			Auth: AuthInput{Type: "none", Secret: "x"}}, "AUTH_UNUSED"},
		{CreateAPIInput{ProviderID: p.ID, Slug: "new-one", Name: "N", BaseURL: "https://x.example",
			Auth: AuthInput{Type: "api_key", Header: "X-Key"}}, "SECRET_REQUIRED"},
		{CreateAPIInput{ProviderID: p.ID, Slug: "new-one", Name: "N", BaseURL: "https://x.example",
			Auth: AuthInput{Type: "oauth_cc", Secret: "x"}}, "AUTH_UNUSED"},
	}
	for _, c := range cases {
		if _, err := f.svc.CreateAPI(ctx, admin, c.in); codeOf(err) != c.code {
			t.Errorf("%+v: got %v, want %s", c.in, err, c.code)
		}
	}

	f.lm.failSeal = true
	if _, err := f.svc.CreateAPI(ctx, admin, CreateAPIInput{ProviderID: p.ID, Slug: "sealfail", Name: "N",
		BaseURL: "https://x.example", Auth: AuthInput{Type: "api_key", Header: "X-Key", Secret: "s"}}); err == nil {
		t.Fatal("seal failure must fail the create")
	}
	if _, err := f.svc.GetAPI(ctx, "sealfail"); codeOf(err) != "API_NOT_FOUND" {
		t.Fatalf("nothing stored on failure: %v", err)
	}
}

func TestUpdateAPI(t *testing.T) {
	ctx := context.Background()
	f := setupListing()
	p := f.provider(t, true)
	f.api(t, p.ID, "ifsc")
	str := func(s string) *string { return &s }

	d, err := f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{Description: str("Bank branches"), BaseURL: str("https://ifsc2.example")})
	if err != nil || d.API.Description != "Bank branches" || d.API.BaseURL != "https://ifsc2.example" || d.API.Name != "IFSC" {
		t.Fatalf("%+v %v", d, err)
	}
	last := f.m.audit[len(f.m.audit)-1]
	if last.Action != "api.updated" || fmt.Sprint(last.Metadata["fields"]) != "[description base_url]" {
		t.Fatalf("audit %+v", last)
	}

	// Switch to api_key with a secret, then rename the header keeping the secret, then rotate it.
	if _, err := f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{Auth: &AuthInput{Type: "api_key", Header: "X-Key"}}); codeOf(err) != "SECRET_REQUIRED" {
		t.Fatalf("switching to api_key needs a secret: %v", err)
	}
	if _, err := f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{Auth: &AuthInput{Type: "api_key", Header: "X-Key", Secret: "one"}}); err != nil {
		t.Fatal(err)
	}
	d, err = f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{Auth: &AuthInput{Type: "api_key", Header: "authorization"}})
	if err != nil || d.API.AuthHeader != "Authorization" || unseal(d.API.SecretSealed) != "one" {
		t.Fatalf("keep secret: %+v %v", d.API, err)
	}
	if f.m.audit[len(f.m.audit)-1].Metadata["secret_rotated"] != false {
		t.Fatal("header rename is not a rotation")
	}
	d, _ = f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{Auth: &AuthInput{Type: "api_key", Header: "Authorization", Secret: "two"}})
	if unseal(d.API.SecretSealed) != "two" || f.m.audit[len(f.m.audit)-1].Metadata["secret_rotated"] != true {
		t.Fatalf("rotate: %+v", d.API)
	}
	d, _ = f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{Auth: &AuthInput{Type: "none"}})
	if d.API.SecretSealed != "" || d.API.AuthHeader != "" {
		t.Fatalf("none clears the credential: %+v", d.API)
	}

	if _, err := f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{}); codeOf(err) != "NOTHING_TO_UPDATE" {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateAPI(ctx, admin, "ifsc", UpdateAPIInput{BaseURL: str("nope")}); codeOf(err) != "INVALID_BASE_URL" {
		t.Fatal(err)
	}
	if got := f.lm.apisByID[d.API.ID].BaseURL; got != "https://ifsc2.example" {
		t.Fatalf("invalid update must not be stored: %s", got)
	}
	if _, err := f.svc.UpdateAPI(ctx, admin, "missing", UpdateAPIInput{Name: str("x")}); codeOf(err) != "API_NOT_FOUND" {
		t.Fatal(err)
	}
}

func TestPublishSuspendLifecycle(t *testing.T) {
	ctx := context.Background()
	f := setupListing()
	unsigned := f.provider(t, false)
	signed := f.provider(t, true)
	f.api(t, unsigned.ID, "unsigned-api")
	f.api(t, signed.ID, "ifsc")

	if _, err := f.svc.Publish(ctx, admin, "ifsc"); codeOf(err) != "NO_ACTIVE_PLAN" {
		t.Fatalf("no plan: %v", err)
	}
	if _, err := f.svc.Suspend(ctx, admin, "ifsc"); codeOf(err) != "NOT_PUBLISHED" {
		t.Fatalf("suspend draft: %v", err)
	}
	if _, err := f.svc.CreatePlan(ctx, admin, "unsigned-api", payg); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Publish(ctx, admin, "unsigned-api"); codeOf(err) != "AGREEMENT_REQUIRED" {
		t.Fatalf("unsigned: %v", err)
	}

	plan, err := f.svc.CreatePlan(ctx, admin, "ifsc", payg)
	if err != nil || plan.Status != "active" || plan.PricePerCall != 20_000 {
		t.Fatalf("%+v %v", plan, err)
	}
	if _, err := f.svc.CreatePlan(ctx, admin, "ifsc", domain.PlanInput{Name: "bad", Kind: domain.PlanPAYG}); codeOf(err) != "INVALID_PLAN" {
		t.Fatal(err)
	}
	d, err := f.svc.Publish(ctx, admin, "ifsc")
	if err != nil || d.API.Status != domain.APIPublished || len(d.Plans) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	n := len(f.m.audit)
	if d, err := f.svc.Publish(ctx, admin, "ifsc"); err != nil || d.API.Status != domain.APIPublished || len(f.m.audit) != n {
		t.Fatal("re-publishing is a no-op without a new audit row")
	}

	if _, err := f.svc.RetirePlan(ctx, admin, plan.ID); codeOf(err) != "LAST_ACTIVE_PLAN" {
		t.Fatalf("last plan: %v", err)
	}
	second, _ := f.svc.CreatePlan(ctx, admin, "ifsc", domain.PlanInput{Name: "Free", Kind: domain.PlanFree, IncludedCalls: 100})
	if p, err := f.svc.RetirePlan(ctx, admin, plan.ID); err != nil || p.Status != "retired" {
		t.Fatalf("%+v %v", p, err)
	}
	if p, err := f.svc.RetirePlan(ctx, admin, plan.ID); err != nil || p.Status != "retired" {
		t.Fatal("retiring twice is a no-op")
	}
	if _, err := f.svc.RetirePlan(ctx, admin, "nope"); codeOf(err) != "PLAN_NOT_FOUND" {
		t.Fatal(err)
	}

	d, err = f.svc.Suspend(ctx, admin, "ifsc")
	if err != nil || d.API.Status != domain.APISuspended {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := f.svc.RetirePlan(ctx, admin, second.ID); err != nil {
		t.Fatalf("a suspended API's last plan can be retired: %v", err)
	}
	if _, err := f.svc.Publish(ctx, admin, "ifsc"); codeOf(err) != "NO_ACTIVE_PLAN" {
		t.Fatal(err)
	}

	var actions []string
	for _, e := range f.m.audit {
		actions = append(actions, e.Action)
	}
	want := "provider.created provider.created api.created api.created plan.created plan.created api.published plan.created plan.retired api.suspended plan.retired"
	if strings.Join(actions, " ") != want {
		t.Fatalf("audit trail:\n got %v\nwant %s", actions, want)
	}
}

func TestListAPIs(t *testing.T) {
	ctx := context.Background()
	f := setupListing()
	p := f.provider(t, true)
	f.api(t, p.ID, "zeta")
	f.api(t, p.ID, "alpha")
	f.svc.CreatePlan(ctx, admin, "alpha", payg)
	list, err := f.svc.ListAPIs(ctx)
	if err != nil || len(list) != 2 || list[0].API.Slug != "alpha" || len(list[0].Plans) != 1 || len(list[1].Plans) != 0 {
		t.Fatalf("%+v %v", list, err)
	}
	if provs, _ := f.svc.ListProviders(ctx); len(provs) != 1 {
		t.Fatal(provs)
	}
}

func TestEndpointsLifecycle(t *testing.T) {
	ctx := context.Background()
	f := setupListing()
	p := f.provider(t, true)
	f.api(t, p.ID, "ifsc")

	e, err := f.svc.CreateEndpoint(ctx, admin, "ifsc", domain.Endpoint{Method: "get", Path: "/{code}",
		Params: []domain.Param{{Name: "code", In: "path", Example: "HDFC0CAGSBK"}}, ResponseExample: `{"BANK":"HDFC Bank"}`})
	if err != nil || e.ID == "" || e.Method != "GET" || !e.Params[0].Required || e.ResponseStatus != 200 {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := f.svc.CreateEndpoint(ctx, admin, "ifsc", domain.Endpoint{Method: "GET", Path: "/{code}",
		Params: []domain.Param{{Name: "code", In: "path"}}}); codeOf(err) != "ENDPOINT_EXISTS" {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := f.svc.CreateEndpoint(ctx, admin, "ifsc", domain.Endpoint{Method: "GET", Path: "/{code}"}); codeOf(err) != "INVALID_PARAM" {
		t.Fatalf("validation runs before storage: %v", err)
	}
	if _, err := f.svc.CreateEndpoint(ctx, admin, "missing", domain.Endpoint{Method: "GET", Path: "/"}); codeOf(err) != "API_NOT_FOUND" {
		t.Fatal(err)
	}

	up, err := f.svc.UpdateEndpoint(ctx, admin, e.ID, domain.Endpoint{Method: "GET", Path: "/{code}", Summary: "Branch by IFSC",
		Params: []domain.Param{{Name: "code", In: "path"}}})
	if err != nil || up.ID != e.ID || up.APIID != e.APIID || up.Summary != "Branch by IFSC" {
		t.Fatalf("%+v %v", up, err)
	}
	d, _ := f.svc.GetAPI(ctx, "ifsc")
	if len(d.Endpoints) != 1 || d.Endpoints[0].Summary != "Branch by IFSC" {
		t.Fatalf("detail includes endpoints: %+v", d.Endpoints)
	}

	if err := f.svc.DeleteEndpoint(ctx, admin, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteEndpoint(ctx, admin, e.ID); codeOf(err) != "ENDPOINT_NOT_FOUND" {
		t.Fatal(err)
	}
	if _, err := f.svc.UpdateEndpoint(ctx, admin, "nope", domain.Endpoint{Method: "GET", Path: "/"}); codeOf(err) != "ENDPOINT_NOT_FOUND" {
		t.Fatal(err)
	}
	var actions []string
	for _, a := range f.m.audit[len(f.m.audit)-3:] {
		actions = append(actions, a.Action)
	}
	if strings.Join(actions, " ") != "endpoint.created endpoint.updated endpoint.deleted" {
		t.Fatal(actions)
	}
}

func TestNewAuthTypesAndUpstreamHeaders(t *testing.T) {
	ctx := context.Background()
	f := setupListing()
	p := f.provider(t, true)
	base := CreateAPIInput{ProviderID: p.ID, Name: "X", BaseURL: "https://x.example"}

	in := base
	in.Slug, in.Auth = "bearer-api", AuthInput{Type: "bearer", Secret: "tok"}
	in.UpstreamHeaders = []domain.Header{{Name: "x-client-id", Value: "acme"}}
	d, err := f.svc.CreateAPI(ctx, admin, in)
	if err != nil || d.API.AuthType != "bearer" || unseal(d.API.SecretSealed) != "tok" || d.API.UpstreamHeaders[0].Name != "X-Client-Id" {
		t.Fatalf("%+v %v", d.API, err)
	}

	in = base
	in.Slug, in.Auth = "basic-api", AuthInput{Type: "basic", Secret: "no-colon"}
	if _, err := f.svc.CreateAPI(ctx, admin, in); codeOf(err) != "INVALID_BASIC_SECRET" {
		t.Fatalf("basic format: %v", err)
	}
	in.Auth.Secret = "user:pw"
	if _, err := f.svc.CreateAPI(ctx, admin, in); err != nil {
		t.Fatal(err)
	}

	in = base
	in.Slug, in.Auth = "query-api", AuthInput{Type: "query", Header: "appid", Secret: "k1"}
	if _, err := f.svc.CreateAPI(ctx, admin, in); err != nil {
		t.Fatal(err)
	}
	in.Slug, in.Auth.Secret = "newline", "k1\r\nX-Evil: 1"
	if _, err := f.svc.CreateAPI(ctx, admin, in); codeOf(err) != "INVALID_SECRET" {
		t.Fatalf("header injection via secret: %v", err)
	}

	// Moving a key from a query parameter to a header keeps it; switching to basic needs a new secret.
	d, err = f.svc.UpdateAPI(ctx, admin, "query-api", UpdateAPIInput{Auth: &AuthInput{Type: "api_key", Header: "X-Key"}})
	if err != nil || unseal(d.API.SecretSealed) != "k1" {
		t.Fatalf("keep across key-like types: %+v %v", d.API, err)
	}
	if _, err := f.svc.UpdateAPI(ctx, admin, "query-api", UpdateAPIInput{Auth: &AuthInput{Type: "basic"}}); codeOf(err) != "SECRET_REQUIRED" {
		t.Fatalf("to basic: %v", err)
	}
	hs := []domain.Header{{Name: "Host", Value: "x"}}
	if _, err := f.svc.UpdateAPI(ctx, admin, "query-api", UpdateAPIInput{UpstreamHeaders: &hs}); codeOf(err) != "INVALID_UPSTREAM_HEADER" {
		t.Fatal(err)
	}
	empty := []domain.Header{}
	d, err = f.svc.UpdateAPI(ctx, admin, "bearer-api", UpdateAPIInput{UpstreamHeaders: &empty})
	if err != nil || len(d.API.UpstreamHeaders) != 0 {
		t.Fatalf("clear headers: %+v %v", d.API.UpstreamHeaders, err)
	}
}
