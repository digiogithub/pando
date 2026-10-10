//go:build unix

package db

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// Multi-process tests re-exec the test binary: TestMain dispatches the child
// role named by childRoleEnv instead of running the tests.
const (
	childRoleEnv = "PANDO_DBTEST_ROLE"
	childDBEnv   = "PANDO_DBTEST_DB"
	childIDEnv   = "PANDO_DBTEST_ID"
	childNEnv    = "PANDO_DBTEST_N"
	childOutEnv  = "PANDO_DBTEST_OUT"  // result / ack file
	childSyncEnv = "PANDO_DBTEST_SYNC" // directory for ready/stop marker files
	childDurEnv  = "PANDO_DBTEST_DUR"  // run time for the loop role
	childPadEnv  = "PANDO_DBTEST_PAD"  // extra KB chunk bytes per loop operation
)

func TestMain(m *testing.M) {
	goose.SetLogger(goose.NopLogger())
	if role := os.Getenv(childRoleEnv); role != "" {
		os.Exit(runChild(role))
	}
	os.Exit(m.Run())
}

// childResult is what a "work" child acknowledges: every count is of rows it
// committed (and saw the commit return for).
type childResult struct {
	Sessions int `json:"sessions"`
	Messages int `json:"messages"`
	KBDocs   int `json:"kb_docs"`
	KBChunks int `json:"kb_chunks"`
	Events   int `json:"events"`
	Counter  int `json:"counter"`
}

func runChild(role string) int {
	path := os.Getenv(childDBEnv)
	id, _ := strconv.Atoi(os.Getenv(childIDEnv))
	n, _ := strconv.Atoi(os.Getenv(childNEnv))
	fail := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, "child %d (%s): %s\n", id, role, fmt.Sprintf(format, args...))
		return 3
	}
	conn, err := Open(path)
	if err != nil {
		return fail("open: %v", err)
	}
	ctx := context.Background()
	switch role {
	case "open":
		// First start: Open (migrations) plus one write, then a clean close.
		if _, err := New(conn).CreateSession(ctx, CreateSessionParams{ID: fmt.Sprintf("c%d-first", id), Title: "first"}); err != nil {
			return fail("create session: %v", err)
		}
	case "work":
		res, err := childWork(ctx, conn, id, n)
		if err != nil {
			return fail("%v (after %+v)", err, res)
		}
		b, _ := json.Marshal(res)
		if err := os.WriteFile(os.Getenv(childOutEnv), b, 0o644); err != nil {
			return fail("write result: %v", err)
		}
	case "loop":
		// Writes until the deadline (or SIGKILL), acknowledging every
		// committed operation with a line in the ack file. With n > 0 it stops
		// writing after n operations but keeps the database open until the
		// deadline, so a kill still finds it in use.
		ack, err := os.OpenFile(os.Getenv(childOutEnv), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err != nil {
			return fail("ack file: %v", err)
		}
		dur, _ := time.ParseDuration(os.Getenv(childDurEnv))
		pad, _ := strconv.Atoi(os.Getenv(childPadEnv))
		deadline := time.Now().Add(dur)
		for i := 0; time.Now().Before(deadline); i++ {
			if n > 0 && i >= n {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if err := loopOp(ctx, conn, id, i, pad); err != nil {
				return fail("op %d: %v", i, err)
			}
			if _, err := fmt.Fprintf(ack, "%d\n", i); err != nil {
				return fail("ack: %v", err)
			}
		}
		_ = ack.Close()
	case "hold":
		// Writes n rows, signals readiness and keeps the database open until
		// the stop marker appears (or the parent kills it).
		for i := 0; i < n; i++ {
			if _, err := conn.Exec("INSERT INTO hold_rows (child, v) VALUES (?, ?)", id, i); err != nil {
				return fail("insert: %v", err)
			}
		}
		sync := os.Getenv(childSyncEnv)
		if err := os.WriteFile(filepath.Join(sync, fmt.Sprintf("ready-%d", id)), nil, 0o644); err != nil {
			return fail("ready marker: %v", err)
		}
		stop := filepath.Join(sync, fmt.Sprintf("stop-%d", id))
		for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(stop); err == nil {
				break
			}
		}
	default:
		return fail("unknown role")
	}
	if err := Close(conn); err != nil {
		return fail("close: %v", err)
	}
	return 0
}

// childWork mixes the writes a Pando process does: sessions and messages through
// the sqlc querier (autocommit, RETURNING), KB documents/chunks and events with
// their FTS5 indexes in explicit transactions, and a read-modify-write of a
// counter every process shares.
func childWork(ctx context.Context, conn *sql.DB, id, n int) (childResult, error) {
	var res childResult
	q := New(conn)
	for i := 0; i < n; i++ {
		sid := fmt.Sprintf("c%d-s%d", id, i)
		if _, err := q.CreateSession(ctx, CreateSessionParams{ID: sid, Title: "session " + sid}); err != nil {
			return res, fmt.Errorf("CreateSession: %w", err)
		}
		res.Sessions++
		for m := 0; m < 2; m++ {
			if _, err := q.CreateMessage(ctx, CreateMessageParams{
				ID: fmt.Sprintf("%s-m%d", sid, m), SessionID: sid, Role: "user",
				Parts: fmt.Sprintf(`[{"type":"text","data":{"text":"hello %d"}}]`, m),
			}); err != nil {
				return res, fmt.Errorf("CreateMessage: %w", err)
			}
			res.Messages++
		}
		if _, err := q.UpdateSession(ctx, UpdateSessionParams{ID: sid, Title: "renamed " + sid, PromptTokens: int64(i), Cost: 0.5}); err != nil {
			return res, fmt.Errorf("UpdateSession: %w", err)
		}

		if err := RunTx(ctx, conn, func(tx *sql.Tx) error {
			return insertKBDoc(tx, fmt.Sprintf("c%d/doc%d.md", id, i), 2)
		}); err != nil {
			return res, fmt.Errorf("kb insert: %w", err)
		}
		res.KBDocs++
		res.KBChunks += 2
		if i%5 == 4 { // delete an older document, keeping kb_fts in sync
			if err := RunTx(ctx, conn, func(tx *sql.Tx) error {
				return deleteKBDoc(tx, fmt.Sprintf("c%d/doc%d.md", id, i-2))
			}); err != nil {
				return res, fmt.Errorf("kb delete: %w", err)
			}
			res.KBDocs--
			res.KBChunks -= 2
		}

		if err := RunTx(ctx, conn, func(tx *sql.Tx) error {
			return insertEvent(tx, fmt.Sprintf("c%d", id), fmt.Sprintf("child %d event %d deploy finished", id, i))
		}); err != nil {
			return res, fmt.Errorf("event: %w", err)
		}
		res.Events++

		if err := RunTx(ctx, conn, func(tx *sql.Tx) error {
			var v int
			if err := tx.QueryRow("SELECT n FROM shared_counter WHERE id = 1").Scan(&v); err != nil {
				return err
			}
			_, err := tx.Exec("UPDATE shared_counter SET n = ? WHERE id = 1", v+1)
			return err
		}); err != nil {
			return res, fmt.Errorf("counter: %w", err)
		}
		res.Counter++
	}
	return res, nil
}

func insertKBDoc(tx *sql.Tx, path string, chunks int) error {
	var docID int64
	if err := tx.QueryRow("INSERT INTO kb_documents (file_path, content) VALUES (?, ?) RETURNING id",
		path, "document "+path).Scan(&docID); err != nil {
		return err
	}
	for c := 0; c < chunks; c++ {
		text := fmt.Sprintf("chunk %d of %s about multiwriter sqlite engines", c, path)
		r, err := tx.Exec("INSERT INTO kb_chunks (document_id, chunk_index, content, embedding) VALUES (?, ?, ?, zeroblob(16))",
			docID, c, text)
		if err != nil {
			return err
		}
		rowid, _ := r.LastInsertId()
		if _, err := tx.Exec("INSERT INTO kb_fts (rowid, content) VALUES (?, ?)", rowid, text); err != nil {
			return err
		}
	}
	return nil
}

func deleteKBDoc(tx *sql.Tx, path string) error {
	if _, err := tx.Exec(`INSERT INTO kb_fts (kb_fts, rowid, content)
		SELECT 'delete', c.id, c.content FROM kb_chunks c JOIN kb_documents d ON d.id = c.document_id WHERE d.file_path = ?`, path); err != nil {
		return err
	}
	r, err := tx.Exec("DELETE FROM kb_documents WHERE file_path = ?", path) // chunks cascade
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return fmt.Errorf("deleted %d documents for %s", n, path)
	}
	return nil
}

func insertEvent(tx *sql.Tx, subject, content string) error {
	r, err := tx.Exec("INSERT INTO events (subject, content, embedding) VALUES (?, ?, zeroblob(16))", subject, content)
	if err != nil {
		return err
	}
	rowid, _ := r.LastInsertId()
	_, err = tx.Exec("INSERT INTO events_fts (rowid, subject, content) VALUES (?, ?, ?)", rowid, subject, content)
	return err
}

// loopOp is one acknowledged operation of the crash test: an event and a KB
// document in one transaction, then a session through the querier.
func loopOp(ctx context.Context, conn *sql.DB, id, i, pad int) error {
	if err := RunTx(ctx, conn, func(tx *sql.Tx) error {
		if err := insertEvent(tx, fmt.Sprintf("c%d", id), fmt.Sprintf("op %d", i)); err != nil {
			return err
		}
		if pad > 0 {
			// Bulk that only grows the log (a large document body).
			if _, err := tx.Exec("INSERT INTO kb_documents (file_path, content) VALUES (?, ?)",
				fmt.Sprintf("pad/c%d/op%d", id, i), strings.Repeat(fmt.Sprintf("%d-%d ", id, i), pad/8)); err != nil {
				return err
			}
		}
		return insertKBDoc(tx, fmt.Sprintf("c%d/op%d.md", id, i), 1)
	}); err != nil {
		return err
	}
	_, err := New(conn).CreateSession(ctx, CreateSessionParams{ID: fmt.Sprintf("c%d-op%d", id, i), Title: "op"})
	return err
}

// --- parent helpers ---------------------------------------------------------

func spawnChild(t *testing.T, role, path string, id, n int, extra ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		childRoleEnv+"="+role, childDBEnv+"="+path,
		fmt.Sprintf("%s=%d", childIDEnv, id), fmt.Sprintf("%s=%d", childNEnv, n))
	cmd.Env = append(cmd.Env, extra...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd
}

func waitChild(t *testing.T, cmd *exec.Cmd, name string) {
	t.Helper()
	if err := cmd.Wait(); err != nil {
		t.Errorf("%s: %v", name, err)
	}
}

func migrationCount(t *testing.T) (int, int64) {
	t.Helper()
	ents, err := fs.ReadDir(FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^(\d+)_.*\.sql$`)
	var count int
	var last int64
	for _, e := range ents {
		m := re.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		count++
		v, _ := strconv.ParseInt(m[1], 10, 64)
		if v > last {
			last = v
		}
	}
	return count, last
}

// checkGoose verifies every migration is recorded exactly once.
func checkGoose(t *testing.T, conn *sql.DB) {
	t.Helper()
	want, last := migrationCount(t)
	var rows, distinct int
	var maxV int64
	if err := conn.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT version_id), COALESCE(MAX(version_id), 0)
		FROM goose_db_version WHERE version_id > 0 AND is_applied`).Scan(&rows, &distinct, &maxV); err != nil {
		t.Fatal(err)
	}
	if rows != want || distinct != want || maxV != last {
		t.Fatalf("goose_db_version: %d applied rows, %d distinct, max %d; want %d migrations up to %d", rows, distinct, maxV, want, last)
	}
}

// checkIntegrity runs SQLite's and the FTS5 indexes' own integrity checks.
func checkIntegrity(t *testing.T, conn *sql.DB) {
	t.Helper()
	rows, err := conn.Query("PRAGMA integrity_check")
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, s)
	}
	rows.Close()
	if len(msgs) != 1 || msgs[0] != "ok" {
		t.Fatalf("integrity_check: %v", msgs)
	}
	var fk int
	if err := conn.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 0 {
		t.Fatalf("foreign_key_check: %d violations", fk)
	}
	for _, fts := range []string{"kb_fts", "events_fts", "code_symbols_fts"} {
		if _, err := conn.Exec(fmt.Sprintf("INSERT INTO %s(%s) VALUES('integrity-check')", fts, fts)); err != nil {
			t.Fatalf("%s integrity-check: %v", fts, err)
		}
	}
}

func countRows(t *testing.T, conn *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// checkNoLog verifies the multiwriter log was compacted away by the last close.
func checkNoLog(t *testing.T, path string) {
	t.Helper()
	left, _ := filepath.Glob(path + "-mw*")
	var bad []string
	for _, f := range left {
		if strings.HasSuffix(f, "-mwlock") { // the lock file may stay, like <db>.lock
			continue
		}
		if strings.HasSuffix(f, ".prep.tmp") {
			// go-sqlite-multiwriter v0.1.0 leak: a process killed while it
			// prefilled the next segment leaves <db>-mw.N.prep.tmp (32 MiB);
			// the last close only removes *.prep. Harmless to the data.
			t.Logf("leaked segment preparation file: %s", filepath.Base(f))
			continue
		}
		bad = append(bad, filepath.Base(f))
	}
	if len(bad) > 0 {
		t.Fatalf("multiwriter log files left after the last close: %v", bad)
	}
}

func skipNonMultiwriter(t *testing.T) {
	t.Helper()
	if SelectedEngine() != EngineMultiwriter {
		t.Skipf("engine is %s, not multiwriter", SelectedEngine())
	}
}

// --- tests ------------------------------------------------------------------

// Four processes open a migrated database and write it concurrently.
func TestMultiProcessConcurrentWriters(t *testing.T) {
	skipNonMultiwriter(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pando.db")
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mustExecT(t, conn, "CREATE TABLE shared_counter (id INTEGER PRIMARY KEY, n INT NOT NULL)")
	mustExecT(t, conn, "INSERT INTO shared_counter VALUES (1, 0)")
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}

	const children, iters = 4, 25
	start := time.Now()
	cmds := make([]*exec.Cmd, children)
	for c := 0; c < children; c++ {
		cmds[c] = spawnChild(t, "work", path, c, iters, childOutEnv+"="+filepath.Join(dir, fmt.Sprintf("res-%d.json", c)))
	}
	for c, cmd := range cmds {
		waitChild(t, cmd, fmt.Sprintf("child %d", c))
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Logf("%d children x %d iterations in %v", children, iters, time.Since(start).Round(time.Millisecond))

	conn, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkIntegrity(t, conn)
	checkGoose(t, conn)
	total := 0
	for c := 0; c < children; c++ {
		b, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("res-%d.json", c)))
		if err != nil {
			t.Fatal(err)
		}
		var r childResult
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		got := childResult{
			Sessions: countRows(t, conn, "SELECT COUNT(*) FROM sessions WHERE id LIKE ?", fmt.Sprintf("c%d-s%%", c)),
			Messages: countRows(t, conn, "SELECT COUNT(*) FROM messages WHERE session_id LIKE ?", fmt.Sprintf("c%d-s%%", c)),
			KBDocs:   countRows(t, conn, "SELECT COUNT(*) FROM kb_documents WHERE file_path LIKE ?", fmt.Sprintf("c%d/%%", c)),
			KBChunks: countRows(t, conn, "SELECT COUNT(*) FROM kb_chunks k JOIN kb_documents d ON d.id = k.document_id WHERE d.file_path LIKE ?", fmt.Sprintf("c%d/%%", c)),
			Events:   countRows(t, conn, "SELECT COUNT(*) FROM events WHERE subject = ?", fmt.Sprintf("c%d", c)),
			Counter:  r.Counter,
		}
		if got != r {
			t.Errorf("child %d: database has %+v, child acknowledged %+v", c, got, r)
		}
		// Message-count triggers and UpdateSession must agree with the rows.
		if bad := countRows(t, conn, "SELECT COUNT(*) FROM sessions WHERE id LIKE ? AND (message_count != 2 OR title NOT LIKE 'renamed %')", fmt.Sprintf("c%d-s%%", c)); bad != 0 {
			t.Errorf("child %d: %d sessions with a wrong message_count or title", c, bad)
		}
		total += r.Counter
	}
	if n := countRows(t, conn, "SELECT n FROM shared_counter WHERE id = 1"); n != total {
		t.Errorf("shared counter = %d, want %d (lost updates)", n, total)
	}
	if n := countRows(t, conn, "SELECT COUNT(*) FROM kb_fts WHERE kb_fts MATCH 'multiwriter'"); n != countRows(t, conn, "SELECT COUNT(*) FROM kb_chunks") {
		t.Errorf("kb_fts matches %d chunks, kb_chunks has %d", n, countRows(t, conn, "SELECT COUNT(*) FROM kb_chunks"))
	}
	if n := countRows(t, conn, "SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'deploy'"); n != children*iters {
		t.Errorf("events_fts matches %d events, want %d", n, children*iters)
	}
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}
	checkNoLog(t, path)
}

// Four processes start against a brand-new path at the same time: every one
// opens it, and each migration is applied exactly once.
func TestFirstStartConcurrentMigrations(t *testing.T) {
	skipNonMultiwriter(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pando.db")
	const children = 4
	cmds := make([]*exec.Cmd, children)
	for c := 0; c < children; c++ {
		cmds[c] = spawnChild(t, "open", path, c, 0)
	}
	for c, cmd := range cmds {
		waitChild(t, cmd, fmt.Sprintf("child %d", c))
	}
	if t.Failed() {
		t.FailNow()
	}
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	checkGoose(t, conn)
	checkIntegrity(t, conn)
	if n := countRows(t, conn, "SELECT COUNT(*) FROM sessions WHERE id LIKE 'c%-first'"); n != children {
		t.Errorf("first-start sessions = %d, want %d", n, children)
	}
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}
	checkNoLog(t, path)
}

// Writers are SIGKILLed mid-run: every acknowledged commit survives and the
// database and its FTS indexes stay consistent. Round 0 kills one of three
// writers (the others carry on and close); round 1 kills all three, so the next
// opener alone recovers the log. Round 1 is capped so its log stays within one
// segment (see TestCrashKilledWritersMultiSegment).
func TestCrashKilledWriters(t *testing.T) {
	skipNonMultiwriter(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "pando.db")
	initDB(t, path)

	var all []crashProc
	round := spawnLoopers(t, dir, path, 0, 3, 0, 0, "3s")
	waitAcks(t, round, 1)
	time.Sleep(500 * time.Millisecond)
	if err := round[0].cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = round[0].cmd.Wait()
	for _, p := range round[1:] {
		waitChild(t, p.cmd, fmt.Sprintf("child %d", p.id))
	}
	all = append(all, round...)

	round = spawnLoopers(t, dir, path, 3, 3, 0, 0, "30s")
	waitAcks(t, round, 1)
	time.Sleep(700 * time.Millisecond)
	killAll(round)
	all = append(all, round...)
	if t.Failed() {
		t.FailNow()
	}
	verifyCrash(t, path, all)
}

// All writers are killed while the live log spans more than one segment.
//
// Regression test for go-sqlite-multiwriter v0.1.0, which lost acknowledged
// commits here: wlog.replay (log.go) treated the zero padding at the end of a
// full, prefilled multi-process segment as a torn tail and deleted every later
// segment. Fixed in v0.1.1.
func TestCrashKilledWritersMultiSegment(t *testing.T) {
	skipNonMultiwriter(t)
	// Kill while the writers are still going (live processes compact the log
	// when idle) and after ~3 x 40 ops x 512 KiB, so the 32 MiB segment has
	// rolled. Compaction can still leave a single live segment: retry a few
	// times until the kill catches two.
	for attempt := 1; attempt <= 6; attempt++ {
		dir := t.TempDir()
		path := filepath.Join(dir, "pando.db")
		initDB(t, path)
		round := spawnLoopers(t, dir, path, 0, 3, 0, 512<<10, "60s")
		waitAcks(t, round, 60)
		segs, _ := filepath.Glob(path + "-mw.[0-9][0-9][0-9][0-9][0-9][0-9]")
		killAll(round)
		t.Logf("attempt %d: %d live log segments at the kill", attempt, len(segs))
		if len(segs) < 2 && attempt < 6 {
			continue
		}
		verifyCrash(t, path, round)
		return
	}
}

type crashProc struct {
	cmd *exec.Cmd
	id  int
	ack string
}

func initDB(t *testing.T, path string) {
	t.Helper()
	conn, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}
}

func spawnLoopers(t *testing.T, dir, path string, first, count, maxOps, pad int, dur string) []crashProc {
	t.Helper()
	var out []crashProc
	for c := first; c < first+count; c++ {
		p := crashProc{id: c, ack: filepath.Join(dir, fmt.Sprintf("ack-%d", c))}
		p.cmd = spawnChild(t, "loop", path, c, maxOps,
			childOutEnv+"="+p.ack, childDurEnv+"="+dur, fmt.Sprintf("%s=%d", childPadEnv, pad))
		out = append(out, p)
	}
	return out
}

// waitAcks waits (bounded; slow machines start children slowly) until every
// process acknowledged at least min operations.
func waitAcks(t *testing.T, ps []crashProc, min int) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		done := true
		for _, p := range ps {
			if len(readAcks(t, p.ack)) < min {
				done = false
				break
			}
		}
		if done {
			return
		}
	}
	t.Logf("not every writer reached %d acknowledged ops in 30s", min)
}

func killAll(ps []crashProc) {
	for _, p := range ps {
		_ = p.cmd.Process.Signal(syscall.SIGKILL)
	}
	for _, p := range ps {
		_ = p.cmd.Wait()
	}
}

func verifyCrash(t *testing.T, path string, all []crashProc) {
	t.Helper()
	conn, err := Open(path)
	if err != nil {
		t.Fatalf("open after crash: %v", err)
	}
	checkIntegrity(t, conn)
	checkGoose(t, conn)
	for _, p := range all {
		acked := readAcks(t, p.ack)
		if len(acked) == 0 {
			t.Errorf("child %d acknowledged nothing", p.id)
			continue
		}
		events := countRows(t, conn, "SELECT COUNT(*) FROM events WHERE subject = ?", fmt.Sprintf("c%d", p.id))
		docs := countRows(t, conn, "SELECT COUNT(*) FROM kb_documents WHERE file_path LIKE ?", fmt.Sprintf("c%d/op%%", p.id))
		sessions := countRows(t, conn, "SELECT COUNT(*) FROM sessions WHERE id LIKE ?", fmt.Sprintf("c%d-op%%", p.id))
		if events < len(acked) || docs < len(acked) || sessions < len(acked) {
			t.Errorf("child %d: %d acked ops but %d events, %d docs, %d sessions", p.id, len(acked), events, docs, sessions)
		}
		// The transaction is atomic: an event never exists without its document.
		if events != docs {
			t.Errorf("child %d: %d events but %d documents (torn transaction)", p.id, events, docs)
		}
		for _, i := range acked {
			if n := countRows(t, conn, `SELECT (SELECT COUNT(*) FROM events WHERE subject = ? AND content = ?)
				+ (SELECT COUNT(*) FROM sessions WHERE id = ?)`,
				fmt.Sprintf("c%d", p.id), fmt.Sprintf("op %d", i), fmt.Sprintf("c%d-op%d", p.id, i)); n != 2 {
				t.Errorf("child %d: acknowledged op %d is missing", p.id, i)
				break
			}
		}
		t.Logf("child %d: %d acked, %d events", p.id, len(acked), events)
	}
	if err := Close(conn); err != nil {
		t.Fatal(err)
	}
	checkNoLog(t, path)
}

func readAcks(t *testing.T, path string) []int {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// A SIGKILL can cut the last line short; only whole lines count.
		if v, err := strconv.Atoi(sc.Text()); err == nil {
			out = append(out, v)
		}
	}
	return out
}
