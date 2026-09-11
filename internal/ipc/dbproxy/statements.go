// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// Registered write statements.
//
// Some writers do not go through sqlc (the design store, the MCP gateway's
// tool registry and usage stats, the AG-UI thread map): they hold a *sql.DB
// and run their own SQL. On an IPC secondary that pool is the 1-connection,
// 200 ms busy-timeout pool, so without help those writes fail as soon as the
// primary holds the write lock a little longer than 200 ms.
//
// SQLWriter gives them the same contract DBProxy gives sqlc writes: execute
// directly on the local pool first and, on SQLITE_BUSY/LOCKED, forward the
// write to the primary's write coordinator. Only a statement *name* and its
// arguments cross the IPC boundary: the primary looks the SQL up in its own
// registry (populated by the same package-level RegisterStatement calls), so
// a peer can never make the primary run arbitrary SQL. A batch of calls is
// executed in one transaction on whichever side runs it, which is how a
// multi-statement write (e.g. design's AddVersion) keeps its atomicity
// across the proxy.

// MethodExecStatements is the db.write method carrying registered statements.
const MethodExecStatements = "ExecStatements"

// Statement is a write statement registered under a stable name.
type Statement struct {
	name  string
	query string
}

// Name returns the registered name.
func (s Statement) Name() string { return s.name }

// Query returns the SQL text.
func (s Statement) Query() string { return s.query }

// With binds arguments to the statement for SQLWriter.ExecBatch.
func (s Statement) With(args ...any) StmtCall { return StmtCall{Stmt: s, Args: args} }

// StmtCall is one statement execution in a batch.
type StmtCall struct {
	Stmt Statement
	Args []any
}

var statementRegistry sync.Map // name -> query

// RegisterStatement registers query under name and returns the handle writers
// execute. Call it from package-level var initialisers so every instance of
// the binary has the same registry. Registering the same name twice with the
// same SQL is harmless; with different SQL it panics (a programming error).
func RegisterStatement(name, query string) Statement {
	if name == "" || strings.TrimSpace(query) == "" {
		panic("dbproxy: RegisterStatement needs a name and a query")
	}
	if prev, loaded := statementRegistry.LoadOrStore(name, query); loaded && prev.(string) != query {
		panic(fmt.Sprintf("dbproxy: statement %q registered twice with different SQL", name))
	}
	return Statement{name: name, query: query}
}

func lookupStatement(name string) (string, bool) {
	q, ok := statementRegistry.Load(name)
	if !ok {
		return "", false
	}
	return q.(string), true
}

// ---- primary side ----

var statementExecutor atomic.Pointer[sql.DB]

// RegisterStatementExecutor sets the pool the primary executes forwarded
// statements on (its read-write pool). A primary registers it when it wires
// its bus, a promoted secondary before starting its bus; pass nil to clear.
// Without it, ExecStatements answers "unknown write method", which makes the
// forwarding secondary fall back to a bounded direct retry.
func RegisterStatementExecutor(db *sql.DB) {
	statementExecutor.Store(db)
}

func dispatchExecStatements(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	conn := statementExecutor.Load()
	if conn == nil {
		return nil, &WriteError{
			Code:    ErrCodeMethodNotFound,
			Method:  MethodExecStatements,
			Message: fmt.Sprintf("unknown write method %q: no statement executor on this primary", MethodExecStatements),
		}
	}
	var req execStatementsRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, invalidParamsErr(MethodExecStatements, err)
	}
	calls, err := decodeStatementCalls(req)
	if err != nil {
		return nil, err
	}
	affected, err := execStatementsLocal(ctx, conn, calls)
	if err != nil {
		return nil, mapToWriteError(MethodExecStatements, err)
	}
	return json.Marshal(execStatementsResult{RowsAffected: affected})
}

// execStatementsLocal runs calls on conn: a single call as one autocommit
// statement, several in one transaction (BEGIN IMMEDIATE on Pando pools).
// It returns the rows affected by each call.
func execStatementsLocal(ctx context.Context, conn *sql.DB, calls []StmtCall) ([]int64, error) {
	switch len(calls) {
	case 0:
		return nil, nil
	case 1:
		res, err := conn.ExecContext(ctx, calls[0].Stmt.query, calls[0].Args...)
		if err != nil {
			return nil, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, err
		}
		return []int64{n}, nil
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	prepared := make(map[string]*sql.Stmt)
	defer func() {
		for _, st := range prepared {
			_ = st.Close()
		}
	}()
	affected := make([]int64, len(calls))
	for i, c := range calls {
		st, ok := prepared[c.Stmt.name]
		if !ok {
			st, err = tx.PrepareContext(ctx, c.Stmt.query)
			if err != nil {
				return nil, err
			}
			prepared[c.Stmt.name] = st
		}
		res, err := st.ExecContext(ctx, c.Args...)
		if err != nil {
			return nil, err
		}
		if affected[i], err = res.RowsAffected(); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return affected, nil
}

// ---- secondary side ----

// SQLWriter executes registered write statements against db, forwarding them
// to the IPC primary on lock contention when its proxy is remote. With a nil
// or non-remote proxy (a primary, a promoted secondary, a CLI) it is exactly
// a direct ExecContext / transaction on db. Without an explicit proxy it uses
// the proxy db was bound to with BindPool, if any.
type SQLWriter struct {
	db    *sql.DB
	proxy atomic.Pointer[DBProxy]
}

// NewSQLWriter builds a writer over db; proxy may be nil.
func NewSQLWriter(db *sql.DB, proxy *DBProxy) *SQLWriter {
	w := &SQLWriter{db: db}
	w.SetProxy(proxy)
	return w
}

// SetProxy sets (or clears, with nil) the write proxy. Safe to call while
// other goroutines write.
func (w *SQLWriter) SetProxy(p *DBProxy) {
	w.proxy.Store(p)
}

// Exec executes one statement and returns the rows it affected.
func (w *SQLWriter) Exec(ctx context.Context, stmt Statement, args ...any) (int64, error) {
	affected, err := w.ExecBatch(ctx, stmt.With(args...))
	if err != nil {
		return 0, err
	}
	return affected[0], nil
}

// ExecBatch executes calls atomically (one transaction when there are
// several) and returns the rows affected by each.
func (w *SQLWriter) ExecBatch(ctx context.Context, calls ...StmtCall) ([]int64, error) {
	if w == nil || w.db == nil {
		return nil, errors.New("dbproxy: SQLWriter has no database")
	}
	if len(calls) == 0 {
		return nil, nil
	}
	p := w.proxy.Load()
	if p == nil {
		// No explicit proxy: use the one the pool was bound to by app.New,
		// if any, so writers built only from the shared *sql.DB (the AG-UI
		// thread map, the WebUI's MCP catalog fallback) follow the topology.
		p = ProxyForPool(w.db)
	}
	affected, err := execStatementsLocal(ctx, w.db, calls)
	if err == nil || !p.IsRemote() || !isLockError(err) {
		return affected, err
	}

	logging.Debug("dbproxy: direct statement write got lock contention, forwarding to primary",
		"statement", calls[0].Stmt.name, "calls", len(calls), "error", err)
	req, encErr := encodeStatementCalls(calls)
	if encErr != nil {
		return nil, fmt.Errorf("dbproxy: cannot forward %s after lock contention (%v): %w",
			calls[0].Stmt.name, encErr, err)
	}
	timeout := DefaultWriteTimeouts.Default
	if len(calls) > 1 {
		timeout = DefaultWriteTimeouts.Long
	}
	res, ferr := proxyWriteRetry[execStatementsResult](ctx, p, MethodExecStatements, req, timeout)
	switch {
	case errors.Is(ferr, ErrNotRemote):
		// Promoted between the direct attempt and the forward: this pool is
		// now the primary's writer (long busy timeout).
		return execStatementsLocal(ctx, w.db, calls)
	case IsMethodNotSupportedError(ferr):
		// A primary from an older binary (or one without an executor): keep
		// the pre-P5 direct write, with a bounded retry on lock errors.
		logging.Debug("dbproxy: primary cannot execute registered statements, retrying directly",
			"statement", calls[0].Stmt.name, "error", ferr)
		return execStatementsWithBusyRetry(ctx, w.db, calls)
	case ferr != nil:
		return nil, ferr
	}
	if len(res.RowsAffected) != len(calls) {
		return nil, fmt.Errorf("dbproxy: %s: primary returned %d results for %d statements",
			MethodExecStatements, len(res.RowsAffected), len(calls))
	}
	return res.RowsAffected, nil
}

// directBusyRetryAttempts/backoff bound the direct fallback used when the
// primary cannot execute forwarded statements: on the secondary's 200 ms
// pool this waits roughly 5 × 200 ms busy timeout + ~1.5 s of backoff.
const (
	directBusyRetryAttempts = 5
	directBusyRetryBackoff  = 100 * time.Millisecond
)

func execStatementsWithBusyRetry(ctx context.Context, conn *sql.DB, calls []StmtCall) ([]int64, error) {
	backoff := directBusyRetryBackoff
	var err error
	for attempt := 1; attempt <= directBusyRetryAttempts; attempt++ {
		var affected []int64
		affected, err = execStatementsLocal(ctx, conn, calls)
		if err == nil || !isLockError(err) || attempt == directBusyRetryAttempts {
			return affected, err
		}
		timer := time.NewTimer(jitterDuration(backoff))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, err
		case <-timer.C:
		}
		backoff *= 2
	}
	return nil, err
}

// ---- pool binding ----

var poolProxies sync.Map // *sql.DB -> *DBProxy

// BindPool records that writes on db belong to proxy's IPC topology, for
// components that only receive the *sql.DB (e.g. the AG-UI adapter, built by
// entrypoints that pass the shared pool). app.New binds the pool it is given
// to its DBProxy. A nil proxy removes the binding.
func BindPool(db *sql.DB, proxy *DBProxy) {
	if db == nil {
		return
	}
	if proxy == nil {
		poolProxies.Delete(db)
		return
	}
	poolProxies.Store(db, proxy)
}

// ProxyForPool returns the proxy bound to db with BindPool, or nil.
func ProxyForPool(db *sql.DB) *DBProxy {
	if db == nil {
		return nil
	}
	if p, ok := poolProxies.Load(db); ok {
		return p.(*DBProxy)
	}
	return nil
}

// ---- wire format ----

type execStatementsRequest struct {
	Statements []wireStatement `json:"statements"`
}

type wireStatement struct {
	Name string    `json:"name"`
	Args []wireArg `json:"args,omitempty"`
}

// wireArg carries one argument with its Go type, so the primary binds exactly
// what the secondary would have bound locally (an int stays an int, a
// time.Time stays a time.Time and is formatted by the driver the same way).
type wireArg struct {
	T string          `json:"t"`
	V json.RawMessage `json:"v,omitempty"`
}

type execStatementsResult struct {
	RowsAffected []int64 `json:"rows_affected"`
}

const (
	argNull  = "null"
	argStr   = "str"
	argBytes = "bytes"
	argBool  = "bool"
	argInt   = "int"
	argFloat = "float"
	argTime  = "time"
)

func encodeStatementCalls(calls []StmtCall) (execStatementsRequest, error) {
	req := execStatementsRequest{Statements: make([]wireStatement, len(calls))}
	for i, c := range calls {
		ws := wireStatement{Name: c.Stmt.name, Args: make([]wireArg, len(c.Args))}
		for j, a := range c.Args {
			wa, err := encodeArg(a, 0)
			if err != nil {
				return req, fmt.Errorf("statement %s arg %d: %w", c.Stmt.name, j, err)
			}
			ws.Args[j] = wa
		}
		req.Statements[i] = ws
	}
	return req, nil
}

func encodeArg(v any, depth int) (wireArg, error) {
	marshal := func(t string, x any) (wireArg, error) {
		raw, err := json.Marshal(x)
		if err != nil {
			return wireArg{}, err
		}
		return wireArg{T: t, V: raw}, nil
	}
	switch x := v.(type) {
	case nil:
		return wireArg{T: argNull}, nil
	case string:
		return marshal(argStr, x)
	case []byte:
		if x == nil {
			return wireArg{T: argNull}, nil
		}
		return marshal(argBytes, x)
	case bool:
		return marshal(argBool, x)
	case int:
		return marshal(argInt, int64(x))
	case int8:
		return marshal(argInt, int64(x))
	case int16:
		return marshal(argInt, int64(x))
	case int32:
		return marshal(argInt, int64(x))
	case int64:
		return marshal(argInt, x)
	case uint8:
		return marshal(argInt, int64(x))
	case uint16:
		return marshal(argInt, int64(x))
	case uint32:
		return marshal(argInt, int64(x))
	case uint:
		if uint64(x) > 1<<63-1 {
			return wireArg{}, fmt.Errorf("uint %d overflows int64", x)
		}
		return marshal(argInt, int64(x))
	case uint64:
		if x > 1<<63-1 {
			return wireArg{}, fmt.Errorf("uint64 %d overflows int64", x)
		}
		return marshal(argInt, int64(x))
	case float32:
		return marshal(argFloat, float64(x))
	case float64:
		return marshal(argFloat, x)
	case time.Time:
		return marshal(argTime, x)
	case driver.Valuer:
		if depth > 0 {
			return wireArg{}, fmt.Errorf("nested driver.Valuer %T", v)
		}
		val, err := x.Value()
		if err != nil {
			return wireArg{}, err
		}
		return encodeArg(val, depth+1)
	default:
		return wireArg{}, fmt.Errorf("unsupported argument type %T", v)
	}
}

func decodeStatementCalls(req execStatementsRequest) ([]StmtCall, error) {
	calls := make([]StmtCall, len(req.Statements))
	for i, ws := range req.Statements {
		query, ok := lookupStatement(ws.Name)
		if !ok {
			return nil, &WriteError{
				Code:    ErrCodeMethodNotFound,
				Method:  MethodExecStatements,
				Message: fmt.Sprintf("unknown write method %q: statement %q is not registered on this primary", MethodExecStatements, ws.Name),
			}
		}
		args := make([]any, len(ws.Args))
		for j, wa := range ws.Args {
			a, err := decodeArg(wa)
			if err != nil {
				return nil, invalidParamsErr(MethodExecStatements,
					fmt.Errorf("statement %s arg %d: %w", ws.Name, j, err))
			}
			args[j] = a
		}
		calls[i] = StmtCall{Stmt: Statement{name: ws.Name, query: query}, Args: args}
	}
	return calls, nil
}

func decodeArg(wa wireArg) (any, error) {
	switch wa.T {
	case argNull:
		return nil, nil
	case argStr:
		var s string
		err := json.Unmarshal(wa.V, &s)
		return s, err
	case argBytes:
		var b []byte
		err := json.Unmarshal(wa.V, &b)
		if b == nil {
			b = []byte{}
		}
		return b, err
	case argBool:
		var b bool
		err := json.Unmarshal(wa.V, &b)
		return b, err
	case argInt:
		n, err := strconv.ParseInt(string(wa.V), 10, 64)
		return n, err
	case argFloat:
		var f float64
		err := json.Unmarshal(wa.V, &f)
		return f, err
	case argTime:
		var t time.Time
		err := json.Unmarshal(wa.V, &t)
		return t, err
	default:
		return nil, fmt.Errorf("unknown argument type tag %q", wa.T)
	}
}
