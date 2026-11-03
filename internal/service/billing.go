package service

import (
	"context"
	"errors"
	"log/slog"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
)

type BillingService struct {
	Tx       TxManager
	Orgs     OrgRepo
	Ledger   LedgerRepo
	Audit    AuditRepo
	Balances BalanceStore
	Log      *slog.Logger
}

const maxTopUpRupees = 1_000_000

// TopUp records a confirmed card payment for a prepaid org. Stage 0 has no
// payment gateway, so the admin API stands in for the PSP webhook; paymentRef
// makes the posting idempotent (the same payment can't be credited twice).
func (s *BillingService) TopUp(ctx context.Context, actor domain.Actor, orgID string, rupees int64, paymentRef string) (txnID string, balance money.Micro, err error) {
	if rupees <= 0 || rupees > maxTopUpRupees {
		return "", 0, domain.Invalid("INVALID_AMOUNT", "amount_rupees must be between 1 and 1000000")
	}
	if paymentRef == "" {
		return "", 0, domain.Invalid("PAYMENT_REF_REQUIRED", "payment_ref is required")
	}
	amount := money.FromRupees(rupees)
	err = s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		org, err := getActiveOrg(ctx, s.Orgs, orgID)
		if err != nil {
			return err
		}
		if org.BillingMode != domain.Prepaid {
			return domain.Invalid("NOT_PREPAID", "only prepaid (individual) accounts can top up")
		}
		if txnID, err = s.Ledger.Post(ctx, domain.TopUpTxn(orgID, paymentRef, amount)); err != nil {
			return err
		}
		return s.Audit.Record(ctx, audit(actor, orgID, "wallet.topup", "ledger_txn", txnID,
			map[string]any{"amount_micro": int64(amount), "payment_ref": paymentRef}))
	})
	if err != nil {
		return "", 0, err
	}
	// Ledger committed first; the live balance follows. If this fails, a
	// rebuild from the ledger (or the reconciliation job, TODO) restores it.
	balance, err = s.Balances.Add(ctx, orgID, amount)
	if err != nil {
		s.Log.Error("live balance update failed after ledger commit", "org", orgID, "err", err)
		return txnID, 0, nil
	}
	return txnID, balance, nil
}

// Balance returns the live prepaid balance.
func (s *BillingService) Balance(ctx context.Context, orgID string) (money.Micro, error) {
	b, err := s.Balances.Get(ctx, orgID)
	if errors.Is(err, domain.ErrNotFound) {
		return 0, domain.NotFound("NO_BALANCE", "no prepaid balance for this organization")
	}
	return b, err
}

// RebuildBalances restores missing live balances from the ledger.
//
// TODO(billing): once usage is posted to the ledger, subtract usage reserved
// in Redis but not yet posted, and run the hourly reconciliation.
func (s *BillingService) RebuildBalances(ctx context.Context) (restored int, err error) {
	balances, err := s.Ledger.PrepaidWalletBalances(ctx)
	if err != nil {
		return 0, err
	}
	for orgID, b := range balances {
		ok, err := s.Balances.SetIfMissing(ctx, orgID, b)
		if err != nil {
			return restored, err
		}
		if ok {
			restored++
		}
	}
	return restored, nil
}
