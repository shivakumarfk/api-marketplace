package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
	"apimarket/internal/service"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeServices implements every handler port; err is returned when set.
type fakeServices struct {
	err      error
	gotActor domain.Actor
	gotOrgIn service.CreateOrgInput
	gotTopUp []any
}

func (f *fakeServices) Create(_ context.Context, a domain.Actor, in service.CreateOrgInput) (domain.Org, error) {
	f.gotActor, f.gotOrgIn = a, in
	return domain.Org{ID: "org-1", Name: in.Name, Type: in.Type, BillingMode: domain.Prepaid}, f.err
}

type fakeKeys struct{ err error }

func (f fakeKeys) Create(_ context.Context, _ domain.Actor, orgID, env, name string) (service.CreatedKey, error) {
	return service.CreatedKey{Key: domain.APIKey{ID: "key-1", OrgID: orgID, Env: domain.KeyEnv(env), Prefix: "mk_live_abcd"}, Plain: "mk_live_secret"}, f.err
}
func (f fakeKeys) Revoke(context.Context, domain.Actor, string) error { return f.err }

func (f *fakeServices) Subscribe(_ context.Context, _ domain.Actor, orgID, slug, plan string) (domain.Subscription, error) {
	return domain.Subscription{ID: "sub-1", OrgID: orgID, PlanID: plan}, f.err
}
func (f *fakeServices) TopUp(_ context.Context, _ domain.Actor, org string, rupees int64, ref string) (string, money.Micro, error) {
	f.gotTopUp = []any{org, rupees, ref}
	return "txn-1", money.FromRupees(rupees), f.err
}
func (f *fakeServices) Balance(context.Context, string) (money.Micro, error) {
	return money.FromRupees(99) + 950_000, f.err
}
func (f *fakeServices) ListPublished(context.Context) ([]service.CatalogEntry, error) {
	return []service.CatalogEntry{{API: domain.API{Slug: "weather", Name: "Weather"},
		Plans: []domain.Plan{{ID: "p1", Kind: domain.PlanPAYG, PricePerCall: 50_000}}}}, f.err
}

func newServer(f *fakeServices, keys fakeKeys) *httptest.Server {
	return httptest.NewServer(Router(Deps{Orgs: f, Keys: keys, Subs: f, Wallets: f, Catalog: f,
		AdminToken: "secret", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}))
}

func call(t *testing.T, srv *httptest.Server, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func code(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func TestAdminAuth(t *testing.T) {
	srv := newServer(&fakeServices{}, fakeKeys{})
	defer srv.Close()
	for _, tok := range []string{"", "wrong"} {
		if st, m := call(t, srv, "POST", "/v1/admin/orgs", tok, `{}`); st != 401 || code(m) != "UNAUTHORIZED" {
			t.Errorf("token %q: %d %s", tok, st, code(m))
		}
	}
	if st, _ := call(t, srv, "GET", "/v1/catalog/apis", "", ""); st != 200 {
		t.Errorf("catalog must be public: %d", st)
	}
}

func TestCreateOrgValidationAndActor(t *testing.T) {
	f := &fakeServices{}
	srv := newServer(f, fakeKeys{})
	defer srv.Close()
	if st, m := call(t, srv, "POST", "/v1/admin/orgs", "secret", `{"name":"X","type":"partnership"}`); st != 400 || code(m) != "BAD_REQUEST" {
		t.Fatalf("bad type: %d %v", st, m)
	}
	if st, _ := call(t, srv, "POST", "/v1/admin/orgs", "secret", `not json`); st != 400 {
		t.Fatalf("bad json: %d", st)
	}
	st, m := call(t, srv, "POST", "/v1/admin/orgs", "secret", `{"name":"Asha","type":"individual"}`)
	if st != 201 || m["id"] != "org-1" || m["billing_mode"] != "prepaid" {
		t.Fatalf("%d %v", st, m)
	}
	if f.gotActor.Type != "platform_admin" || f.gotOrgIn.Type != domain.OrgIndividual {
		t.Fatalf("actor %+v input %+v", f.gotActor, f.gotOrgIn)
	}
}

func TestDomainErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{domain.Invalid("INVALID_GSTIN", "bad"), 400, "INVALID_GSTIN"},
		{domain.NotFound("ORG_NOT_FOUND", "no"), 404, "ORG_NOT_FOUND"},
		{domain.Conflict("DUPLICATE_POSTING", "dup"), 409, "DUPLICATE_POSTING"},
		{domain.Forbidden("ORG_INACTIVE", "no"), 403, "ORG_INACTIVE"},
		{errors.New("connection refused to 10.0.0.5"), 500, "INTERNAL"},
	}
	for _, c := range cases {
		srv := newServer(&fakeServices{err: c.err}, fakeKeys{})
		st, m := call(t, srv, "POST", "/v1/admin/orgs", "secret", `{"name":"A","type":"individual"}`)
		srv.Close()
		if st != c.status || code(m) != c.code {
			t.Errorf("%v: got %d %s", c.err, st, code(m))
		}
		if c.status == 500 && strings.Contains(m["error"].(map[string]any)["message"].(string), "10.0.0.5") {
			t.Error("internal error details must not leak to clients")
		}
	}
}

func TestKeyTopUpBalanceEndpoints(t *testing.T) {
	f := &fakeServices{}
	srv := newServer(f, fakeKeys{})
	defer srv.Close()

	if st, m := call(t, srv, "POST", "/v1/admin/orgs/o1/keys", "secret", `{"env":"staging"}`); st != 400 {
		t.Fatalf("bad env: %d %v", st, m)
	}
	st, m := call(t, srv, "POST", "/v1/admin/orgs/o1/keys", "secret", `{"env":"production","name":"ci"}`)
	if st != 201 || m["key"] != "mk_live_secret" || m["prefix"] != "mk_live_abcd" {
		t.Fatalf("%d %v", st, m)
	}
	if st, _ := call(t, srv, "POST", "/v1/admin/orgs/o1/topups", "secret", `{"amount_rupees":10}`); st != 400 {
		t.Fatalf("payment_ref is required: %d", st)
	}
	st, m = call(t, srv, "POST", "/v1/admin/orgs/o1/topups", "secret", `{"amount_rupees":10,"payment_ref":"pay_1"}`)
	if st != 201 || m["balance"] != "₹10.00" || f.gotTopUp[2] != "pay_1" {
		t.Fatalf("%d %v %v", st, m, f.gotTopUp)
	}
	st, m = call(t, srv, "GET", "/v1/admin/orgs/o1/balance", "secret", "")
	if st != 200 || m["balance"] != "₹99.95" {
		t.Fatalf("%d %v", st, m)
	}
	if st, m := call(t, srv, "POST", "/v1/admin/keys/k1/revoke", "secret", ""); st != 200 || m["status"] != "revoked" {
		t.Fatalf("%d %v", st, m)
	}
	srv2 := newServer(f, fakeKeys{err: domain.NotFound("KEY_NOT_FOUND", "x")})
	defer srv2.Close()
	if st, m := call(t, srv2, "POST", "/v1/admin/keys/k1/revoke", "secret", ""); st != 404 || code(m) != "KEY_NOT_FOUND" {
		t.Fatalf("%d %v", st, m)
	}
}

func TestCatalogShape(t *testing.T) {
	srv := newServer(&fakeServices{}, fakeKeys{})
	defer srv.Close()
	_, m := call(t, srv, "GET", "/v1/catalog/apis", "", "")
	apis := m["apis"].([]any)
	plan := apis[0].(map[string]any)["plans"].([]any)[0].(map[string]any)
	if plan["price_per_call"] != "₹0.0500" || plan["kind"] != "payg" {
		t.Fatalf("%v", plan)
	}
	if st, m := call(t, srv, "GET", "/nope", "", ""); st != 404 || code(m) != "NOT_FOUND" {
		t.Fatalf("%d %v", st, m)
	}
}
