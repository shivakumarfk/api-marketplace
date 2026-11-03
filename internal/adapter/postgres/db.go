// Package postgres implements the service repositories and the gateway's
// catalog source on PostgreSQL (pgx).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"apimarket/internal/domain"
)

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txKey struct{}

// DB wraps the pool and hands repositories the current transaction, if any.
type DB struct{ Pool *pgxpool.Pool }

func (d DB) q(ctx context.Context) querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return d.Pool
}

// TxManager implements service.TxManager.
type TxManager struct{ DB DB }

func (m TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx) // already inside a transaction
	}
	tx, err := m.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// mapErr turns driver errors into domain errors where the meaning is clear.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "22P02": // invalid_text_representation, e.g. a malformed UUID in a path
			return domain.ErrNotFound
		case "23503": // foreign_key_violation
			return domain.ErrNotFound
		case "23505": // unique_violation
			return &domain.Error{Kind: domain.KindConflict, Code: "ALREADY_EXISTS", Message: "already exists", Err: err}
		case "23514": // check_violation
			return &domain.Error{Kind: domain.KindInvalid, Code: "CONSTRAINT_VIOLATION",
				Message: fmt.Sprintf("violates rule %s", strings.TrimSpace(pe.ConstraintName)), Err: err}
		}
	}
	return err
}
