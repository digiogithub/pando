package agent

import (
	"testing"
	"time"
)

func TestSessionModelIDFollowsOverride(t *testing.T) {
	setupModelTestEnv(t, true)
	_, sessionID := newModelTestSession(t, "surface")

	if got := SessionModelID(sessionID); got != testModelCheap {
		t.Fatalf("SessionModelID = %q, want the configured %q", got, testModelCheap)
	}
	if got := SessionModelOverrideID(sessionID); got != "" {
		t.Fatalf("SessionModelOverrideID = %q, want empty without a switch", got)
	}

	SetSessionModelOverride(sessionID, testModelPricey)

	if got := SessionModelID(sessionID); got != testModelPricey {
		t.Fatalf("SessionModelID = %q, want the override %q", got, testModelPricey)
	}
	if got := SessionModelOverrideID(sessionID); got != testModelPricey {
		t.Fatalf("SessionModelOverrideID = %q, want %q", got, testModelPricey)
	}

	SetSessionModelOverride(sessionID, "")

	if got := SessionModelID(sessionID); got != testModelCheap {
		t.Fatalf("SessionModelID = %q, want the configured model back", got)
	}
	if got := SessionModelOverrideID(sessionID); got != "" {
		t.Fatalf("SessionModelOverrideID = %q, want empty after clearing", got)
	}
}

// Other sessions must not see a switch: the surfaces read this per session id.
func TestSessionModelIDIsPerSession(t *testing.T) {
	setupModelTestEnv(t, true)
	_, switched := newModelTestSession(t, "switched")
	_, untouched := newModelTestSession(t, "untouched")

	SetSessionModelOverride(switched, testModelPricey)

	if got := SessionModelID(untouched); got != testModelCheap {
		t.Fatalf("second session model = %q, want the configured model", got)
	}
}

func TestPruneExpiredSetupModelProposals(t *testing.T) {
	fresh := "prune-fresh"
	stale := "prune-stale"
	t.Cleanup(func() {
		setupModelProposals.Delete(fresh)
		setupModelProposals.Delete(stale)
	})

	setupModelProposals.Store(fresh, setupModelProposal{Model: testModelPricey, QuotedAt: time.Now()})
	setupModelProposals.Store(stale, setupModelProposal{
		Model:    testModelPricey,
		QuotedAt: time.Now().Add(-setupModelProposalTTL - time.Minute),
	})

	pruneExpiredSetupModelProposals()

	if _, ok := setupModelProposals.Load(stale); ok {
		t.Fatalf("expected the expired quote to be dropped")
	}
	if _, ok := setupModelProposals.Load(fresh); !ok {
		t.Fatalf("expected the live quote to survive")
	}
}

// endModelSwitchRun is where the sweep runs, so a session that quoted a switch
// and never confirmed it does not keep the entry forever.
func TestEndModelSwitchRunPrunesExpiredProposals(t *testing.T) {
	stale := "prune-on-run-end"
	t.Cleanup(func() { setupModelProposals.Delete(stale) })

	setupModelProposals.Store(stale, setupModelProposal{
		Model:    testModelPricey,
		QuotedAt: time.Now().Add(-setupModelProposalTTL - time.Minute),
	})

	endModelSwitchRun("some-other-session")

	if _, ok := setupModelProposals.Load(stale); ok {
		t.Fatalf("expected the expired quote to be swept at the end of a run")
	}
}
