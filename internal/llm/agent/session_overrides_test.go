package agent

import "testing"

func TestAdoptDraftSessionOverrides(t *testing.T) {
	const sessionID = "adopt-draft-test"
	t.Cleanup(func() {
		SetSessionLLMOverrides(sessionID, SessionLLMOverrides{})
		SetSessionLLMOverrides(DraftSessionID, SessionLLMOverrides{})
	})

	// Nothing drafted: the session is left alone.
	AdoptDraftSessionOverrides(sessionID)
	if got := SessionLLMOverridesFor(sessionID); !got.isEmpty() {
		t.Fatalf("overrides without a draft = %+v", got)
	}

	SetSessionModelOverride(DraftSessionID, "draft-model")
	AdoptDraftSessionOverrides(sessionID)
	if got := SessionLLMOverridesFor(sessionID).Model; got != "draft-model" {
		t.Fatalf("adopted model = %q, want draft-model", got)
	}
	if got := SessionLLMOverridesFor(DraftSessionID); !got.isEmpty() {
		t.Fatalf("draft not cleared: %+v", got)
	}
}
