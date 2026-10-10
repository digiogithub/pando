package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"

	multiwriter "github.com/digiogithub/go-sqlite-multiwriter"
	"modernc.org/sqlite"
)

// DriverName is the database/sql driver every Pando connection to pando.db uses.
// It wraps modernc.org/sqlite (which also hosts the multiwriter VFS) and adds two
// things the engine needs from every caller:
//
//   - A statement that runs outside an explicit transaction (autocommit) and
//     writes is retried, whole, when its commit loses against another writer
//     (SQLITE_BUSY_SNAPSHOT under the multiwriter engine, SQLITE_BUSY under stock
//     SQLite). A refused autocommit statement has no effect, so running it again
//     is always safe. Write statements that return rows (INSERT ... RETURNING)
//     are drained into memory first so the retry can happen before the caller
//     sees a single row.
//   - time.Time arguments are bound as RFC 3339 text, the format the previous
//     driver (ncruces/go-sqlite3) wrote, so stored timestamps keep sorting and
//     parsing the same way.
//
// Statements inside an explicit transaction are passed through untouched: a
// transaction that loses must be run again whole, which is what RunTx does.
const DriverName = "pando-sqlite"

// retryBudget bounds how long one autocommit statement keeps retrying a lost
// commit before the error is returned to the caller.
const retryBudget = 30 * time.Second

func init() {
	sql.Register(DriverName, &engineDriver{inner: &sqlite.Driver{}})
}

type engineDriver struct {
	inner *sqlite.Driver
}

func (d *engineDriver) Open(name string) (driver.Conn, error) {
	c, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &engineConn{inner: c}, nil
}

// IsRetryable reports whether err means the transaction (or autocommit
// statement) lost against a concurrent writer and must be run again whole:
// SQLITE_BUSY, SQLITE_BUSY_SNAPSHOT or SQLITE_LOCKED.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if multiwriter.IsBusySnapshot(err) {
		return true
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		code := se.Code() & 0xff
		return code == 5 || code == 6 // SQLITE_BUSY, SQLITE_LOCKED
	}
	// Errors that crossed a fmt.Errorf("%v") boundary lose their type.
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "SQLITE_LOCKED") ||
		strings.Contains(s, "database is locked")
}

// retryLoop runs fn until it succeeds, fails with a non-retryable error, the
// context ends or the retry budget is spent. The backoff starts in the
// microseconds: under the multiwriter engine a lost commit is retried against a
// fresh snapshot and usually wins at once.
func retryLoop(ctx context.Context, fn func() error) error {
	deadline := time.Now().Add(retryBudget)
	backoff := 100 * time.Microsecond
	for attempt := 0; ; attempt++ {
		err := fn()
		if err == nil || !IsRetryable(err) || time.Now().After(deadline) {
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

// isWriteStatement reports whether query can modify the database, judged by its
// leading keyword. Anything that is not clearly a read is treated as a write:
// a read misclassified as a write only costs buffering its rows, the opposite
// would surface a lost commit to the caller.
func isWriteStatement(query string) bool {
	kw, rest := leadingKeyword(query)
	switch kw {
	case "SELECT", "VALUES", "EXPLAIN":
		return false
	case "PRAGMA":
		// "PRAGMA x" and "PRAGMA x(arg)" queries read; "PRAGMA x = v" may write
		// but pragmas are connection state, never a commit worth retrying.
		return false
	case "BEGIN", "COMMIT", "END", "ROLLBACK", "SAVEPOINT", "RELEASE":
		return false
	case "WITH":
		up := strings.ToUpper(rest)
		for _, w := range []string{"INSERT", "UPDATE", "DELETE", "REPLACE"} {
			if containsWord(up, w) {
				return true
			}
		}
		return false
	}
	return true
}

func leadingKeyword(q string) (string, string) {
	for {
		q = strings.TrimLeftFunc(q, unicode.IsSpace)
		switch {
		case strings.HasPrefix(q, "--"):
			if i := strings.IndexByte(q, '\n'); i >= 0 {
				q = q[i+1:]
				continue
			}
			return "", ""
		case strings.HasPrefix(q, "/*"):
			if i := strings.Index(q, "*/"); i >= 0 {
				q = q[i+2:]
				continue
			}
			return "", ""
		case strings.HasPrefix(q, "("):
			q = q[1:]
			continue
		}
		break
	}
	end := strings.IndexFunc(q, func(r rune) bool { return !unicode.IsLetter(r) })
	if end < 0 {
		end = len(q)
	}
	return strings.ToUpper(q[:end]), q[end:]
}

func containsWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !isIdentByte(s[j-1])
		after := j+len(w) == len(s) || !isIdentByte(s[j+len(w)])
		if before && after {
			return true
		}
		i = j + len(w)
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

// normalizeArgs binds time.Time values as RFC 3339 text (see DriverName).
func normalizeArgs(args []driver.NamedValue) []driver.NamedValue {
	var out []driver.NamedValue
	for i, a := range args {
		t, ok := a.Value.(time.Time)
		if !ok {
			continue
		}
		if out == nil {
			out = append([]driver.NamedValue(nil), args...)
		}
		out[i].Value = t.Format(time.RFC3339Nano)
	}
	if out == nil {
		return args
	}
	return out
}

// engineConn wraps a modernc connection. database/sql serialises the use of a
// driver.Conn, so inTx needs no locking.
type engineConn struct {
	inner driver.Conn
	inTx  bool
}

func (c *engineConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *engineConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var (
		s   driver.Stmt
		err error
	)
	if p, ok := c.inner.(driver.ConnPrepareContext); ok {
		s, err = p.PrepareContext(ctx, query)
	} else {
		s, err = c.inner.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &engineStmt{inner: s, conn: c, write: isWriteStatement(query)}, nil
}

func (c *engineConn) Close() error { return c.inner.Close() }

func (c *engineConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *engineConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	var (
		tx  driver.Tx
		err error
	)
	if b, ok := c.inner.(driver.ConnBeginTx); ok {
		tx, err = b.BeginTx(ctx, opts)
	} else {
		tx, err = c.inner.Begin() //nolint:staticcheck // fallback for drivers without BeginTx
	}
	if err != nil {
		return nil, err
	}
	c.inTx = true
	return &engineTx{inner: tx, conn: c}, nil
}

func (c *engineConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ex, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	args = normalizeArgs(args)
	if c.inTx || !isWriteStatement(query) {
		return ex.ExecContext(ctx, query, args)
	}
	var res driver.Result
	err := retryLoop(ctx, func() error {
		var err error
		res, err = ex.ExecContext(ctx, query, args)
		return err
	})
	return res, err
}

func (c *engineConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qr, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	args = normalizeArgs(args)
	if c.inTx || !isWriteStatement(query) {
		return qr.QueryContext(ctx, query, args)
	}
	return bufferedQuery(ctx, func() (driver.Rows, error) { return qr.QueryContext(ctx, query, args) })
}

func (c *engineConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *engineConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *engineConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

type engineTx struct {
	inner driver.Tx
	conn  *engineConn
}

func (t *engineTx) Commit() error {
	defer func() { t.conn.inTx = false }()
	return t.inner.Commit()
}

func (t *engineTx) Rollback() error {
	defer func() { t.conn.inTx = false }()
	return t.inner.Rollback()
}

type engineStmt struct {
	inner driver.Stmt
	conn  *engineConn
	write bool
}

func (s *engineStmt) Close() error  { return s.inner.Close() }
func (s *engineStmt) NumInput() int { return s.inner.NumInput() }

func (s *engineStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), valuesToNamed(args))
}

func (s *engineStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), valuesToNamed(args))
}

func (s *engineStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	args = normalizeArgs(args)
	exec := func() (driver.Result, error) {
		if e, ok := s.inner.(driver.StmtExecContext); ok {
			return e.ExecContext(ctx, args)
		}
		return s.inner.Exec(namedToValues(args)) //nolint:staticcheck // fallback
	}
	if s.conn.inTx || !s.write {
		return exec()
	}
	var res driver.Result
	err := retryLoop(ctx, func() error {
		var err error
		res, err = exec()
		return err
	})
	return res, err
}

func (s *engineStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	args = normalizeArgs(args)
	query := func() (driver.Rows, error) {
		if q, ok := s.inner.(driver.StmtQueryContext); ok {
			return q.QueryContext(ctx, args)
		}
		return s.inner.Query(namedToValues(args)) //nolint:staticcheck // fallback
	}
	if s.conn.inTx || !s.write {
		return query()
	}
	return bufferedQuery(ctx, query)
}

func valuesToNamed(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

func namedToValues(args []driver.NamedValue) []driver.Value {
	out := make([]driver.Value, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

// bufferedQuery runs a write statement that returns rows to completion, so its
// commit has happened (or been refused and retried) before the caller reads.
func bufferedQuery(ctx context.Context, run func() (driver.Rows, error)) (driver.Rows, error) {
	var out *bufferedRows
	err := retryLoop(ctx, func() error {
		rows, err := run()
		if err != nil {
			return err
		}
		b := &bufferedRows{cols: rows.Columns()}
		if tn, ok := rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
			b.types = make([]string, len(b.cols))
			for i := range b.cols {
				b.types[i] = tn.ColumnTypeDatabaseTypeName(i)
			}
		}
		for {
			dest := make([]driver.Value, len(b.cols))
			err = rows.Next(dest)
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				break
			}
			for i, v := range dest {
				if bs, ok := v.([]byte); ok {
					dest[i] = append([]byte(nil), bs...)
				}
			}
			b.rows = append(b.rows, dest)
		}
		if cerr := rows.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type bufferedRows struct {
	cols  []string
	types []string
	rows  [][]driver.Value
	pos   int
}

func (r *bufferedRows) Columns() []string { return r.cols }
func (r *bufferedRows) Close() error      { r.rows = nil; return nil }

func (r *bufferedRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}

func (r *bufferedRows) ColumnTypeDatabaseTypeName(i int) string {
	if i < len(r.types) {
		return r.types[i]
	}
	return ""
}
