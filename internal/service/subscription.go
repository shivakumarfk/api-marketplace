package service

import (
	"context"
	"errors"

	"apimarket/internal/domain"
)

type SubscriptionService struct {
	Tx      TxManager
	Orgs    OrgRepo
	Catalog CatalogRepo
	Subs    SubscriptionRepo
	Audit   AuditRepo
	Clock   Clock
}

// Subscribe puts the org on a plan for an API. Phase 1: only an org Admin
// (here: a platform admin) may do this.
func (s *SubscriptionService) Subscribe(ctx context.Context, actor domain.Actor, orgID, apiSlug, planID string) (domain.Subscription, error) {
	var out domain.Subscription
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := getActiveOrg(ctx, s.Orgs, orgID); err != nil {
			return err
		}
		api, plan, err := s.Catalog.FindPlan(ctx, apiSlug, planID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.NotFound("PLAN_NOT_FOUND", "no active plan with that id for that API")
		}
		if err != nil {
			return err
		}
		start, end := domain.MonthlyPeriod(s.Clock.Now())
		out, err = s.Subs.Upsert(ctx, domain.Subscription{
			OrgID: orgID, APIID: api.ID, PlanID: plan.ID, Status: "active",
			PeriodStart: start, PeriodEnd: end, CreatedBy: actor.ID,
		})
		if err != nil {
			return err
		}
		// TODO(billing): charge the monthly tier fee (prepaid: from the balance at period start).
		return s.Audit.Record(ctx, audit(actor, orgID, "subscription.created", "subscription", out.ID,
			map[string]any{"api": apiSlug, "plan_id": plan.ID}))
	})
	return out, err
}
