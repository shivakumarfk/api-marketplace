package service

import (
	"context"
	"errors"
	"log/slog"

	"apimarket/internal/domain"
)

type OrgService struct {
	Tx       TxManager
	Orgs     OrgRepo
	Audit    AuditRepo
	Balances BalanceStore
	Log      *slog.Logger
}

type CreateOrgInput struct {
	Name  string
	Type  domain.OrgType
	GSTIN string
}

func (s *OrgService) Create(ctx context.Context, actor domain.Actor, in CreateOrgInput) (domain.Org, error) {
	org, err := domain.NewOrg(in.Name, in.Type, in.GSTIN)
	if err != nil {
		return domain.Org{}, err
	}
	err = s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if org, err = s.Orgs.Create(ctx, org); err != nil {
			return err
		}
		return s.Audit.Record(ctx, audit(actor, org.ID, "org.created", "organization", org.ID, map[string]any{"type": org.Type}))
	})
	if err != nil {
		return domain.Org{}, err
	}
	if org.BillingMode == domain.Prepaid {
		if _, err := s.Balances.SetIfMissing(ctx, org.ID, 0); err != nil {
			// Not fatal: the gateway fails closed and the balance is rebuilt from the ledger.
			s.Log.Error("could not initialise live balance", "org", org.ID, "err", err)
		}
	}
	return org, nil
}

// getActiveOrg loads an org and maps "missing" to a client-facing error.
func getActiveOrg(ctx context.Context, repo OrgRepo, id string) (domain.Org, error) {
	o, err := repo.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Org{}, domain.NotFound("ORG_NOT_FOUND", "organization not found")
	}
	if err != nil {
		return domain.Org{}, err
	}
	if !o.Active() {
		return domain.Org{}, domain.Forbidden("ORG_INACTIVE", "organization is not active")
	}
	return o, nil
}

func audit(a domain.Actor, orgID, action, targetType, targetID string, meta map[string]any) domain.AuditEvent {
	return domain.AuditEvent{OrgID: orgID, ActorType: a.Type, ActorID: a.ID, Action: action,
		TargetType: targetType, TargetID: targetID, Metadata: meta}
}
