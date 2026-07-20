package vulnhunter

import (
	"strings"
	"testing"
)

func TestWorkflowsEmbedded(t *testing.T) {
	for name, w := range map[string]string{
		"hunt.md":   huntWorkflow,
		"fix.md":    fixWorkflow,
		"verify.md": verifyWorkflow,
	} {
		if strings.TrimSpace(w) == "" {
			t.Errorf("%s is empty; the workflow template should be embedded", name)
		}
	}
}

func TestHuntPromptCoversPhasesAndTools(t *testing.T) {
	p := HuntPrompt("")
	// The hunt workflow must describe the falsification pipeline and use Pando
	// tools: parallel subagents for the hunt and the KB for the report.
	for _, want := range []string{
		"RECON", "PARALLEL HUNT", "DISPROVE", "FOLLOW THE DATA",
		"mesnada_spawn_agent", "kb_search_documents", "kb_add_document",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("HuntPrompt missing expected token %q", want)
		}
	}
}

func TestFixPromptEnforcesTDD(t *testing.T) {
	p := FixPrompt("")
	// The fix workflow must gate source edits behind RED evidence and cover the
	// full RED->GREEN cycle plus KB documentation.
	for _, want := range []string{
		"TDD GATE", "RED", "GREEN", "regression", "kb_add_document",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("FixPrompt missing expected token %q", want)
		}
	}
}

func TestVerifyPromptIsReadOnlyWithVerdicts(t *testing.T) {
	p := VerifyPrompt("")
	for _, want := range []string{
		"READ-ONLY", "SOURCE OF TRUTH", "FIXED", "PARTIAL", "NOT_FIXED", "INCONCLUSIVE",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("VerifyPrompt missing expected token %q", want)
		}
	}
}

func TestScopeAppendedOnlyWhenExtraGiven(t *testing.T) {
	if strings.Contains(HuntPrompt(""), "ADDITIONAL SCOPE") {
		t.Error("empty extra should not add the additional-scope section")
	}
	p := HuntPrompt("audit only internal/api")
	if !strings.Contains(p, "ADDITIONAL SCOPE") || !strings.Contains(p, "audit only internal/api") {
		t.Error("non-empty extra should append the additional-scope section")
	}
}
