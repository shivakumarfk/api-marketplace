package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"apimarket/internal/domain"
)

// In-memory listing repos. They share mem.seq for IDs; their state lives in
// listingMem so the original fakes stay unchanged.
type listingMem struct {
	*mem
	providers map[string]domain.Provider
	apisByID  map[string]domain.API
	plansByID map[string]domain.Plan
	endpoints map[string]domain.Endpoint
	failSeal  bool
}

func newListingMem(m *mem) *listingMem {
	return &listingMem{mem: m, providers: map[string]domain.Provider{}, apisByID: map[string]domain.API{},
		plansByID: map[string]domain.Plan{}, endpoints: map[string]domain.Endpoint{}}
}

type memProviders struct{ *listingMem }

func (r memProviders) Create(_ context.Context, p domain.Provider) (domain.Provider, error) {
	p.ID = r.id("prov")
	r.providers[p.ID] = p
	return p, nil
}
func (r memProviders) Get(_ context.Context, id string) (domain.Provider, error) {
	p, ok := r.providers[id]
	if !ok {
		return domain.Provider{}, domain.ErrNotFound
	}
	return p, nil
}
func (r memProviders) List(context.Context) ([]domain.Provider, error) {
	var out []domain.Provider
	for _, p := range r.providers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r memProviders) SignAgreement(_ context.Context, id string, at time.Time) (domain.Provider, error) {
	p, ok := r.providers[id]
	if !ok {
		return domain.Provider{}, domain.ErrNotFound
	}
	if p.AgreementSignedAt == nil {
		p.AgreementSignedAt = &at
		r.providers[id] = p
	}
	return p, nil
}

type memAPIs struct{ *listingMem }

func (r memAPIs) Create(_ context.Context, a domain.API) (domain.API, error) {
	for _, x := range r.apisByID {
		if x.Slug == a.Slug {
			return domain.API{}, domain.Conflict("ALREADY_EXISTS", "already exists")
		}
	}
	a.ID = r.id("api")
	r.apisByID[a.ID] = a
	return a, nil
}
func (r memAPIs) GetBySlug(_ context.Context, slug string) (domain.API, error) {
	for _, a := range r.apisByID {
		if a.Slug == slug {
			return a, nil
		}
	}
	return domain.API{}, domain.ErrNotFound
}
func (r memAPIs) GetByID(_ context.Context, id string) (domain.API, error) {
	a, ok := r.apisByID[id]
	if !ok {
		return domain.API{}, domain.ErrNotFound
	}
	return a, nil
}
func (r memAPIs) List(context.Context) ([]domain.API, error) {
	var out []domain.API
	for _, a := range r.apisByID {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}
func (r memAPIs) Update(_ context.Context, a domain.API) error {
	old, ok := r.apisByID[a.ID]
	if !ok {
		return domain.ErrNotFound
	}
	a.Status = old.Status // Update never changes status
	r.apisByID[a.ID] = a
	return nil
}
func (r memAPIs) SetStatus(_ context.Context, id, status string) error {
	a, ok := r.apisByID[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.Status = status
	r.apisByID[id] = a
	return nil
}

type memPlans struct{ *listingMem }

func (r memPlans) Create(_ context.Context, p domain.Plan) (domain.Plan, error) {
	p.ID = r.id("plan")
	r.plansByID[p.ID] = p
	return p, nil
}
func (r memPlans) Get(_ context.Context, id string) (domain.Plan, error) {
	p, ok := r.plansByID[id]
	if !ok {
		return domain.Plan{}, domain.ErrNotFound
	}
	return p, nil
}
func (r memPlans) ListByAPI(_ context.Context, apiID string) ([]domain.Plan, error) {
	out := []domain.Plan{}
	for _, p := range r.plansByID {
		if p.APIID == apiID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (r memPlans) SetStatus(_ context.Context, id, status string) error {
	p, ok := r.plansByID[id]
	if !ok {
		return domain.ErrNotFound
	}
	p.Status = status
	r.plansByID[id] = p
	return nil
}

// fakeSealer "encrypts" by reversing and tagging, so tests can check the
// plaintext never ends up stored as-is.
type fakeSealer struct{ *listingMem }

func (s fakeSealer) Seal(pt string) (string, error) {
	if s.failSeal {
		return "", errors.New("kms down")
	}
	b := []rune(pt)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return "sealed:" + string(b), nil
}

func unseal(s string) string {
	b := []rune(strings.TrimPrefix(s, "sealed:"))
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

type memEndpoints struct{ *listingMem }

func (r memEndpoints) Create(_ context.Context, e domain.Endpoint) (domain.Endpoint, error) {
	for _, x := range r.endpoints {
		if x.APIID == e.APIID && x.Method == e.Method && x.Path == e.Path {
			return domain.Endpoint{}, domain.Conflict("ALREADY_EXISTS", "already exists")
		}
	}
	e.ID = r.id("ep")
	r.endpoints[e.ID] = e
	return e, nil
}
func (r memEndpoints) Get(_ context.Context, id string) (domain.Endpoint, error) {
	e, ok := r.endpoints[id]
	if !ok {
		return domain.Endpoint{}, domain.ErrNotFound
	}
	return e, nil
}
func (r memEndpoints) ListByAPI(_ context.Context, apiID string) ([]domain.Endpoint, error) {
	out := []domain.Endpoint{}
	for _, e := range r.endpoints {
		if e.APIID == apiID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path+out[i].Method < out[j].Path+out[j].Method })
	return out, nil
}
func (r memEndpoints) Update(_ context.Context, e domain.Endpoint) error {
	if _, ok := r.endpoints[e.ID]; !ok {
		return domain.ErrNotFound
	}
	for _, x := range r.endpoints {
		if x.ID != e.ID && x.APIID == e.APIID && x.Method == e.Method && x.Path == e.Path {
			return domain.Conflict("ALREADY_EXISTS", "already exists")
		}
	}
	r.endpoints[e.ID] = e
	return nil
}
func (r memEndpoints) Delete(_ context.Context, id string) error {
	if _, ok := r.endpoints[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.endpoints, id)
	return nil
}
