package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is the minimal query surface repos depend on: the three methods that
// *pgxpool.Pool and pgx.Tx share, so a repo constructed over a tx behaves
// exactly like one over the pool. The compile-time assertions below fail the
// build if a pgx upgrade ever changes either signature.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ DBTX = (*pgxpool.Pool)(nil)
	_ DBTX = pgx.Tx(nil)
)

// WithTx runs fn inside a transaction with default options and commits if fn
// returns nil. On error or panic the transaction is rolled back; a panic is
// re-raised after rollback so it is never swallowed.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return WithTxOptions(ctx, pool, pgx.TxOptions{}, fn)
}

// WithTxOptions is WithTx with explicit isolation / access mode, for the rare
// query that needs SERIALIZABLE or a read-only snapshot.
func WithTxOptions(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions, fn func(ctx context.Context, tx pgx.Tx) error) (err error) {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("db: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			// Rollback's own error is deliberately dropped: the caller's error
			// is the interesting one, and a rollback failing after a failed
			// statement is almost always "connection already gone".
			_ = tx.Rollback(ctx)
		}
	}()

	// `return` assigns the named result before the deferred rollback reads it.
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit tx: %w", err)
	}
	return nil
}
