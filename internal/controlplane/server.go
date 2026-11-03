// Package controlplane is the console/admin API (modular monolith). Stage 0
// exposes a minimal admin surface protected by a static token; Google
// sign-in and the customer console come next (build order §9).
package controlplane

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"apimarket/internal/apikey"
	"apimarket/internal/gstin"
	"apimarket/internal/limiter"
	"apimarket/internal/money"
)

type Server struct {
	DB         *pgxpool.Pool
	Redis      *redis.Client
	AdminToken string
	Log        *slog.Logger
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /v1/catalog/apis", s.listCatalog)
	mux.HandleFunc("POST /v1/admin/orgs", s.admin(s.createOrg))
	mux.HandleFunc("POST /v1/admin/orgs/{org}/keys", s.admin(s.createKey))
	mux.HandleFunc("POST /v1/admin/keys/{key}/revoke", s.admin(s.revokeKey))
	mux.HandleFunc("POST /v1/admin/orgs/{org}/subscriptions", s.admin(s.subscribe))
	mux.HandleFunc("POST /v1/admin/orgs/{org}/topups", s.admin(s.topup))
	mux.HandleFunc("GET /v1/admin/orgs/{org}/balance", s.admin(s.balance))
	return mux
}

// ---- helpers ----

type apiErr struct {
	status int
	code   string
	msg    string
}

func (e apiErr) Error() string { return e.msg }

func bad(code, msg string) error { return apiErr{http.StatusBadRequest, code, msg} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) admin(h func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.AdminToken == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(s.AdminToken)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "UNAUTHORIZED", "message": "admin token required"}})
			return
		}
		if err := h(w, r); err != nil {
			var ae apiErr
			if errors.As(err, &ae) {
				writeJSON(w, ae.status, map[string]any{"error": map[string]string{"code": ae.code, "message": ae.msg}})
				return
			}
			s.Log.Error("request failed", "path", r.URL.Path, "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "INTERNAL", "message": "internal error"}})
		}
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return bad("BAD_JSON", err.Error())
	}
	return nil
}

func (s *Server) audit(ctx context.Context, tx pgx.Tx, orgID, action, targetType, targetID string, meta any) error {
	b, _ := json.Marshal(meta)
	_, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, actor_type, actor_id, action, target_type, target_id, metadata)
		VALUES (NULLIF($1,'')::uuid, 'platform_admin', 'admin-token', $2, $3, $4, $5)`, orgID, action, targetType, targetID, b)
	return err
}

// ---- catalog ----

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `
		SELECT a.slug, a.name, a.description, a.category, p.id, p.name, p.kind,
		       p.monthly_fee_micro, p.included_calls, p.price_per_call_micro, p.rate_limit_rps
		FROM apis a JOIN plans p ON p.api_id = a.id AND p.status = 'active'
		WHERE a.status = 'published' ORDER BY a.name, p.monthly_fee_micro`)
	if err != nil {
		s.Log.Error("catalog", "err", err)
		writeJSON(w, 500, map[string]string{"error": "internal"})
		return
	}
	defer rows.Close()
	type plan struct {
		ID           string  `json:"id"`
		Name         string  `json:"name"`
		Kind         string  `json:"kind"`
		MonthlyFee   string  `json:"monthly_fee"`
		IncludedCall int64   `json:"included_calls"`
		PricePerCall string  `json:"price_per_call"`
		RateLimitRPS float64 `json:"rate_limit_rps"`
	}
	type api struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Plans       []plan `json:"plans"`
	}
	var out []*api
	idx := map[string]*api{}
	for rows.Next() {
		var a api
		var p plan
		var fee, price int64
		if err := rows.Scan(&a.Slug, &a.Name, &a.Description, &a.Category, &p.ID, &p.Name, &p.Kind, &fee, &p.IncludedCall, &price, &p.RateLimitRPS); err != nil {
			s.Log.Error("catalog scan", "err", err)
			writeJSON(w, 500, map[string]string{"error": "internal"})
			return
		}
		p.MonthlyFee, p.PricePerCall = money.Micro(fee).String(), fmt.Sprintf("₹%.4f", float64(price)/1e6)
		cur, ok := idx[a.Slug]
		if !ok {
			cur = &a
			idx[a.Slug] = cur
			out = append(out, cur)
		}
		cur.Plans = append(cur.Plans, p)
	}
	writeJSON(w, 200, map[string]any{"apis": out})
}

// ---- orgs ----

func (s *Server) createOrg(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Name  string `json:"name"`
		Type  string `json:"type"` // individual | institution
		GSTIN string `json:"gstin"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" {
		return bad("NAME_REQUIRED", "name is required")
	}
	mode := ""
	switch in.Type {
	case "individual":
		mode = "prepaid" // individuals are always prepaid
		in.GSTIN = ""
	case "institution":
		mode = "postpaid"
		if err := gstin.ValidateFormat(in.GSTIN); err != nil {
			return bad("INVALID_GSTIN", err.Error())
		}
		in.GSTIN = gstin.Normalize(in.GSTIN)
	default:
		return bad("INVALID_TYPE", "type must be individual or institution")
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO organizations (name, type, billing_mode, gstin)
		VALUES ($1,$2,$3,NULLIF($4,'')) RETURNING id`, in.Name, in.Type, mode, in.GSTIN).Scan(&id); err != nil {
		return err
	}
	if err := s.audit(ctx, tx, id, "org.created", "organization", id, map[string]string{"type": in.Type}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if mode == "prepaid" {
		s.Redis.SetNX(ctx, limiter.BalanceKey(id), 0, 0)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "type": in.Type, "billing_mode": mode})
	return nil
}

// ---- keys ----

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Env  string `json:"env"`
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	env := apikey.Env(in.Env)
	if env != apikey.EnvProduction && env != apikey.EnvSandbox {
		return bad("INVALID_ENV", "env must be production or sandbox")
	}
	k, err := apikey.Generate(env)
	if err != nil {
		return err
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO api_keys (org_id, env, name, prefix, secret_hash)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`, r.PathValue("org"), in.Env, in.Name, k.Prefix, k.Hash).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "foreign key") || strings.Contains(err.Error(), "invalid input syntax") {
			return apiErr{http.StatusNotFound, "ORG_NOT_FOUND", "organization not found"}
		}
		return err
	}
	if err := s.audit(ctx, tx, r.PathValue("org"), "key.created", "api_key", id, map[string]string{"env": in.Env, "prefix": k.Prefix}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"id": id, "key": k.Plain, "prefix": k.Prefix, "env": in.Env,
		"note": "store this key now; it will not be shown again",
	})
	return nil
}

func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orgID string
	err = tx.QueryRow(ctx, `UPDATE api_keys SET status='revoked', revoked_at=now() WHERE id=$1 AND status <> 'revoked' RETURNING org_id`,
		r.PathValue("key")).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "invalid input syntax")) {
		return apiErr{http.StatusNotFound, "KEY_NOT_FOUND", "active key not found"}
	}
	if err != nil {
		return err
	}
	if err := s.audit(ctx, tx, orgID, "key.revoked", "api_key", r.PathValue("key"), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]string{"status": "revoked", "effective": "within the gateway catalog refresh interval"})
	return nil
}

// ---- subscriptions ----

func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		APISlug string `json:"api_slug"`
		PlanID  string `json:"plan_id"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var apiID string
	err = tx.QueryRow(ctx, `SELECT a.id FROM apis a JOIN plans p ON p.api_id = a.id
		WHERE a.slug=$1 AND a.status='published' AND p.id::text=$2 AND p.status='active'`, in.APISlug, in.PlanID).Scan(&apiID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apiErr{http.StatusNotFound, "PLAN_NOT_FOUND", "no active plan with that id for that API"}
	}
	if err != nil {
		return err
	}
	start := time.Now().UTC()
	periodStart := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO subscriptions (org_id, api_id, plan_id, period_start, period_end)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (org_id, api_id) WHERE status = 'active'
		DO UPDATE SET plan_id = EXCLUDED.plan_id
		RETURNING id`, r.PathValue("org"), apiID, in.PlanID, periodStart, periodStart.AddDate(0, 1, 0)).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "foreign key") || strings.Contains(err.Error(), "invalid input syntax") {
			return apiErr{http.StatusNotFound, "ORG_NOT_FOUND", "organization not found"}
		}
		return err
	}
	// TODO(billing): charge the monthly tier fee (prepaid: from balance at period start).
	if err := s.audit(ctx, tx, r.PathValue("org"), "subscription.created", "subscription", id, in); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "api": in.APISlug, "plan_id": in.PlanID})
	return nil
}

// ---- prepaid balance ----

// topup records a card top-up for a prepaid org. Stage 0 has no payment
// gateway yet, so this stands in for the PSP webhook that confirms payment.
func (s *Server) topup(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		AmountRupees int64 `json:"amount_rupees"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.AmountRupees <= 0 || in.AmountRupees > 1_000_000 {
		return bad("INVALID_AMOUNT", "amount_rupees must be between 1 and 1000000")
	}
	orgID := r.PathValue("org")
	ctx := r.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var mode string
	if err := tx.QueryRow(ctx, `SELECT billing_mode FROM organizations WHERE id::text=$1`, orgID).Scan(&mode); err != nil {
		return apiErr{http.StatusNotFound, "ORG_NOT_FOUND", "organization not found"}
	}
	if mode != "prepaid" {
		return bad("NOT_PREPAID", "only prepaid (individual) accounts can top up")
	}
	amount := money.FromRupees(in.AmountRupees)
	txnID, err := PostTxn(ctx, tx, "topup", "", []Entry{
		{Owner: "platform", OwnerID: "", Kind: "cash", Direction: "debit", Amount: amount},
		{Owner: "org", OwnerID: orgID, Kind: "wallet", Direction: "credit", Amount: amount},
	})
	if err != nil {
		return err
	}
	if err := s.audit(ctx, tx, orgID, "wallet.topup", "ledger_txn", txnID, map[string]int64{"amount_micro": int64(amount)}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Ledger committed first; Redis is updated after. If this fails, the
	// reconciliation job (TODO) or a rebuild restores it from the ledger.
	bal, err := s.Redis.IncrBy(ctx, limiter.BalanceKey(orgID), int64(amount)).Result()
	if err != nil {
		s.Log.Error("redis balance update failed after ledger commit", "org", orgID, "err", err)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"txn_id": txnID, "balance": money.Micro(bal).String()})
	return nil
}

func (s *Server) balance(w http.ResponseWriter, r *http.Request) error {
	orgID := r.PathValue("org")
	live, err := s.Redis.Get(r.Context(), limiter.BalanceKey(orgID)).Int64()
	if errors.Is(err, redis.Nil) {
		return apiErr{http.StatusNotFound, "NO_BALANCE", "no prepaid balance for this organization"}
	}
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"balance": money.Micro(live).String(), "balance_micro": live})
	return nil
}

// RebuildBalances sets the Redis balance for every prepaid org whose key is
// missing, from the ledger. Redis is never the source of truth.
//
// TODO(billing): once usage is posted to the ledger, also subtract usage
// reserved in Redis but not yet posted, and run the hourly reconciliation.
func (s *Server) RebuildBalances(ctx context.Context) (int, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT o.id, COALESCE(SUM(CASE e.direction WHEN 'credit' THEN e.amount_micro ELSE -e.amount_micro END), 0)
		FROM organizations o
		LEFT JOIN ledger_accounts a ON a.owner_type='org' AND a.owner_id=o.id::text AND a.kind='wallet'
		LEFT JOIN ledger_entries e ON e.account_id = a.id
		WHERE o.billing_mode = 'prepaid'
		GROUP BY o.id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id string
		var bal int64
		if err := rows.Scan(&id, &bal); err != nil {
			return n, err
		}
		ok, err := s.Redis.SetNX(ctx, limiter.BalanceKey(id), bal, 0).Result()
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, rows.Err()
}
