package app

import (
	"context"
	"strings"
	"testing"
)

// TestCompactDatabaseNeedsExclusiveAccess verifies the /db-compact funnel: a
// running instance has the database open itself, so it cannot VACUUM it and
// must point the user at `pando db compact` (internal/db.CompactPath covers the
// compaction itself).
func TestCompactDatabaseNeedsExclusiveAccess(t *testing.T) {
	a := &App{}
	_, err := a.CompactDatabase(context.Background(), false, false)
	if err == nil {
		t.Fatal("expected CompactDatabase to refuse while the app has the database open")
	}
	if !strings.Contains(err.Error(), "pando db compact") {
		t.Fatalf("error does not explain how to compact: %v", err)
	}
}
