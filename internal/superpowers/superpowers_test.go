package superpowers

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSetEnabledAndEnabled(t *testing.T) {
	const sessionID = "session-enable"
	t.Cleanup(func() { SetEnabled(sessionID, false) })

	if Enabled(sessionID) {
		t.Fatal("expected mode to be disabled by default")
	}

	SetEnabled(sessionID, true)
	if !Enabled(sessionID) {
		t.Fatal("expected mode to be enabled after SetEnabled(true)")
	}

	// Re-enabling is idempotent.
	SetEnabled(sessionID, true)
	if !Enabled(sessionID) {
		t.Fatal("expected mode to stay enabled after a second SetEnabled(true)")
	}

	SetEnabled(sessionID, false)
	if Enabled(sessionID) {
		t.Fatal("expected mode to be disabled after SetEnabled(false)")
	}
}

func TestSessionIDNormalization(t *testing.T) {
	const sessionID = "session-normalize"
	t.Cleanup(func() { SetEnabled(sessionID, false) })

	SetEnabled("  "+sessionID+"  ", true)
	if !Enabled(sessionID) {
		t.Fatal("expected a padded session ID to be normalized on store")
	}
	if !Enabled("\t" + sessionID + "\n") {
		t.Fatal("expected a padded session ID to be normalized on lookup")
	}
}

func TestEmptySessionIDIsIgnored(t *testing.T) {
	SetEnabled("   ", true)
	if Enabled("") || Enabled("   ") {
		t.Fatal("expected an empty session ID never to be enabled")
	}
}

func TestSessionsAreIsolated(t *testing.T) {
	const enabledSession = "session-a"
	const otherSession = "session-b"
	t.Cleanup(func() {
		SetEnabled(enabledSession, false)
		SetEnabled(otherSession, false)
	})

	SetEnabled(enabledSession, true)
	if Enabled(otherSession) {
		t.Fatal("expected the mode not to leak into another session")
	}
	if !Enabled(enabledSession) {
		t.Fatal("expected the enabled session to keep the mode")
	}
}

// Context-based resolution is tested in internal/llm/agent
// (TestSuperpowersEnabledForContext): the session-ID context keys live in
// internal/llm/prompt and internal/llm/tools, which this package deliberately
// does not import — see superpowersEnabledForContext for why.

func TestConcurrentAccessIsRaceFree(t *testing.T) {
	const sessions = 16
	var wg sync.WaitGroup

	for i := range sessions {
		sessionID := fmt.Sprintf("concurrent-%d", i)
		t.Cleanup(func() { SetEnabled(sessionID, false) })

		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 50 {
				SetEnabled(sessionID, true)
				SetEnabled(sessionID, false)
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				_ = Enabled(sessionID)
			}
		}()
	}

	wg.Wait()
}

func TestInstructionsContainLifecycleGates(t *testing.T) {
	got := Instructions()
	if strings.TrimSpace(got) == "" {
		t.Fatal("expected non-empty instructions")
	}
	for _, want := range []string{
		"/superpowers-finish",
		"PRECEDENCE",
		"DESIGN, THEN GET APPROVAL",
		"PLAN LONG WORK EXPLICITLY",
		"TEST-FIRST",
		"VERIFY WITH EVIDENCE",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q", want)
		}
	}
}
