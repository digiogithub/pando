package code

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// insertGraphSymbol inserts a code_symbols row into a migrated DB (which has no
// source_code column), for graph tests.
func insertGraphSymbol(t *testing.T, db *sql.DB, fileID int64, projectID, filePath, id, namePath, symbolType, name string, now time.Time) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO code_symbols (
			id, project_id, file_id, file_path, language, symbol_type, name, name_path,
			start_line, end_line, start_byte, end_byte, signature, doc_string,
			parent_id, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'go', ?, ?, ?, 1, 10, 1, 100, '', '', NULL, '{}', ?, ?)`,
		id, projectID, fileID, filePath, symbolType, name, namePath, now, now,
	); err != nil {
		t.Fatalf("insert graph symbol %s: %v", id, err)
	}
}

// insertEdgeRow inserts a single code_edges row for tests.
func insertEdgeRow(t *testing.T, db *sql.DB, projectID string, fileID int64, edgeType, srcFile, srcSymbol, dstName, dstPath string, line int) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO code_edges (project_id, file_id, edge_type, src_file, src_symbol, dst_name, dst_path, start_line)
		VALUES (?,?,?,?,?,?,?,?)`,
		projectID, fileID, edgeType, srcFile, srcSymbol, dstName, dstPath, line,
	); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
}

func TestImpactAnalysis(t *testing.T) {
	db := setupMigratedDB(t)
	idx := NewCodeIndexer(db, nil, 1)
	ctx := context.Background()
	now := time.Now().UTC()

	insertTestCodeProject(t, db, "p1", "/p1", now)
	aID := insertTestCodeFile(t, db, "p1", "a.go", now)
	bID := insertTestCodeFile(t, db, "p1", "b.go", now)
	cID := insertTestCodeFile(t, db, "p1", "c.go", now)

	// helper defined in a.go; caller (b.go) calls helper; caller2 (c.go) calls caller.
	insertGraphSymbol(t, db, aID, "p1", "a.go", "sym-helper", "/helper", "function", "helper", now)
	insertGraphSymbol(t, db, bID, "p1", "b.go", "sym-caller", "/caller", "function", "caller", now)
	insertGraphSymbol(t, db, cID, "p1", "c.go", "sym-caller2", "/caller2", "function", "caller2", now)

	insertEdgeRow(t, db, "p1", bID, "calls", "b.go", "sym-caller", "helper", "", 10)
	insertEdgeRow(t, db, "p1", cID, "calls", "c.go", "sym-caller2", "caller", "", 20)

	// Depth 1: only direct caller.
	res, err := idx.ImpactAnalysis(ctx, "p1", "helper", 1, 50)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(res.Callers) != 1 || res.Callers[0].Name != "caller" {
		t.Fatalf("depth-1 expected [caller], got %+v", res.Callers)
	}

	// Depth 2: transitive caller2.
	res, err = idx.ImpactAnalysis(ctx, "p1", "helper", 2, 50)
	if err != nil {
		t.Fatalf("impact depth2: %v", err)
	}
	names := map[string]int{}
	for _, c := range res.Callers {
		names[c.Name] = c.Depth
	}
	if names["caller"] != 1 || names["caller2"] != 2 {
		t.Fatalf("depth-2 expected caller@1 and caller2@2, got %+v", res.Callers)
	}

	// Unknown symbol → empty.
	res, err = idx.ImpactAnalysis(ctx, "p1", "nope", 2, 50)
	if err != nil {
		t.Fatalf("impact unknown: %v", err)
	}
	if len(res.Callers) != 0 {
		t.Fatalf("unknown symbol should have no callers, got %+v", res.Callers)
	}
}

func TestRelatedFiles(t *testing.T) {
	db := setupMigratedDB(t)
	idx := NewCodeIndexer(db, nil, 1)
	ctx := context.Background()
	now := time.Now().UTC()

	insertTestCodeProject(t, db, "p1", "/p1", now)
	runID := insertTestCodeFile(t, db, "p1", "src/run.ts", now)
	fooID := insertTestCodeFile(t, db, "p1", "src/foo.ts", now)

	// foo.ts defines doThing (method) and a class Foo.
	insertGraphSymbol(t, db, fooID, "p1", "src/foo.ts", "sym-dothing", "/Foo/doThing", "method", "doThing", now)
	// run.ts defines run().
	insertGraphSymbol(t, db, runID, "p1", "src/run.ts", "sym-run", "/run", "function", "run", now)

	// run.ts imports ./foo (resolves to src/foo.ts) and calls doThing.
	insertEdgeRow(t, db, "p1", runID, "imports", "src/run.ts", "", "", "./foo", 1)
	insertEdgeRow(t, db, "p1", runID, "calls", "src/run.ts", "sym-run", "doThing", "", 5)

	res, err := idx.RelatedFiles(ctx, "p1", "src/run.ts", 20)
	if err != nil {
		t.Fatalf("related: %v", err)
	}
	if len(res.Related) != 1 {
		t.Fatalf("expected 1 related file, got %+v", res.Related)
	}
	rf := res.Related[0]
	if rf.FilePath != "src/foo.ts" {
		t.Fatalf("expected src/foo.ts, got %s", rf.FilePath)
	}
	// Both imports (1.0) and calls (0.8) reasons must be present.
	gotImports, gotCalls := false, false
	for _, r := range rf.Reasons {
		if r == "imports" {
			gotImports = true
		}
		if r == "calls" {
			gotCalls = true
		}
	}
	if !gotImports || !gotCalls {
		t.Fatalf("expected imports+calls reasons, got %+v", rf.Reasons)
	}
	if rf.Score < 1.8 {
		t.Fatalf("expected score >= 1.8 (imports+calls), got %.2f", rf.Score)
	}

	// Reverse direction: foo.ts is related to run.ts via incoming calls.
	rev, err := idx.RelatedFiles(ctx, "p1", "src/foo.ts", 20)
	if err != nil {
		t.Fatalf("related rev: %v", err)
	}
	if len(rev.Related) != 1 || rev.Related[0].FilePath != "src/run.ts" {
		t.Fatalf("expected reverse relation to src/run.ts, got %+v", rev.Related)
	}
}

func TestRelatedFilesEmpty(t *testing.T) {
	db := setupMigratedDB(t)
	idx := NewCodeIndexer(db, nil, 1)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestCodeProject(t, db, "p1", "/p1", now)
	insertTestCodeFile(t, db, "p1", "lonely.go", now)

	res, err := idx.RelatedFiles(ctx, "p1", "lonely.go", 20)
	if err != nil {
		t.Fatalf("related empty: %v", err)
	}
	if len(res.Related) != 0 {
		t.Fatalf("expected no related files, got %+v", res.Related)
	}
}
