// Package catalog is the gateway's read model: an immutable, fully loaded
// view of keys, orgs, APIs, plans and subscriptions. The gateway never
// queries the database on the hot path.
package catalog

import (
	"time"

	"apimarket/internal/domain"
)

// API is a published API plus its decrypted upstream credential.
type API struct {
	domain.API
	Secret string // never logged or serialised
}

type Snapshot struct {
	LoadedAt   time.Time
	KeysByHash map[string]domain.APIKey
	Orgs       map[string]domain.Org
	APIsBySlug map[string]API
	Plans      map[string]domain.Plan
	Subs       map[string]domain.Subscription // keyed by SubKey(orgID, apiID)
}

func SubKey(orgID, apiID string) string { return orgID + "|" + apiID }

func Empty() *Snapshot {
	return &Snapshot{
		KeysByHash: map[string]domain.APIKey{}, Orgs: map[string]domain.Org{},
		APIsBySlug: map[string]API{}, Plans: map[string]domain.Plan{}, Subs: map[string]domain.Subscription{},
	}
}
