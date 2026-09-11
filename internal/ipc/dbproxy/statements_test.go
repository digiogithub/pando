// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package dbproxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/db"
)

var (
	testStmtInsert = RegisterStatement("dbproxy_test.insert", `INSERT INTO p5_items (id, n, at) VALUES (?, ?, ?)`)
	testStmtBad    = RegisterStatement("dbproxy_test.bad", `INSERT INTO p5_missing_table (id) VALUES (?)`)
	testStmtDelete = RegisterStatement("dbproxy_test.delete", `DELETE FROM p5_items WHERE id = ?`)
)

func openStatementsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.ConnectAt(filepath.Join(t.TempDir(), "stmts.db"))
	if err != nil {
		t.Fatalf("ConnectAt: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`CREATE TABLE p5_items (id TEXT PRIMARY KEY, n INTEGER, at TEXT)`); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestStatementArgsRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 12, 1, 2, 3, 456789000, time.UTC)
	zone := time.Date(2026, 9, 12, 1, 2, 3, 0, time.FixedZone("CEST", 2*3600))
	in := []any{nil, "s", []byte("b"), true, 7, int64(-9), uint32(5), 1.5, float32(2.5), now, zone,
		sql.NullInt64{Int64: 3, Valid: true}, sql.NullInt64{}, sql.NullString{String: "x", Valid: true}}
	req, err := encodeStatementCalls([]StmtCall{testStmtInsert.With(in...)})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var back execStatementsRequest
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	calls, err := decodeStatementCalls(back)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := calls[0].Args
	want := []any{nil, "s", []byte("b"), true, int64(7), int64(-9), int64(5), 1.5, 2.5, now, zone,
		int64(3), nil, "x"}
	if len(got) != len(want) {
		t.Fatalf("got %d args, want %d", len(got), len(want))
	}
	for i := range want {
		switch w := want[i].(type) {
		case time.Time:
			g, ok := got[i].(time.Time)
			if !ok || !g.Equal(w) || g.Format(time.RFC3339Nano) != w.Format(time.RFC3339Nano) {
				t.Errorf("arg %d: got %#v, want %v (same instant and same formatted text)", i, got[i], w)
			}
		case []byte:
			if string(got[i].([]byte)) != string(w) {
				t.Errorf("arg %d: got %q, want %q", i, got[i], w)
			}
		default:
			if got[i] != want[i] {
				t.Errorf("arg %d: got %#v (%T), want %#v (%T)", i, got[i], got[i], want[i], want[i])
			}
		}
	}
	if calls[0].Stmt.Query() != testStmtInsert.Query() {
		t.Errorf("query resolved from the registry by name: got %q", calls[0].Stmt.Query())
	}

	if _, err := encodeStatementCalls([]StmtCall{testStmtInsert.With(struct{}{})}); err == nil {
		t.Error("an unsupported argument type must fail to encode")
	}
	if _, err := encodeStatementCalls([]StmtCall{testStmtInsert.With(uint64(1 << 63))}); err == nil {
		t.Error("a uint64 overflowing int64 must fail to encode")
	}
}

func TestRegisterStatementConflictPanics(t *testing.T) {
	RegisterStatement("dbproxy_test.insert", testStmtInsert.Query()) // same SQL: fine
	defer func() {
		if recover() == nil {
			t.Error("re-registering a name with different SQL must panic")
		}
	}()
	RegisterStatement("dbproxy_test.insert", `DELETE FROM p5_items`)
}

// With no proxy (a primary, a CLI) the writer is a plain direct exec, and a
// batch is atomic.
func TestSQLWriterDirectAndAtomicBatch(t *testing.T) {
	conn := openStatementsTestDB(t)
	w := NewSQLWriter(conn, nil)
	ctx := context.Background()

	n, err := w.Exec(ctx, testStmtInsert, "a", 1, time.Now().UTC())
	if err != nil || n != 1 {
		t.Fatalf("Exec: n=%d err=%v", n, err)
	}
	affected, err := w.ExecBatch(ctx, testStmtInsert.With("b", 2, nil), testStmtDelete.With("a"), testStmtDelete.With("zzz"))
	if err != nil {
		t.Fatalf("ExecBatch: %v", err)
	}
	if len(affected) != 3 || affected[0] != 1 || affected[1] != 1 || affected[2] != 0 {
		t.Fatalf("rows affected = %v, want [1 1 0]", affected)
	}

	// A failing statement rolls the whole batch back.
	if _, err := w.ExecBatch(ctx, testStmtInsert.With("c", 3, nil), testStmtBad.With("x")); err == nil {
		t.Fatal("batch with a failing statement must fail")
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM p5_items WHERE id = 'c'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("batch not rolled back: count=%d err=%v", count, err)
	}
}

func TestDispatchExecStatementsErrors(t *testing.T) {
	t.Cleanup(func() { RegisterStatementExecutor(nil) })
	ctx := context.Background()
	params, _ := json.Marshal(execStatementsRequest{Statements: []wireStatement{{Name: "dbproxy_test.delete",
		Args: []wireArg{{T: argStr, V: json.RawMessage(`"a"`)}}}}})

	// No executor registered: "unknown write method", so an older/unwired
	// primary makes the secondary fall back to direct writes.
	RegisterStatementExecutor(nil)
	_, err := dispatchWrite(ctx, nil, WriteRequest{Method: MethodExecStatements, Params: params})
	if !IsMethodNotSupportedError(err) {
		t.Fatalf("no executor: got %v, want method-not-found", err)
	}
	// Round trip as text (what a secondary sees) keeps the classification.
	if werr := ClassifyError(MethodExecStatements, errors.New("ipc: RPC error -32000: "+err.Error())); werr.Code != ErrCodeMethodNotFound {
		t.Fatalf("text round trip: got %s", werr.Code)
	}

	conn := openStatementsTestDB(t)
	RegisterStatementExecutor(conn)
	unknown, _ := json.Marshal(execStatementsRequest{Statements: []wireStatement{{Name: "not.registered"}}})
	if _, err := dispatchWrite(ctx, nil, WriteRequest{Method: MethodExecStatements, Params: unknown}); !IsMethodNotSupportedError(err) {
		t.Fatalf("unknown statement: got %v, want method-not-found", err)
	}
	raw, err := dispatchWrite(ctx, nil, WriteRequest{Method: MethodExecStatements, Params: params})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	var res execStatementsResult
	if err := json.Unmarshal(raw, &res); err != nil || len(res.RowsAffected) != 1 || res.RowsAffected[0] != 0 {
		t.Fatalf("result = %s err=%v", raw, err)
	}
}

func TestPoolBinding(t *testing.T) {
	conn := openStatementsTestDB(t)
	p := New(db.New(conn), nil, "")
	BindPool(conn, p)
	t.Cleanup(func() { BindPool(conn, nil) })
	if ProxyForPool(conn) != p {
		t.Fatal("ProxyForPool must return the bound proxy")
	}
	BindPool(conn, nil)
	if ProxyForPool(conn) != nil || ProxyForPool(nil) != nil {
		t.Fatal("unbound pools have no proxy")
	}
}

func TestHandoverTextsClassifyAsRetryable(t *testing.T) {
	cases := map[string]WriteErrorCode{
		"ipc: RPC error -32000: writecoordinator: draining for primary handover, not accepting writes": ErrCodeUnavailable,
		"ipc: RPC error -32000: writecoordinator: coordinator is shut down":                            ErrCodeUnavailable,
		"ipc: RPC error -32000: writecoordinator: coordinator shut down while waiting for result":      ErrCodeTimeout,
	}
	for msg, want := range cases {
		werr := ClassifyError("X", errors.New(msg))
		if werr.Code != want || !werr.IsRetryable() {
			t.Errorf("%q: got %s (retryable=%v), want retryable %s", msg, werr.Code, werr.IsRetryable(), want)
		}
	}
}
