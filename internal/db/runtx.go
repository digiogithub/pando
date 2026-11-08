package db

import (
	"context"
	"database/sql"

	"github.com/digiogithub/pando/rag/sqlx"
)

// TxBeginner is satisfied by *sql.DB (and by test doubles that open real
// transactions).
type TxBeginner = sqlx.TxBeginner

// RunTx runs fn inside a transaction and commits it. When the commit (or any
// statement) loses against a concurrent writer — SQLITE_BUSY_SNAPSHOT under the
// multiwriter engine, SQLITE_BUSY under stock SQLite — the transaction is rolled
// back and fn runs again, whole, on a fresh snapshot.
//
// fn may therefore run more than once: it must only act through tx and must not
// have effects outside the transaction (send events, mutate shared state)
// before RunTx returns. Compute expensive inputs such as embeddings before
// calling RunTx.
//
// The loop itself lives in github.com/digiogithub/pando/rag/sqlx so that the
// rag module and Pando share one implementation; this wrapper supplies the
// engine-aware IsRetryable.
func RunTx(ctx context.Context, conn TxBeginner, fn func(*sql.Tx) error) error {
	return RunTxOpts(ctx, conn, nil, fn)
}

// RunTxOpts is RunTx with explicit transaction options.
func RunTxOpts(ctx context.Context, conn TxBeginner, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	return sqlx.RunTxOpts(ctx, conn, opts, IsRetryable, fn)
}
