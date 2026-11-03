package domain

import (
	"net/netip"
	"strings"
	"time"

	"apimarket/internal/domain/gstin"
	"apimarket/internal/domain/money"
)

// ---------- organizations ----------

type OrgType string
type BillingMode string

const (
	OrgIndividual  OrgType = "individual"
	OrgInstitution OrgType = "institution"

	Prepaid  BillingMode = "prepaid"
	Postpaid BillingMode = "postpaid"
)

type Org struct {
	ID          string
	Name        string
	Type        OrgType
	BillingMode BillingMode
	GSTIN       string
	Status      string
	CreatedAt   time.Time
}

// NewOrg applies the account rules: individuals are always prepaid;
// institutions are postpaid and need a well-formed GSTIN (format check only).
func NewOrg(name string, t OrgType, gstinRaw string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, Invalid("NAME_REQUIRED", "name is required")
	}
	o := Org{Name: name, Type: t, Status: "active"}
	switch t {
	case OrgIndividual:
		o.BillingMode = Prepaid
	case OrgInstitution:
		if err := gstin.ValidateFormat(gstinRaw); err != nil {
			return Org{}, Invalid("INVALID_GSTIN", err.Error())
		}
		o.BillingMode, o.GSTIN = Postpaid, gstin.Normalize(gstinRaw)
	default:
		return Org{}, Invalid("INVALID_TYPE", "type must be individual or institution")
	}
	return o, nil
}

func (o Org) Active() bool { return o.Status == "active" }

// ---------- keys ----------

type KeyEnv string

const (
	EnvProduction KeyEnv = "production"
	EnvSandbox    KeyEnv = "sandbox"
)

func ParseKeyEnv(s string) (KeyEnv, error) {
	switch KeyEnv(s) {
	case EnvProduction, EnvSandbox:
		return KeyEnv(s), nil
	}
	return "", Invalid("INVALID_ENV", "env must be production or sandbox")
}

type APIKey struct {
	ID        string
	OrgID     string
	Env       KeyEnv
	Name      string
	Prefix    string
	Hash      string // SHA-256 of the plain key; the plain key is never stored
	IPAllow   []netip.Prefix
	ExpiresAt *time.Time
	Status    string // active | rotating | revoked
	CreatedAt time.Time
}

// Usable reports whether the key may be used at time now.
func (k APIKey) Usable(now time.Time) bool {
	if k.Status != "active" && k.Status != "rotating" {
		return false
	}
	return k.ExpiresAt == nil || now.Before(*k.ExpiresAt)
}

// AllowsIP reports whether ip passes the allowlist (empty = allow all).
func (k APIKey) AllowsIP(ip netip.Addr) bool {
	if len(k.IPAllow) == 0 {
		return true
	}
	for _, p := range k.IPAllow {
		if p.Contains(ip.Unmap()) {
			return true
		}
	}
	return false
}

// ---------- catalog ----------

type API struct {
	ID           string
	ProviderID   string
	Slug         string
	Name         string
	Description  string
	Category     string
	BaseURL      string
	MockURL      string
	AuthType     string // none | api_key | oauth_cc
	AuthHeader   string
	SecretSealed string // encrypted upstream credential
	UpstreamRPS  float64
	Status       string
}

type PlanKind string

const (
	PlanFree PlanKind = "free"
	PlanTier PlanKind = "tier"
	PlanPAYG PlanKind = "payg"
)

type Plan struct {
	ID                  string
	APIID               string
	Name                string
	Kind                PlanKind
	MonthlyFee          money.Micro
	IncludedCalls       int64 // 0 = no quota
	PricePerCall        money.Micro
	ProviderCostPerCall money.Micro
	RateLimitRPS        float64 // 0 = unlimited
	Status              string
}

type Subscription struct {
	ID          string
	OrgID       string
	APIID       string
	PlanID      string
	Status      string
	PeriodStart time.Time
	PeriodEnd   time.Time
	CreatedBy   string
}

// MonthlyPeriod returns the calendar month (UTC) containing t.
func MonthlyPeriod(t time.Time) (start, end time.Time) {
	t = t.UTC()
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

// ---------- ledger ----------

type Direction string

const (
	Debit  Direction = "debit"
	Credit Direction = "credit"
)

// Account identifies a ledger account.
type Account struct {
	OwnerType string // org | provider | platform
	OwnerID   string
	Kind      string // wallet | receivable | payable | revenue | tax | cash
}

func OrgWallet(orgID string) Account { return Account{"org", orgID, "wallet"} }
func PlatformCash() Account          { return Account{"platform", "", "cash"} }

type Entry struct {
	Account   Account
	Direction Direction
	Amount    money.Micro
}

// Txn is a double-entry transaction. RefType+RefID make posting idempotent.
type Txn struct {
	RefType string
	RefID   string
	Entries []Entry
}

// Balanced reports whether the transaction is valid: at least two lines,
// every amount positive, total debits equal total credits.
func (t Txn) Balanced() bool {
	if len(t.Entries) < 2 {
		return false
	}
	var d, c money.Micro
	for _, e := range t.Entries {
		if e.Amount <= 0 {
			return false
		}
		switch e.Direction {
		case Debit:
			d += e.Amount
		case Credit:
			c += e.Amount
		default:
			return false
		}
	}
	return d == c
}

// TopUpTxn records a prepaid card top-up: cash in, wallet credited.
func TopUpTxn(orgID, paymentRef string, amount money.Micro) Txn {
	return Txn{RefType: "topup", RefID: paymentRef, Entries: []Entry{
		{PlatformCash(), Debit, amount},
		{OrgWallet(orgID), Credit, amount},
	}}
}

// ---------- audit ----------

type AuditEvent struct {
	OrgID      string
	ActorType  string
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	Metadata   map[string]any
}

// Actor is who performed an action, for the audit trail.
type Actor struct {
	Type string // platform_admin | user | system
	ID   string
}
