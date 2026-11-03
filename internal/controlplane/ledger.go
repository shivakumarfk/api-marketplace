package controlplane

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"apimarket/internal/money"
)

// Entry is one line of a double-entry transaction.
type Entry struct {
	Owner     string // org | provider | platform
	OwnerID   string
	Kind      string // wallet | receivable | payable | revenue | tax | cash
	Direction string // debit | credit
	Amount    money.Micro
}

var ErrUnbalanced = errors.New("ledger: debits and credits must be equal and positive")

// Balanced reports whether entries form a valid transaction: at least two
// lines, all amounts positive, total debits equal total credits.
func Balanced(es []Entry) bool {
	if len(es) < 2 {
		return false
	}
	var d, c money.Micro
	for _, e := range es {
		if e.Amount <= 0 {
			return false
		}
		switch e.Direction {
		case "debit":
			d += e.Amount
		case "credit":
			c += e.Amount
		default:
			return false
		}
	}
	return d == c
}

// PostTxn writes a balanced transaction inside tx and returns its id.
// The database also enforces balance with a deferred constraint trigger.
func PostTxn(ctx context.Context, tx pgx.Tx, refType, refID string, es []Entry) (string, error) {
	if !Balanced(es) {
		return "", ErrUnbalanced
	}
	var txnID string
	if err := tx.QueryRow(ctx, `INSERT INTO ledger_txns (ref_type, ref_id) VALUES ($1, NULLIF($2,'')) RETURNING id`, refType, refID).Scan(&txnID); err != nil {
		return "", err
	}
	for _, e := range es {
		var acct string
		err := tx.QueryRow(ctx, `
			INSERT INTO ledger_accounts (owner_type, owner_id, kind) VALUES ($1, $2, $3)
			ON CONFLICT (owner_type, owner_id, kind) DO UPDATE SET kind = EXCLUDED.kind
			RETURNING id`, e.Owner, e.OwnerID, e.Kind).Scan(&acct)
		if err != nil {
			return "", fmt.Errorf("ledger account: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries (txn_id, account_id, amount_micro, direction) VALUES ($1,$2,$3,$4)`,
			txnID, acct, int64(e.Amount), e.Direction); err != nil {
			return "", err
		}
	}
	return txnID, nil
}
