package controlplane

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
	"apimarket/internal/service"
)

type handlers struct{ d Deps }

// ---------- DTOs (the public JSON contract) ----------

type createOrgReq struct {
	Name  string `json:"name" binding:"required"`
	Type  string `json:"type" binding:"required,oneof=individual institution"`
	GSTIN string `json:"gstin"`
}

type orgResp struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	BillingMode string    `json:"billing_mode"`
	GSTIN       string    `json:"gstin,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type createKeyReq struct {
	Env  string `json:"env" binding:"required,oneof=production sandbox"`
	Name string `json:"name"`
}

type keyResp struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Prefix string `json:"prefix"`
	Env    string `json:"env"`
	Note   string `json:"note"`
}

type subscribeReq struct {
	APISlug string `json:"api_slug" binding:"required"`
	PlanID  string `json:"plan_id" binding:"required"`
}

type subscriptionResp struct {
	ID          string    `json:"id"`
	API         string    `json:"api"`
	PlanID      string    `json:"plan_id"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
}

type topUpReq struct {
	AmountRupees int64  `json:"amount_rupees" binding:"required"`
	PaymentRef   string `json:"payment_ref" binding:"required"`
}

type balanceResp struct {
	Balance      string `json:"balance"`
	BalanceMicro int64  `json:"balance_micro"`
}

type planResp struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	MonthlyFee    string  `json:"monthly_fee"`
	IncludedCalls int64   `json:"included_calls"`
	PricePerCall  string  `json:"price_per_call"`
	RateLimitRPS  float64 `json:"rate_limit_rps"`
}

type apiResp struct {
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Plans       []planResp `json:"plans"`
}

// ---------- handlers ----------

func (h handlers) listCatalog(c *gin.Context) {
	entries, err := h.d.Catalog.ListPublished(c.Request.Context())
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	out := make([]apiResp, 0, len(entries))
	for _, e := range entries {
		a := apiResp{Slug: e.API.Slug, Name: e.API.Name, Description: e.API.Description, Category: e.API.Category}
		for _, p := range e.Plans {
			a.Plans = append(a.Plans, planResp{
				ID: p.ID, Name: p.Name, Kind: string(p.Kind), MonthlyFee: p.MonthlyFee.String(),
				IncludedCalls: p.IncludedCalls, PricePerCall: fmt.Sprintf("₹%.4f", float64(p.PricePerCall)/float64(money.MicroPerRupee)),
				RateLimitRPS: p.RateLimitRPS,
			})
		}
		out = append(out, a)
	}
	c.JSON(http.StatusOK, gin.H{"apis": out})
}

func (h handlers) createOrg(c *gin.Context) {
	var req createOrgReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	o, err := h.d.Orgs.Create(c.Request.Context(), actor(c),
		service.CreateOrgInput{Name: req.Name, Type: domain.OrgType(req.Type), GSTIN: req.GSTIN})
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, orgResp{ID: o.ID, Name: o.Name, Type: string(o.Type), BillingMode: string(o.BillingMode),
		GSTIN: o.GSTIN, CreatedAt: o.CreatedAt})
}

func (h handlers) createKey(c *gin.Context) {
	var req createKeyReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	k, err := h.d.Keys.Create(c.Request.Context(), actor(c), c.Param("org"), req.Env, req.Name)
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, keyResp{ID: k.Key.ID, Key: k.Plain, Prefix: k.Key.Prefix, Env: string(k.Key.Env),
		Note: "store this key now; it will not be shown again"})
}

func (h handlers) revokeKey(c *gin.Context) {
	if err := h.d.Keys.Revoke(c.Request.Context(), actor(c), c.Param("key")); err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked", "effective": "within the gateway catalog refresh interval"})
}

func (h handlers) subscribe(c *gin.Context) {
	var req subscribeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	s, err := h.d.Subs.Subscribe(c.Request.Context(), actor(c), c.Param("org"), req.APISlug, req.PlanID)
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, subscriptionResp{ID: s.ID, API: req.APISlug, PlanID: s.PlanID, PeriodStart: s.PeriodStart, PeriodEnd: s.PeriodEnd})
}

func (h handlers) topUp(c *gin.Context) {
	var req topUpReq
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err)
		return
	}
	txn, bal, err := h.d.Wallets.TopUp(c.Request.Context(), actor(c), c.Param("org"), req.AmountRupees, req.PaymentRef)
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"txn_id": txn, "balance": bal.String(), "balance_micro": int64(bal)})
}

func (h handlers) balance(c *gin.Context) {
	b, err := h.d.Wallets.Balance(c.Request.Context(), c.Param("org"))
	if err != nil {
		fail(c, h.d.Log, err)
		return
	}
	c.JSON(http.StatusOK, balanceResp{Balance: b.String(), BalanceMicro: int64(b)})
}
