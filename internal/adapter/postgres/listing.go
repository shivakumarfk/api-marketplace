package postgres

import (
	"context"
	"time"

	"apimarket/internal/domain"
	"apimarket/internal/service"
)

var (
	_ service.ProviderRepo = ProviderRepo{}
	_ service.APIRepo      = APIRepo{}
	_ service.PlanRepo     = PlanRepo{}
)

// ---------- providers ----------

type ProviderRepo struct{ DB DB }

const providerCols = `id, name, COALESCE(contact_email, ''), agreement_signed_at, status, created_at`

func scanProvider(s scanner, p *domain.Provider) error {
	return s.Scan(&p.ID, &p.Name, &p.ContactEmail, &p.AgreementSignedAt, &p.Status, &p.CreatedAt)
}

func (r ProviderRepo) Create(ctx context.Context, p domain.Provider) (domain.Provider, error) {
	err := r.DB.q(ctx).QueryRow(ctx, `
		INSERT INTO providers (name, contact_email, agreement_signed_at, status)
		VALUES ($1, NULLIF($2, ''), $3, $4) RETURNING id, created_at`,
		p.Name, p.ContactEmail, p.AgreementSignedAt, p.Status).Scan(&p.ID, &p.CreatedAt)
	return p, mapErr(err)
}

func (r ProviderRepo) Get(ctx context.Context, id string) (domain.Provider, error) {
	var p domain.Provider
	err := scanProvider(r.DB.q(ctx).QueryRow(ctx, `SELECT `+providerCols+` FROM providers WHERE id = $1`, id), &p)
	return p, mapErr(err)
}

func (r ProviderRepo) List(ctx context.Context) ([]domain.Provider, error) {
	rows, err := r.DB.q(ctx).Query(ctx, `SELECT `+providerCols+` FROM providers ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Provider{}
	for rows.Next() {
		var p domain.Provider
		if err := scanProvider(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r ProviderRepo) SignAgreement(ctx context.Context, id string, at time.Time) (domain.Provider, error) {
	var p domain.Provider
	err := scanProvider(r.DB.q(ctx).QueryRow(ctx, `
		UPDATE providers SET agreement_signed_at = COALESCE(agreement_signed_at, $2) WHERE id = $1
		RETURNING `+providerCols, id, at), &p)
	return p, mapErr(err)
}

// ---------- APIs (any status) ----------

type APIRepo struct{ DB DB }

func (r APIRepo) Create(ctx context.Context, a domain.API) (domain.API, error) {
	err := r.DB.q(ctx).QueryRow(ctx, `
		INSERT INTO apis (provider_id, slug, name, description, category, base_url, mock_url,
		                  upstream_auth_type, upstream_auth_header, upstream_secret_sealed, upstream_rate_limit_rps, status)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''), NULLIF($10, ''), NULLIF($11::numeric, 0), $12)
		RETURNING id`,
		a.ProviderID, a.Slug, a.Name, a.Description, a.Category, a.BaseURL, a.MockURL,
		a.AuthType, a.AuthHeader, a.SecretSealed, a.UpstreamRPS, a.Status).Scan(&a.ID)
	return a, mapErr(err)
}

func (r APIRepo) GetBySlug(ctx context.Context, slug string) (domain.API, error) {
	var a domain.API
	err := scanAPI(r.DB.q(ctx).QueryRow(ctx, `SELECT `+apiCols+` FROM apis a WHERE a.slug = $1`, slug), &a)
	return a, mapErr(err)
}

func (r APIRepo) GetByID(ctx context.Context, id string) (domain.API, error) {
	var a domain.API
	err := scanAPI(r.DB.q(ctx).QueryRow(ctx, `SELECT `+apiCols+` FROM apis a WHERE a.id = $1`, id), &a)
	return a, mapErr(err)
}

func (r APIRepo) List(ctx context.Context) ([]domain.API, error) {
	rows, err := r.DB.q(ctx).Query(ctx, `SELECT `+apiCols+` FROM apis a ORDER BY a.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.API{}
	for rows.Next() {
		var a domain.API
		if err := scanAPI(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Update writes the listing fields and credential. It never changes the
// slug, provider or status.
func (r APIRepo) Update(ctx context.Context, a domain.API) error {
	tag, err := r.DB.q(ctx).Exec(ctx, `
		UPDATE apis SET name = $2, description = $3, category = $4, base_url = $5, mock_url = NULLIF($6, ''),
		       upstream_auth_type = $7, upstream_auth_header = NULLIF($8, ''), upstream_secret_sealed = NULLIF($9, ''),
		       upstream_rate_limit_rps = NULLIF($10::numeric, 0)
		WHERE id = $1`,
		a.ID, a.Name, a.Description, a.Category, a.BaseURL, a.MockURL, a.AuthType, a.AuthHeader, a.SecretSealed, a.UpstreamRPS)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r APIRepo) SetStatus(ctx context.Context, id, status string) error {
	tag, err := r.DB.q(ctx).Exec(ctx, `UPDATE apis SET status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ---------- plans ----------

type PlanRepo struct{ DB DB }

func (r PlanRepo) Create(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	err := r.DB.q(ctx).QueryRow(ctx, `
		INSERT INTO plans (api_id, name, kind, monthly_fee_micro, included_calls, price_per_call_micro,
		                   provider_cost_per_call_micro, rate_limit_rps, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		p.APIID, p.Name, p.Kind, int64(p.MonthlyFee), p.IncludedCalls, int64(p.PricePerCall),
		int64(p.ProviderCostPerCall), p.RateLimitRPS, p.Status).Scan(&p.ID)
	return p, mapErr(err)
}

func (r PlanRepo) Get(ctx context.Context, id string) (domain.Plan, error) {
	var p domain.Plan
	err := r.DB.q(ctx).QueryRow(ctx, `SELECT `+planCols+` FROM plans p WHERE p.id = $1`, id).Scan(planDest(&p)...)
	return p, mapErr(err)
}

func (r PlanRepo) ListByAPI(ctx context.Context, apiID string) ([]domain.Plan, error) {
	rows, err := r.DB.q(ctx).Query(ctx, `SELECT `+planCols+` FROM plans p WHERE p.api_id = $1
		ORDER BY p.status, p.monthly_fee_micro, p.price_per_call_micro, p.name`, apiID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []domain.Plan{}
	for rows.Next() {
		var p domain.Plan
		if err := rows.Scan(planDest(&p)...); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r PlanRepo) SetStatus(ctx context.Context, id, status string) error {
	tag, err := r.DB.q(ctx).Exec(ctx, `UPDATE plans SET status = $2 WHERE id = $1`, id, status)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
