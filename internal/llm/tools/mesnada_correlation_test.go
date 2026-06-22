package tools

import (
	"context"
	"os"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// TestSpawnCorrelation verifies that delegation correlation metadata is derived
// from the tool context and work dir: parent session id from the context, a
// non-empty unique correlation id, and the canonical project path.
func TestSpawnCorrelation(t *testing.T) {
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "sess-42")

	dir := t.TempDir()
	// Ensure no inherited depth env leaks into this case.
	t.Setenv("PANDO_DELEGATION_DEPTH", "")
	parentSessionID, correlationID, projectPath, depth := spawnCorrelation(ctx, dir)

	if parentSessionID != "sess-42" {
		t.Fatalf("parentSessionID = %q, want sess-42", parentSessionID)
	}
	if correlationID == "" {
		t.Fatalf("correlationID is empty, want a generated id")
	}
	if want := config.CanonicalProjectPath(dir); projectPath != want {
		t.Fatalf("projectPath = %q, want %q", projectPath, want)
	}
	// No PANDO_DELEGATION_DEPTH set => parent depth 0 => this spawn is depth 1.
	if depth != 1 {
		t.Fatalf("depth = %d, want 1 (top-level spawn)", depth)
	}

	// Each call produces a unique correlation id.
	_, corr2, _, _ := spawnCorrelation(ctx, dir)
	if corr2 == correlationID {
		t.Fatalf("correlation id not unique across calls: %q", corr2)
	}
}

// TestSpawnCorrelationDepthPropagation verifies that the parent depth carried via
// PANDO_DELEGATION_DEPTH is read and the new task's depth is parent+1, and that an
// unset/invalid value defaults the parent depth to 0.
func TestSpawnCorrelationDepthPropagation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Parent depth 2 => child depth 3.
	t.Setenv("PANDO_DELEGATION_DEPTH", "2")
	if _, _, _, depth := spawnCorrelation(ctx, dir); depth != 3 {
		t.Fatalf("depth = %d, want 3 (parent 2 + 1)", depth)
	}

	// Invalid value => parent depth 0 => child depth 1.
	t.Setenv("PANDO_DELEGATION_DEPTH", "not-a-number")
	if _, _, _, depth := spawnCorrelation(ctx, dir); depth != 1 {
		t.Fatalf("depth = %d, want 1 (invalid env => parent 0)", depth)
	}

	// Unset => parent depth 0 => child depth 1.
	t.Setenv("PANDO_DELEGATION_DEPTH", "")
	if _, _, _, depth := spawnCorrelation(ctx, dir); depth != 1 {
		t.Fatalf("depth = %d, want 1 (unset env => parent 0)", depth)
	}
}

// TestSpawnCorrelationDefaults verifies empty work dir resolves to the current
// directory and a missing session id yields an empty parent session.
func TestSpawnCorrelationDefaults(t *testing.T) {
	parentSessionID, correlationID, projectPath, _ := spawnCorrelation(context.Background(), "")

	if parentSessionID != "" {
		t.Fatalf("parentSessionID = %q, want empty (no session in ctx)", parentSessionID)
	}
	if correlationID == "" {
		t.Fatalf("correlationID empty, want generated id even without session")
	}
	if want := config.CanonicalProjectPath("."); projectPath != want {
		t.Fatalf("projectPath = %q, want canonical of %q", projectPath, mustWD())
	}
}

func mustWD() string {
	wd, _ := os.Getwd()
	return wd
}
