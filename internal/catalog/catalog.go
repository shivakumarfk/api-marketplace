// Package catalog is the gateway's in-memory view of keys, APIs, plans and
// subscriptions. The gateway never queries PostgreSQL on the hot path: it
// works from a Snapshot that is refreshed in the background, so it keeps
// serving even if the control plane or database is briefly down.
package catalog

import (
	"net/netip"
	"time"
)

type Key struct {
	ID        string
	OrgID     string
	Env       string // production | sandbox
	Status    string // active | rotating | revoked
	ExpiresAt *time.Time
	IPAllow   []netip.Prefix
}

// Usable reports whether the key may be used at time now.
func (k Key) Usable(now time.Time) bool {
	if k.Status != "active" && k.Status != "rotating" {
		return false
	}
	return k.ExpiresAt == nil || now.Before(*k.ExpiresAt)
}

// AllowsIP reports whether the client IP passes the key's allowlist.
// An empty allowlist allows every IP.
func (k Key) AllowsIP(ip netip.Addr) bool {
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

type Org struct {
	ID          string
	Type        string // individual | institution
	BillingMode string // prepaid | postpaid
	Status      string
}

type API struct {
	ID          string
	Slug        string
	BaseURL     string
	MockURL     string // used for sandbox keys
	AuthType    string // api_key | none (oauth_cc later)
	AuthHeader  string // header the provider expects, e.g. X-Api-Key
	Secret      string // decrypted upstream credential, never logged
	UpstreamRPS float64
	Status      string
}

type Plan struct {
	ID                string
	Kind              string // free | tier | payg
	IncludedCalls     int64
	PricePerCallMicro int64
	ProviderCostMicro int64
	RateLimitRPS      float64
}

type Subscription struct {
	ID          string
	OrgID       string
	APIID       string
	PlanID      string
	Status      string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// Snapshot is an immutable, fully loaded view. Replace it, never mutate it.
type Snapshot struct {
	LoadedAt   time.Time
	KeysByHash map[string]Key
	Orgs       map[string]Org
	APIsBySlug map[string]API
	Plans      map[string]Plan
	Subs       map[string]Subscription // keyed by SubKey(orgID, apiID)
}

func SubKey(orgID, apiID string) string { return orgID + "|" + apiID }

func Empty() *Snapshot {
	return &Snapshot{
		KeysByHash: map[string]Key{}, Orgs: map[string]Org{}, APIsBySlug: map[string]API{},
		Plans: map[string]Plan{}, Subs: map[string]Subscription{},
	}
}
