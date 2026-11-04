package controlplane

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apimarket/internal/domain"
	"apimarket/internal/service"
)

// fakeListing records inputs and returns canned results (or err).
type fakeListing struct {
	err         error
	gotAPI      service.CreateAPIInput
	gotUpdate   service.UpdateAPIInput
	gotPlan     domain.PlanInput
	gotSlug     string
	gotEndpoint domain.Endpoint
}

var signedAt = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)

func (f *fakeListing) CreateProvider(_ context.Context, _ domain.Actor, in service.CreateProviderInput) (domain.Provider, error) {
	return domain.Provider{ID: "prov-1", Name: in.Name, AgreementSignedAt: &signedAt, Status: "active"}, f.err
}
func (f *fakeListing) SignAgreement(_ context.Context, _ domain.Actor, id string) (domain.Provider, error) {
	return domain.Provider{ID: id, Name: "P", AgreementSignedAt: &signedAt, Status: "active"}, f.err
}
func (f *fakeListing) ListProviders(context.Context) ([]domain.Provider, error) {
	return []domain.Provider{{ID: "prov-1", Name: "P", Status: "active"}}, f.err
}
func (f *fakeListing) detail(slug string) service.APIDetail {
	return service.APIDetail{API: domain.API{ID: "api-1", Slug: slug, Name: "IFSC", Status: "draft", AuthType: "api_key",
		AuthHeader: "X-Key", SecretSealed: "SEALED-CIPHERTEXT"},
		Plans: []domain.Plan{{ID: "plan-1", Kind: domain.PlanPAYG, PricePerCall: 20_000, Status: "active"}}}
}
func (f *fakeListing) CreateAPI(_ context.Context, _ domain.Actor, in service.CreateAPIInput) (service.APIDetail, error) {
	f.gotAPI = in
	return f.detail(in.Slug), f.err
}
func (f *fakeListing) GetAPI(_ context.Context, slug string) (service.APIDetail, error) {
	f.gotSlug = slug
	return f.detail(slug), f.err
}
func (f *fakeListing) ListAPIs(context.Context) ([]service.APIDetail, error) {
	return []service.APIDetail{f.detail("ifsc")}, f.err
}
func (f *fakeListing) UpdateAPI(_ context.Context, _ domain.Actor, slug string, in service.UpdateAPIInput) (service.APIDetail, error) {
	f.gotSlug, f.gotUpdate = slug, in
	return f.detail(slug), f.err
}
func (f *fakeListing) Publish(_ context.Context, _ domain.Actor, slug string) (service.APIDetail, error) {
	d := f.detail(slug)
	d.API.Status = "published"
	return d, f.err
}
func (f *fakeListing) Suspend(_ context.Context, _ domain.Actor, slug string) (service.APIDetail, error) {
	d := f.detail(slug)
	d.API.Status = "suspended"
	return d, f.err
}
func (f *fakeListing) CreatePlan(_ context.Context, _ domain.Actor, slug string, in domain.PlanInput) (domain.Plan, error) {
	f.gotSlug, f.gotPlan = slug, in
	return domain.Plan{ID: "plan-2", Name: in.Name, Kind: in.Kind, MonthlyFee: in.MonthlyFee, PricePerCall: in.PricePerCall,
		ProviderCostPerCall: in.ProviderCostPerCall, Status: "active"}, f.err
}
func (f *fakeListing) CreateEndpoint(_ context.Context, _ domain.Actor, slug string, e domain.Endpoint) (domain.Endpoint, error) {
	f.gotSlug, f.gotEndpoint = slug, e
	e.ID = "ep-1"
	return e, f.err
}
func (f *fakeListing) UpdateEndpoint(_ context.Context, _ domain.Actor, id string, e domain.Endpoint) (domain.Endpoint, error) {
	f.gotSlug, f.gotEndpoint = id, e
	e.ID = id
	return e, f.err
}
func (f *fakeListing) DeleteEndpoint(_ context.Context, _ domain.Actor, id string) error {
	f.gotSlug = id
	return f.err
}
func (f *fakeListing) RetirePlan(_ context.Context, _ domain.Actor, id string) (domain.Plan, error) {
	return domain.Plan{ID: id, Kind: domain.PlanPAYG, Status: "retired"}, f.err
}

func newListingServer(l *fakeListing) *httptest.Server {
	f := &fakeServices{}
	return httptest.NewServer(Router(Deps{Orgs: f, Keys: fakeKeys{}, Subs: f, Wallets: f, Catalog: f, Listing: l,
		AdminToken: "secret", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}))
}

func TestListingRoutesNeedAdmin(t *testing.T) {
	srv := newListingServer(&fakeListing{})
	defer srv.Close()
	for _, r := range [][2]string{{"POST", "/v1/admin/providers"}, {"GET", "/v1/admin/apis"}, {"PATCH", "/v1/admin/apis/x"},
		{"POST", "/v1/admin/apis/x/publish"}, {"POST", "/v1/admin/plans/p/retire"}} {
		if st, _ := call(t, srv, r[0], r[1], "", `{}`); st != 401 {
			t.Errorf("%s %s without token: %d", r[0], r[1], st)
		}
	}
}

func TestCreateProviderAndAPI(t *testing.T) {
	l := &fakeListing{}
	srv := newListingServer(l)
	defer srv.Close()

	st, m := call(t, srv, "POST", "/v1/admin/providers", "secret", `{"name":"Razorpay","agreement_signed":true}`)
	if st != 201 || m["id"] != "prov-1" || m["agreement_signed_at"] != "2026-11-04T10:00:00Z" {
		t.Fatalf("%d %v", st, m)
	}
	if st, m := call(t, srv, "POST", "/v1/admin/providers/prov-9/agreement", "secret", ""); st != 200 || m["id"] != "prov-9" || m["agreement_signed_at"] == nil {
		t.Fatalf("%d %v", st, m)
	}
	if st, m := call(t, srv, "GET", "/v1/admin/providers", "secret", ""); st != 200 || len(m["providers"].([]any)) != 1 {
		t.Fatalf("%d %v", st, m)
	}

	if st, _ := call(t, srv, "POST", "/v1/admin/apis", "secret", `{"slug":"ifsc","name":"IFSC"}`); st != 400 {
		t.Fatalf("missing provider_id/base_url: %d", st)
	}
	body := `{"provider_id":"prov-1","slug":"ifsc","name":"IFSC","base_url":"https://ifsc.razorpay.com",
		"auth":{"type":"api_key","header":"X-Key","secret":"plain-secret"},"upstream_rate_limit_rps":5}`
	st, m = call(t, srv, "POST", "/v1/admin/apis", "secret", body)
	if st != 201 || m["slug"] != "ifsc" || l.gotAPI.Auth.Secret != "plain-secret" || l.gotAPI.UpstreamRPS != 5 {
		t.Fatalf("%d %v %+v", st, m, l.gotAPI)
	}
	auth := m["auth"].(map[string]any)
	if auth["has_secret"] != true || auth["header"] != "X-Key" {
		t.Fatalf("auth %v", auth)
	}
	if _, ok := auth["secret"]; ok {
		t.Fatal("secret must never be returned")
	}

	// Without auth the service gets the zero AuthInput (it defaults to none).
	call(t, srv, "POST", "/v1/admin/apis", "secret", `{"provider_id":"p","slug":"x1","name":"X","base_url":"https://x.example"}`)
	if l.gotAPI.Auth != (service.AuthInput{}) {
		t.Fatalf("%+v", l.gotAPI.Auth)
	}
}

func TestSecretNeverInAnyAPIResponse(t *testing.T) {
	srv := newListingServer(&fakeListing{})
	defer srv.Close()
	for _, r := range [][3]string{
		{"GET", "/v1/admin/apis", ""}, {"GET", "/v1/admin/apis/ifsc", ""},
		{"PATCH", "/v1/admin/apis/ifsc", `{"name":"x"}`}, {"POST", "/v1/admin/apis/ifsc/publish", ""},
	} {
		st, m := call(t, srv, r[0], r[1], "secret", r[2])
		if st != 200 || strings.Contains(strings.ToLower(jsonString(m)), "sealed") {
			t.Errorf("%s %s: %d %v", r[0], r[1], st, m)
		}
	}
}

func TestUpdateAPIPartial(t *testing.T) {
	l := &fakeListing{}
	srv := newListingServer(l)
	defer srv.Close()
	st, _ := call(t, srv, "PATCH", "/v1/admin/apis/ifsc", "secret", `{"description":"","upstream_rate_limit_rps":0}`)
	u := l.gotUpdate
	if st != 200 || l.gotSlug != "ifsc" || u.Description == nil || *u.Description != "" || u.UpstreamRPS == nil || u.Name != nil || u.Auth != nil {
		t.Fatalf("explicit empty values must be passed, absent ones nil: %d %+v", st, u)
	}
	call(t, srv, "PATCH", "/v1/admin/apis/ifsc", "secret", `{"auth":{"type":"none"}}`)
	if l.gotUpdate.Auth == nil || l.gotUpdate.Auth.Type != "none" {
		t.Fatalf("%+v", l.gotUpdate.Auth)
	}
}

func TestPublishSuspendAndErrors(t *testing.T) {
	srv := newListingServer(&fakeListing{})
	defer srv.Close()
	if st, m := call(t, srv, "POST", "/v1/admin/apis/ifsc/publish", "secret", ""); st != 200 || m["status"] != "published" {
		t.Fatalf("%d %v", st, m)
	}
	if st, m := call(t, srv, "POST", "/v1/admin/apis/ifsc/suspend", "secret", ""); st != 200 || m["status"] != "suspended" {
		t.Fatalf("%d %v", st, m)
	}
	srv2 := newListingServer(&fakeListing{err: domain.Conflict("AGREEMENT_REQUIRED", "no")})
	defer srv2.Close()
	if st, m := call(t, srv2, "POST", "/v1/admin/apis/ifsc/publish", "secret", ""); st != 409 || code(m) != "AGREEMENT_REQUIRED" {
		t.Fatalf("%d %v", st, m)
	}
	srv3 := newListingServer(&fakeListing{err: domain.NotFound("API_NOT_FOUND", "no")})
	defer srv3.Close()
	if st, m := call(t, srv3, "GET", "/v1/admin/apis/nope", "secret", ""); st != 404 || code(m) != "API_NOT_FOUND" {
		t.Fatalf("%d %v", st, m)
	}
}

func TestCreatePlanMoney(t *testing.T) {
	l := &fakeListing{}
	srv := newListingServer(l)
	defer srv.Close()
	st, m := call(t, srv, "POST", "/v1/admin/apis/ifsc/plans", "secret",
		`{"name":"PAYG","kind":"payg","price_per_call":"0.02","provider_cost_per_call":"0.015","rate_limit_rps":2}`)
	if st != 201 || l.gotPlan.PricePerCall != 20_000 || l.gotPlan.ProviderCostPerCall != 15_000 || l.gotPlan.MonthlyFee != 0 {
		t.Fatalf("%d %v %+v", st, m, l.gotPlan)
	}
	if m["price_per_call"] != "0.02" || m["provider_cost_per_call"] != "0.015" || m["monthly_fee"] != "0.00" {
		t.Fatalf("exact decimal strings: %v", m)
	}
	for _, bad := range []string{`"price_per_call":"-1"`, `"price_per_call":"0.0000001"`, `"monthly_fee":"₹499"`} {
		st, m := call(t, srv, "POST", "/v1/admin/apis/ifsc/plans", "secret", `{"name":"X","kind":"payg",`+bad+`}`)
		if st != 400 || code(m) != "INVALID_AMOUNT" {
			t.Errorf("%s: %d %v", bad, st, m)
		}
	}
	if st, m := call(t, srv, "POST", "/v1/admin/apis/ifsc/plans", "secret", `{"name":"X","kind":"payg","price_per_call":0.02}`); st != 400 {
		t.Errorf("a JSON number for money must be rejected: %d %v", st, m)
	}
	if st, m := call(t, srv, "POST", "/v1/admin/plans/plan-1/retire", "secret", ""); st != 200 || m["status"] != "retired" {
		t.Fatalf("%d %v", st, m)
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestEndpointRoutes(t *testing.T) {
	l := &fakeListing{}
	srv := newListingServer(l)
	defer srv.Close()
	body := `{"method":"GET","path":"/{code}","params":[{"name":"code","in":"path","example":"HDFC0CAGSBK"}],
		"headers":[{"name":"Accept-Language","required":false}],"response_status":200,"response_example":"{}"}`
	st, m := call(t, srv, "POST", "/v1/admin/apis/ifsc/endpoints", "secret", body)
	if st != 201 || m["id"] != "ep-1" || l.gotSlug != "ifsc" || l.gotEndpoint.Params[0].Example != "HDFC0CAGSBK" || l.gotEndpoint.Headers[0].Name != "Accept-Language" {
		t.Fatalf("%d %v %+v", st, m, l.gotEndpoint)
	}
	if st, _ := call(t, srv, "POST", "/v1/admin/apis/ifsc/endpoints", "secret", `{"path":"/x"}`); st != 400 {
		t.Fatalf("method required: %d", st)
	}
	if st, m := call(t, srv, "PUT", "/v1/admin/endpoints/ep-9", "secret", `{"method":"GET","path":"/"}`); st != 200 || m["id"] != "ep-9" {
		t.Fatalf("%d %v", st, m)
	}
	if st, _ := call(t, srv, "DELETE", "/v1/admin/endpoints/ep-9", "secret", ""); st != 204 || l.gotSlug != "ep-9" {
		t.Fatalf("%d", st)
	}
	if st, _ := call(t, srv, "DELETE", "/v1/admin/endpoints/ep-9", "", ""); st != 401 {
		t.Fatal("needs admin")
	}
	// upstream_headers round-trip on create
	call(t, srv, "POST", "/v1/admin/apis", "secret", `{"provider_id":"p","slug":"x1","name":"X","base_url":"https://x.example",
		"upstream_headers":[{"name":"X-Client-Id","value":"acme"}]}`)
	if len(l.gotAPI.UpstreamHeaders) != 1 || l.gotAPI.UpstreamHeaders[0].Value != "acme" {
		t.Fatalf("%+v", l.gotAPI.UpstreamHeaders)
	}
}
