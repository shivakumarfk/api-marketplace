package controlplane

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
	"apimarket/internal/service"
)

// Listing is the catalog-administration port (implemented by service.ListingService).
type Listing interface {
	CreateProvider(ctx context.Context, actor domain.Actor, in service.CreateProviderInput) (domain.Provider, error)
	ListProviders(ctx context.Context) ([]domain.Provider, error)
	SignAgreement(ctx context.Context, actor domain.Actor, providerID string) (domain.Provider, error)
	CreateAPI(ctx context.Context, actor domain.Actor, in service.CreateAPIInput) (service.APIDetail, error)
	GetAPI(ctx context.Context, slug string) (service.APIDetail, error)
	ListAPIs(ctx context.Context) ([]service.APIDetail, error)
	UpdateAPI(ctx context.Context, actor domain.Actor, slug string, in service.UpdateAPIInput) (service.APIDetail, error)
	Publish(ctx context.Context, actor domain.Actor, slug string) (service.APIDetail, error)
	Suspend(ctx context.Context, actor domain.Actor, slug string) (service.APIDetail, error)
	CreatePlan(ctx context.Context, actor domain.Actor, slug string, in domain.PlanInput) (domain.Plan, error)
	RetirePlan(ctx context.Context, actor domain.Actor, planID string) (domain.Plan, error)
	CreateEndpoint(ctx context.Context, actor domain.Actor, slug string, e domain.Endpoint) (domain.Endpoint, error)
	UpdateEndpoint(ctx context.Context, actor domain.Actor, id string, e domain.Endpoint) (domain.Endpoint, error)
	DeleteEndpoint(ctx context.Context, actor domain.Actor, id string) error
}

func registerListing(admin *gin.RouterGroup, h handlers) {
	admin.POST("/providers", h.createProvider)
	admin.GET("/providers", h.listProviders)
	admin.POST("/providers/:provider/agreement", h.signAgreement)
	admin.POST("/apis", h.createAPI)
	admin.GET("/apis", h.listAPIs)
	admin.GET("/apis/:api", h.getAPI)
	admin.PATCH("/apis/:api", h.updateAPI)
	admin.POST("/apis/:api/publish", h.publishAPI)
	admin.POST("/apis/:api/suspend", h.suspendAPI)
	admin.POST("/apis/:api/plans", h.createPlan)
	admin.POST("/plans/:plan/retire", h.retirePlan)
	admin.POST("/apis/:api/endpoints", h.createEndpoint)
	admin.PUT("/endpoints/:endpoint", h.updateEndpoint)
	admin.DELETE("/endpoints/:endpoint", h.deleteEndpoint)
}

// ---------- DTOs ----------

type createProviderReq struct {
	Name            string `json:"name" binding:"required"`
	ContactEmail    string `json:"contact_email"`
	AgreementSigned bool   `json:"agreement_signed"`
}

type providerResp struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	ContactEmail      string     `json:"contact_email,omitempty"`
	AgreementSignedAt *time.Time `json:"agreement_signed_at"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
}

// authReq: header is the header name for api_key, the query parameter name
// for query, and empty for none/bearer/basic.
type authReq struct {
	Type   string `json:"type" binding:"required"`
	Header string `json:"header"`
	Secret string `json:"secret"` // write-only
}

func (a *authReq) input() service.AuthInput {
	return service.AuthInput{Type: a.Type, Header: a.Header, Secret: a.Secret}
}

type createAPIReq struct {
	ProviderID      string          `json:"provider_id" binding:"required"`
	Slug            string          `json:"slug" binding:"required"`
	Name            string          `json:"name" binding:"required"`
	Description     string          `json:"description"`
	Category        string          `json:"category"`
	BaseURL         string          `json:"base_url" binding:"required"`
	MockURL         string          `json:"mock_url"`
	Auth            *authReq        `json:"auth"` // omitted = none
	UpstreamRPS     float64         `json:"upstream_rate_limit_rps"`
	UpstreamHeaders []domain.Header `json:"upstream_headers"`
}

type updateAPIReq struct {
	Name            *string          `json:"name"`
	Description     *string          `json:"description"`
	Category        *string          `json:"category"`
	BaseURL         *string          `json:"base_url"`
	MockURL         *string          `json:"mock_url"`
	Auth            *authReq         `json:"auth"`
	UpstreamRPS     *float64         `json:"upstream_rate_limit_rps"`
	UpstreamHeaders *[]domain.Header `json:"upstream_headers"`
}

// Money is sent and returned as exact rupee strings, e.g. "0.02".
type createPlanReq struct {
	Name                string  `json:"name" binding:"required"`
	Kind                string  `json:"kind" binding:"required"`
	MonthlyFee          string  `json:"monthly_fee"`
	IncludedCalls       int64   `json:"included_calls"`
	PricePerCall        string  `json:"price_per_call"`
	ProviderCostPerCall string  `json:"provider_cost_per_call"`
	RateLimitRPS        float64 `json:"rate_limit_rps"`
}

type authResp struct {
	Type      string `json:"type"`
	Header    string `json:"header,omitempty"`
	HasSecret bool   `json:"has_secret"` // the secret itself is never returned
}

type adminPlanResp struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Kind                string  `json:"kind"`
	Status              string  `json:"status"`
	MonthlyFee          string  `json:"monthly_fee"`
	IncludedCalls       int64   `json:"included_calls"`
	PricePerCall        string  `json:"price_per_call"`
	ProviderCostPerCall string  `json:"provider_cost_per_call"`
	RateLimitRPS        float64 `json:"rate_limit_rps"`
}

type adminAPIResp struct {
	ID              string          `json:"id"`
	ProviderID      string          `json:"provider_id"`
	Slug            string          `json:"slug"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Category        string          `json:"category"`
	Status          string          `json:"status"`
	BaseURL         string          `json:"base_url"`
	MockURL         string          `json:"mock_url,omitempty"`
	Auth            authResp        `json:"auth"`
	UpstreamRPS     float64         `json:"upstream_rate_limit_rps"`
	UpstreamHeaders []domain.Header `json:"upstream_headers"`
	Plans           []adminPlanResp `json:"plans"`
	Endpoints       []endpointResp  `json:"endpoints"`
}

// endpointReq / endpointResp: the documented contract of one operation. The
// same shape is used publicly (it holds no internal fields).
type endpointReq struct {
	Method              string             `json:"method" binding:"required"`
	Path                string             `json:"path" binding:"required"`
	Summary             string             `json:"summary"`
	Description         string             `json:"description"`
	Params              []domain.Param     `json:"params"`
	Headers             []domain.DocHeader `json:"headers"`
	RequestExample      string             `json:"request_example"`
	ResponseStatus      int                `json:"response_status"`
	ResponseContentType string             `json:"response_content_type"`
	ResponseExample     string             `json:"response_example"`
}

func (r endpointReq) domain() domain.Endpoint {
	return domain.Endpoint{Method: r.Method, Path: r.Path, Summary: r.Summary, Description: r.Description,
		Params: r.Params, Headers: r.Headers, RequestExample: r.RequestExample, ResponseStatus: r.ResponseStatus,
		ResponseContentType: r.ResponseContentType, ResponseExample: r.ResponseExample}
}

type endpointResp struct {
	ID string `json:"id"`
	endpointReq
	Idempotent bool `json:"idempotent"` // the gateway retries these on provider errors
}

func toEndpointResp(e domain.Endpoint) endpointResp {
	params, headers := e.Params, e.Headers
	if params == nil {
		params = []domain.Param{}
	}
	if headers == nil {
		headers = []domain.DocHeader{}
	}
	return endpointResp{ID: e.ID, Idempotent: e.Idempotent(), endpointReq: endpointReq{Method: e.Method, Path: e.Path,
		Summary: e.Summary, Description: e.Description, Params: params, Headers: headers,
		RequestExample: e.RequestExample, ResponseStatus: e.ResponseStatus,
		ResponseContentType: e.ResponseContentType, ResponseExample: e.ResponseExample}}
}

func toEndpointResps(es []domain.Endpoint) []endpointResp {
	out := make([]endpointResp, 0, len(es))
	for _, e := range es {
		out = append(out, toEndpointResp(e))
	}
	return out
}

func toProviderResp(p domain.Provider) providerResp {
	return providerResp{ID: p.ID, Name: p.Name, ContactEmail: p.ContactEmail, AgreementSignedAt: p.AgreementSignedAt,
		Status: p.Status, CreatedAt: p.CreatedAt}
}

func toPlanResp(p domain.Plan) adminPlanResp {
	return adminPlanResp{ID: p.ID, Name: p.Name, Kind: string(p.Kind), Status: p.Status,
		MonthlyFee: p.MonthlyFee.Decimal(), IncludedCalls: p.IncludedCalls, PricePerCall: p.PricePerCall.Decimal(),
		ProviderCostPerCall: p.ProviderCostPerCall.Decimal(), RateLimitRPS: p.RateLimitRPS}
}

func toAPIResp(d service.APIDetail) adminAPIResp {
	a := d.API
	out := adminAPIResp{ID: a.ID, ProviderID: a.ProviderID, Slug: a.Slug, Name: a.Name, Description: a.Description,
		Category: a.Category, Status: a.Status, BaseURL: a.BaseURL, MockURL: a.MockURL,
		Auth:        authResp{Type: a.AuthType, Header: a.AuthHeader, HasSecret: a.SecretSealed != ""},
		UpstreamRPS: a.UpstreamRPS, UpstreamHeaders: a.UpstreamHeaders, Plans: make([]adminPlanResp, 0, len(d.Plans)),
		Endpoints: toEndpointResps(d.Endpoints)}
	if out.UpstreamHeaders == nil {
		out.UpstreamHeaders = []domain.Header{}
	}
	for _, p := range d.Plans {
		out.Plans = append(out.Plans, toPlanResp(p))
	}
	return out
}

// ---------- handlers ----------

func (h handlers) createProvider(c *gin.Context) {
	var req createProviderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	p, err := h.d.Listing.CreateProvider(c.Request.Context(), actor(c),
		service.CreateProviderInput{Name: req.Name, ContactEmail: req.ContactEmail, AgreementSigned: req.AgreementSigned})
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, toProviderResp(p))
}

func (h handlers) listProviders(c *gin.Context) {
	ps, err := h.d.Listing.ListProviders(c.Request.Context())
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	out := make([]providerResp, 0, len(ps))
	for _, p := range ps {
		out = append(out, toProviderResp(p))
	}
	c.JSON(http.StatusOK, gin.H{"providers": out})
}

func (h handlers) signAgreement(c *gin.Context) {
	p, err := h.d.Listing.SignAgreement(c.Request.Context(), actor(c), c.Param("provider"))
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusOK, toProviderResp(p))
}

func (h handlers) createAPI(c *gin.Context) {
	var req createAPIReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	in := service.CreateAPIInput{ProviderID: req.ProviderID, Slug: req.Slug, Name: req.Name, Description: req.Description,
		Category: req.Category, BaseURL: req.BaseURL, MockURL: req.MockURL, UpstreamRPS: req.UpstreamRPS,
		UpstreamHeaders: req.UpstreamHeaders}
	if req.Auth != nil {
		in.Auth = req.Auth.input()
	}
	d, err := h.d.Listing.CreateAPI(c.Request.Context(), actor(c), in)
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, toAPIResp(d))
}

func (h handlers) listAPIs(c *gin.Context) {
	ds, err := h.d.Listing.ListAPIs(c.Request.Context())
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	out := make([]adminAPIResp, 0, len(ds))
	for _, d := range ds {
		out = append(out, toAPIResp(d))
	}
	c.JSON(http.StatusOK, gin.H{"apis": out})
}

func (h handlers) getAPI(c *gin.Context) {
	h.respondAPI(c, http.StatusOK)(h.d.Listing.GetAPI(c.Request.Context(), c.Param("api")))
}

func (h handlers) updateAPI(c *gin.Context) {
	var req updateAPIReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	in := service.UpdateAPIInput{Name: req.Name, Description: req.Description, Category: req.Category,
		BaseURL: req.BaseURL, MockURL: req.MockURL, UpstreamRPS: req.UpstreamRPS, UpstreamHeaders: req.UpstreamHeaders}
	if req.Auth != nil {
		a := req.Auth.input()
		in.Auth = &a
	}
	h.respondAPI(c, http.StatusOK)(h.d.Listing.UpdateAPI(c.Request.Context(), actor(c), c.Param("api"), in))
}

func (h handlers) publishAPI(c *gin.Context) {
	h.respondAPI(c, http.StatusOK)(h.d.Listing.Publish(c.Request.Context(), actor(c), c.Param("api")))
}

func (h handlers) suspendAPI(c *gin.Context) {
	h.respondAPI(c, http.StatusOK)(h.d.Listing.Suspend(c.Request.Context(), actor(c), c.Param("api")))
}

func (h handlers) respondAPI(c *gin.Context, status int) func(service.APIDetail, error) {
	return func(d service.APIDetail, err error) {
		if err != nil {
			fail(c, h.d.Log, err)
			return
		}
		c.JSON(status, toAPIResp(d))
	}
}

func (h handlers) createPlan(c *gin.Context) {
	var req createPlanReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	in := domain.PlanInput{Name: req.Name, Kind: domain.PlanKind(req.Kind), IncludedCalls: req.IncludedCalls, RateLimitRPS: req.RateLimitRPS}
	for _, f := range []struct {
		field string
		raw   string
		dst   *money.Micro
	}{
		{"monthly_fee", req.MonthlyFee, &in.MonthlyFee},
		{"price_per_call", req.PricePerCall, &in.PricePerCall},
		{"provider_cost_per_call", req.ProviderCostPerCall, &in.ProviderCostPerCall},
	} {
		v, err := money.ParseRupees(f.raw)
		if err != nil {
			fail(c, h.d.Log, domain.Invalid("INVALID_AMOUNT", f.field+": "+err.Error()))
			return
		}
		*f.dst = v
	}
	p, err := h.d.Listing.CreatePlan(c.Request.Context(), actor(c), c.Param("api"), in)
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, toPlanResp(p))
}

func (h handlers) retirePlan(c *gin.Context) {
	p, err := h.d.Listing.RetirePlan(c.Request.Context(), actor(c), c.Param("plan"))
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusOK, toPlanResp(p))
}

func (h handlers) createEndpoint(c *gin.Context) {
	var req endpointReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	e, err := h.d.Listing.CreateEndpoint(c.Request.Context(), actor(c), c.Param("api"), req.domain())
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, toEndpointResp(e))
}

func (h handlers) updateEndpoint(c *gin.Context) {
	var req endpointReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	e, err := h.d.Listing.UpdateEndpoint(c.Request.Context(), actor(c), c.Param("endpoint"), req.domain())
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusOK, toEndpointResp(e))
}

func (h handlers) deleteEndpoint(c *gin.Context) {
	if err := h.d.Listing.DeleteEndpoint(c.Request.Context(), actor(c), c.Param("endpoint")); err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.Status(http.StatusNoContent)
}
