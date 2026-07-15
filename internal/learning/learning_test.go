package learning

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
// (TestLearningEnabledForContext): the session-ID context keys live in
// internal/llm/prompt and internal/llm/tools, which this package deliberately
// does not import — see learningEnabledForContext for why.

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

func TestInstructionsNameTheRequiredTools(t *testing.T) {
	got := Instructions()
	if strings.TrimSpace(got) == "" {
		t.Fatal("expected non-empty instructions")
	}
	for _, want := range []string{
		"/learning-finish",
		"PRECEDENCE",
		"kb_search_documents",
		"AskUserQuestion",
		"kb_add_document",
		"remember",
		"kb_mark_outdated",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("instructions missing %q", want)
		}
	}
}

func TestFinishPromptConsolidatesWithoutGitSideEffects(t *testing.T) {
	got := FinishPrompt()
	if strings.TrimSpace(got) == "" {
		t.Fatal("expected non-empty finish prompt")
	}
	for _, want := range []string{
		"kb_add_document",
		"kb_mark_outdated",
		"Do NOT commit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("finish prompt missing %q", want)
		}
	}
}

func TestActivationMessage(t *testing.T) {
	base := ActivationMessage("")
	if strings.TrimSpace(base) == "" {
		t.Fatal("expected a non-empty activation message")
	}
	if strings.Contains(base, "Focus:") {
		t.Fatal("expected no focus line when focus is empty")
	}

	withFocus := ActivationMessage("  understand the KB graph  ")
	if !strings.Contains(withFocus, "Focus: understand the KB graph") {
		t.Fatalf("expected the trimmed focus to be echoed, got:\n%s", withFocus)
	}
}
