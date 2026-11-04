package service

import (
	"context"
	"errors"
	"strings"

	"apimarket/internal/domain"
)

// ListingService lets a platform admin onboard providers, list their APIs,
// price them and take them live. Changes reach the gateway on its next
// catalog refresh (a few seconds).
type ListingService struct {
	Tx        TxManager
	Providers ProviderRepo
	APIs      APIRepo
	Plans     PlanRepo
	Endpoints EndpointRepo
	Audit     AuditRepo
	Secrets   Sealer
	Clock     Clock
}

type CreateProviderInput struct {
	Name            string
	ContactEmail    string
	AgreementSigned bool
}

// AuthInput is how the gateway authenticates to the provider. Secret is the
// plaintext credential; it is sealed before storage and never returned.
type AuthInput struct {
	Type   string
	Header string
	Secret string
}

type CreateAPIInput struct {
	ProviderID  string
	Slug        string
	Name        string
	Description string
	Category    string
	BaseURL     string
	MockURL     string
	Auth        AuthInput
	UpstreamRPS float64
	// UpstreamHeaders are fixed, non-secret headers sent to the provider.
	UpstreamHeaders []domain.Header
}

// UpdateAPIInput changes only the fields that are set. The slug cannot change:
// customers' integrations depend on it. With Auth set and Auth.Secret empty,
// an api_key API keeps its stored secret (e.g. to rename the header).
type UpdateAPIInput struct {
	Name            *string
	Description     *string
	Category        *string
	BaseURL         *string
	MockURL         *string
	Auth            *AuthInput
	UpstreamRPS     *float64
	UpstreamHeaders *[]domain.Header
}

// APIDetail is an API with all its plans and documented endpoints, for admin views.
type APIDetail struct {
	API       domain.API
	Plans     []domain.Plan
	Endpoints []domain.Endpoint
}

// ---------- providers ----------

func (s *ListingService) CreateProvider(ctx context.Context, actor domain.Actor, in CreateProviderInput) (domain.Provider, error) {
	p, err := domain.NewProvider(in.Name, in.ContactEmail, in.AgreementSigned, s.Clock.Now())
	if err != nil {
		return domain.Provider{}, err
	}
	err = s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if p, err = s.Providers.Create(ctx, p); err != nil {
			return err
		}
		return s.Audit.Record(ctx, audit(actor, "", "provider.created", "provider", p.ID,
			map[string]any{"agreement_signed": p.AgreementSignedAt != nil}))
	})
	return p, err
}

// SignAgreement records that the provider signed the resale agreement, which
// publishing requires. Signing again keeps the original date.
func (s *ListingService) SignAgreement(ctx context.Context, actor domain.Actor, providerID string) (domain.Provider, error) {
	var out domain.Provider
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.getProvider(ctx, providerID)
		if err != nil || p.AgreementSignedAt != nil {
			out = p
			return err
		}
		if out, err = s.Providers.SignAgreement(ctx, providerID, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return s.Audit.Record(ctx, audit(actor, "", "provider.agreement_signed", "provider", providerID, nil))
	})
	return out, err
}

func (s *ListingService) ListProviders(ctx context.Context) ([]domain.Provider, error) {
	return s.Providers.List(ctx)
}

// ---------- APIs ----------

// CreateAPI adds an API as a draft; it is not callable until published.
func (s *ListingService) CreateAPI(ctx context.Context, actor domain.Actor, in CreateAPIInput) (APIDetail, error) {
	a := domain.API{ProviderID: in.ProviderID, Slug: in.Slug, Name: in.Name, Description: in.Description,
		Category: in.Category, BaseURL: in.BaseURL, MockURL: in.MockURL, AuthType: in.Auth.Type,
		AuthHeader: in.Auth.Header, UpstreamRPS: in.UpstreamRPS, UpstreamHeaders: in.UpstreamHeaders, Status: domain.APIDraft}
	if a.AuthType == "" {
		a.AuthType = domain.AuthNone
	}
	if err := s.applySecret(&a, in.Auth.Secret); err != nil {
		return APIDetail{}, err
	}
	if err := a.ValidateListing(); err != nil {
		return APIDetail{}, err
	}
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.getProvider(ctx, a.ProviderID); err != nil {
			return err
		}
		created, err := s.APIs.Create(ctx, a)
		if e, ok := domain.AsError(err); ok && e.Kind == domain.KindConflict {
			return domain.Conflict("SLUG_TAKEN", "another API already uses the API path /"+a.Slug)
		}
		if err != nil {
			return err
		}
		a = created
		return s.Audit.Record(ctx, audit(actor, "", "api.created", "api", a.ID,
			map[string]any{"slug": a.Slug, "provider_id": a.ProviderID, "auth_type": a.AuthType}))
	})
	if err != nil {
		return APIDetail{}, err
	}
	return APIDetail{API: a, Plans: []domain.Plan{}, Endpoints: []domain.Endpoint{}}, nil
}

func (s *ListingService) GetAPI(ctx context.Context, slug string) (APIDetail, error) {
	a, err := s.getAPI(ctx, slug)
	if err != nil {
		return APIDetail{}, err
	}
	return s.detail(ctx, a)
}

func (s *ListingService) detail(ctx context.Context, a domain.API) (APIDetail, error) {
	plans, err := s.Plans.ListByAPI(ctx, a.ID)
	if err != nil {
		return APIDetail{}, err
	}
	eps, err := s.Endpoints.ListByAPI(ctx, a.ID)
	return APIDetail{API: a, Plans: plans, Endpoints: eps}, err
}

func (s *ListingService) ListAPIs(ctx context.Context) ([]APIDetail, error) {
	apis, err := s.APIs.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]APIDetail, 0, len(apis))
	for _, a := range apis { // admin-only view; the API count is small
		d, err := s.detail(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *ListingService) UpdateAPI(ctx context.Context, actor domain.Actor, slug string, in UpdateAPIInput) (APIDetail, error) {
	var out APIDetail
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		a, err := s.getAPI(ctx, slug)
		if err != nil {
			return err
		}
		changed := []string{}
		set := func(field string, dst *string, v *string) {
			if v != nil {
				*dst = *v
				changed = append(changed, field)
			}
		}
		set("name", &a.Name, in.Name)
		set("description", &a.Description, in.Description)
		set("category", &a.Category, in.Category)
		set("base_url", &a.BaseURL, in.BaseURL)
		set("mock_url", &a.MockURL, in.MockURL)
		if in.UpstreamRPS != nil {
			a.UpstreamRPS = *in.UpstreamRPS
			changed = append(changed, "upstream_rate_limit_rps")
		}
		if in.UpstreamHeaders != nil {
			a.UpstreamHeaders = *in.UpstreamHeaders
			changed = append(changed, "upstream_headers")
		}
		rotated := false
		if in.Auth != nil {
			keep := in.Auth.Secret == "" && canKeepSecret(a.AuthType, in.Auth.Type) && a.SecretSealed != ""
			a.AuthType, a.AuthHeader = in.Auth.Type, in.Auth.Header
			if !keep {
				a.SecretSealed = ""
				if err := s.applySecret(&a, in.Auth.Secret); err != nil {
					return err
				}
				rotated = in.Auth.Secret != ""
			}
			changed = append(changed, "auth")
		}
		if len(changed) == 0 {
			return domain.Invalid("NOTHING_TO_UPDATE", "no updatable fields were given")
		}
		if err := a.ValidateListing(); err != nil {
			return err
		}
		if err := s.APIs.Update(ctx, a); err != nil {
			return err
		}
		// Field names only: values (and the secret) never go into the audit log.
		if err := s.Audit.Record(ctx, audit(actor, "", "api.updated", "api", a.ID,
			map[string]any{"slug": a.Slug, "fields": changed, "secret_rotated": rotated})); err != nil {
			return err
		}
		out, err = s.detail(ctx, a)
		return err
	})
	return out, err
}

// canKeepSecret: an empty secret on update keeps the stored one when the new
// auth type also uses a secret of the same shape (a key can move from a
// header to a query parameter; a basic user:password cannot become a token).
func canKeepSecret(from, to string) bool {
	keyLike := map[string]bool{domain.AuthAPIKey: true, domain.AuthBearer: true, domain.AuthQuery: true}
	return (keyLike[from] && keyLike[to]) || (from == domain.AuthBasic && to == domain.AuthBasic)
}

// Publish makes the API callable and visible in the public catalog.
// Publishing an already published API is a no-op.
func (s *ListingService) Publish(ctx context.Context, actor domain.Actor, slug string) (APIDetail, error) {
	return s.setStatus(ctx, actor, slug, domain.APIPublished, func(ctx context.Context, a domain.API, plans []domain.Plan) error {
		p, err := s.getProvider(ctx, a.ProviderID)
		if err != nil {
			return err
		}
		return domain.CheckPublish(a, p, countActive(plans))
	})
}

// Suspend takes the API off the gateway and the public catalog. Calls get
// API_NOT_FOUND; subscriptions are kept, so publishing again restores access.
func (s *ListingService) Suspend(ctx context.Context, actor domain.Actor, slug string) (APIDetail, error) {
	return s.setStatus(ctx, actor, slug, domain.APISuspended, func(_ context.Context, a domain.API, _ []domain.Plan) error {
		if a.Status != domain.APIPublished {
			return domain.Conflict("NOT_PUBLISHED", "only a published API can be suspended")
		}
		return nil
	})
}

func (s *ListingService) setStatus(ctx context.Context, actor domain.Actor, slug, status string,
	check func(context.Context, domain.API, []domain.Plan) error) (APIDetail, error) {
	var out APIDetail
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		a, err := s.getAPI(ctx, slug)
		if err != nil {
			return err
		}
		if out, err = s.detail(ctx, a); err != nil {
			return err
		}
		plans := out.Plans
		if a.Status == status {
			return nil
		}
		if err := check(ctx, a, plans); err != nil {
			return err
		}
		if err := s.APIs.SetStatus(ctx, a.ID, status); err != nil {
			return err
		}
		out.API.Status = status
		return s.Audit.Record(ctx, audit(actor, "", "api."+status, "api", a.ID,
			map[string]any{"slug": a.Slug, "from": a.Status}))
	})
	return out, err
}

// ---------- plans ----------

func (s *ListingService) CreatePlan(ctx context.Context, actor domain.Actor, slug string, in domain.PlanInput) (domain.Plan, error) {
	var out domain.Plan
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		a, err := s.getAPI(ctx, slug)
		if err != nil {
			return err
		}
		if a.Status == domain.APIRetired {
			return domain.Conflict("API_RETIRED", "cannot add plans to a retired API")
		}
		p, err := domain.NewPlan(a.ID, in)
		if err != nil {
			return err
		}
		if out, err = s.Plans.Create(ctx, p); err != nil {
			return err
		}
		return s.Audit.Record(ctx, audit(actor, "", "plan.created", "plan", out.ID,
			map[string]any{"api": a.Slug, "kind": out.Kind, "price_per_call_micro": int64(out.PricePerCall),
				"monthly_fee_micro": int64(out.MonthlyFee)}))
	})
	return out, err
}

// RetirePlan stops new subscriptions to a plan. Existing subscribers keep it
// until they switch plans. The last active plan of a published API cannot be
// retired: suspend the API first.
func (s *ListingService) RetirePlan(ctx context.Context, actor domain.Actor, planID string) (domain.Plan, error) {
	var out domain.Plan
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.Plans.Get(ctx, planID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.NotFound("PLAN_NOT_FOUND", "plan not found")
		}
		if err != nil {
			return err
		}
		out = p
		if p.Status == "retired" {
			return nil
		}
		siblings, err := s.Plans.ListByAPI(ctx, p.APIID)
		if err != nil {
			return err
		}
		a, err := s.APIs.GetByID(ctx, p.APIID)
		if err != nil {
			return err
		}
		if a.Status == domain.APIPublished && countActive(siblings) <= 1 {
			return domain.Conflict("LAST_ACTIVE_PLAN", "a published API needs at least one active plan; suspend the API first")
		}
		if err := s.Plans.SetStatus(ctx, p.ID, "retired"); err != nil {
			return err
		}
		out.Status = "retired"
		return s.Audit.Record(ctx, audit(actor, "", "plan.retired", "plan", p.ID, map[string]any{"api_id": p.APIID}))
	})
	return out, err
}

// ---------- helpers ----------

func (s *ListingService) applySecret(a *domain.API, secret string) error {
	if secret == "" {
		return nil
	}
	switch a.AuthType {
	case domain.AuthAPIKey, domain.AuthBearer, domain.AuthQuery:
	case domain.AuthBasic:
		if err := domain.ValidateBasicSecret(secret); err != nil {
			return err
		}
	default:
		return domain.Invalid("AUTH_UNUSED", "a secret is only used with api_key, bearer, basic or query auth")
	}
	if len(secret) > 4096 || strings.ContainsAny(secret, "\r\n\x00") {
		return domain.Invalid("INVALID_SECRET", "secret is too long or contains line breaks")
	}
	sealed, err := s.Secrets.Seal(secret)
	if err != nil {
		return err
	}
	a.SecretSealed = sealed
	return nil
}

func (s *ListingService) getAPI(ctx context.Context, slug string) (domain.API, error) {
	a, err := s.APIs.GetBySlug(ctx, slug)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.API{}, domain.NotFound("API_NOT_FOUND", "no API at path /"+slug)
	}
	return a, err
}

func (s *ListingService) getProvider(ctx context.Context, id string) (domain.Provider, error) {
	p, err := s.Providers.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Provider{}, domain.NotFound("PROVIDER_NOT_FOUND", "provider not found")
	}
	return p, err
}

func countActive(plans []domain.Plan) int {
	n := 0
	for _, p := range plans {
		if p.Status == "active" {
			n++
		}
	}
	return n
}

// ---------- endpoints (documentation) ----------

func (s *ListingService) CreateEndpoint(ctx context.Context, actor domain.Actor, slug string, e domain.Endpoint) (domain.Endpoint, error) {
	if err := e.Normalize(); err != nil {
		return domain.Endpoint{}, err
	}
	var out domain.Endpoint
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		a, err := s.getAPI(ctx, slug)
		if err != nil {
			return err
		}
		e.APIID = a.ID
		out, err = s.Endpoints.Create(ctx, e)
		if err != nil {
			return endpointConflict(err, e)
		}
		return s.Audit.Record(ctx, audit(actor, "", "endpoint.created", "endpoint", out.ID,
			map[string]any{"api": a.Slug, "method": e.Method, "path": e.Path}))
	})
	return out, err
}

// UpdateEndpoint replaces an endpoint's documentation.
func (s *ListingService) UpdateEndpoint(ctx context.Context, actor domain.Actor, id string, e domain.Endpoint) (domain.Endpoint, error) {
	if err := e.Normalize(); err != nil {
		return domain.Endpoint{}, err
	}
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		old, err := s.getEndpoint(ctx, id)
		if err != nil {
			return err
		}
		e.ID, e.APIID = old.ID, old.APIID
		if err := s.Endpoints.Update(ctx, e); err != nil {
			return endpointConflict(err, e)
		}
		return s.Audit.Record(ctx, audit(actor, "", "endpoint.updated", "endpoint", id,
			map[string]any{"method": e.Method, "path": e.Path}))
	})
	return e, err
}

func (s *ListingService) DeleteEndpoint(ctx context.Context, actor domain.Actor, id string) error {
	return s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		old, err := s.getEndpoint(ctx, id)
		if err != nil {
			return err
		}
		if err := s.Endpoints.Delete(ctx, id); err != nil {
			return err
		}
		return s.Audit.Record(ctx, audit(actor, "", "endpoint.deleted", "endpoint", id,
			map[string]any{"method": old.Method, "path": old.Path}))
	})
}

func (s *ListingService) getEndpoint(ctx context.Context, id string) (domain.Endpoint, error) {
	e, err := s.Endpoints.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Endpoint{}, domain.NotFound("ENDPOINT_NOT_FOUND", "endpoint not found")
	}
	return e, err
}

func endpointConflict(err error, e domain.Endpoint) error {
	if de, ok := domain.AsError(err); ok && de.Kind == domain.KindConflict {
		return domain.Conflict("ENDPOINT_EXISTS", e.Method+" "+e.Path+" is already documented for this API")
	}
	return err
}
