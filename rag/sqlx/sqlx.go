// Package sqlx holds the transaction helpers the rag packages share: running a
// transaction again, whole, when it loses against a concurrent writer.
//
// The package does not know which SQLite driver the caller uses, so the test
// for "this error means the writer lost a race" is a parameter. Callers that
// own a precise classifier (Pando's internal/db.IsRetryable understands the
// multiwriter engine's error types) pass it in; everyone else gets
// DefaultRetryable, which matches the error text every SQLite driver produces.
package sqlx

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// RetryBudget bounds how long one operation keeps retrying a lost commit
// before the error is returned to the caller.
const RetryBudget = 30 * time.Second

// TxBeginner is satisfied by *sql.DB (and by test doubles that open real
// transactions).
type TxBeginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Retryable reports whether err means the operation lost against a concurrent
// writer and may be run again whole.
type Retryable func(err error) bool

// DefaultRetryable recognises SQLITE_BUSY, SQLITE_LOCKED (including the
// extended BUSY_SNAPSHOT code) and "database is locked" by their error text,
// which every Go SQLite driver includes in its messages.
func DefaultRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "SQLITE_LOCKED") ||
		strings.Contains(s, "database is locked")
}

// RetryLoop runs fn until it succeeds, fails with an error retryable does not
// accept, the context ends or RetryBudget is spent. The backoff starts in the
// microseconds: a lost commit retried against a fresh snapshot usually wins at
// once. A nil retryable means DefaultRetryable.
func RetryLoop(ctx context.Context, retryable Retryable, fn func() error) error {
	if retryable == nil {
		retryable = DefaultRetryable
	}
	deadline := time.Now().Add(RetryBudget)
	backoff := 100 * time.Microsecond
	for attempt := 0; ; attempt++ {
		err := fn()
		if err == nil || !retryable(err) || time.Now().After(deadline) {
			return err
		}
		if attempt < 2 {
			continue
		}
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		if backoff < 20*time.Millisecond {
			backoff *= 2
		}
	}
}

// RunTx runs fn inside a transaction and commits it. When the commit (or any
// statement) loses against a concurrent writer, the transaction is rolled back
// and fn runs again, whole, on a fresh snapshot.
//
// fn may therefore run more than once: it must only act through tx and must not
// have effects outside the transaction (send events, mutate shared state)
// before RunTx returns. Compute expensive inputs such as embeddings before
// calling RunTx.
func RunTx(ctx context.Context, conn TxBeginner, retryable Retryable, fn func(*sql.Tx) error) error {
	return RunTxOpts(ctx, conn, nil, retryable, fn)
}

// RunTxOpts is RunTx with explicit transaction options.
func RunTxOpts(ctx context.Context, conn TxBeginner, opts *sql.TxOptions, retryable Retryable, fn func(*sql.Tx) error) error {
	return RetryLoop(ctx, retryable, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, opts)
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	})
}
