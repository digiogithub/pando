package agentsmd

import (
	"strings"
	"testing"
)

func TestCanonicalTemplateEmbedded(t *testing.T) {
	if strings.TrimSpace(CanonicalTemplate) == "" {
		t.Fatal("CanonicalTemplate is empty; template.md should be embedded")
	}
	if !strings.Contains(CanonicalTemplate, BeginMarker) || !strings.Contains(CanonicalTemplate, EndMarker) {
		t.Fatal("CanonicalTemplate must contain the begin/end sentinel markers")
	}
	for _, want := range []string{"MANDATORY", "kb_search_documents", "c7_resolve_library_id", "browser_navigate", "kb_add_document"} {
		if !strings.Contains(CanonicalTemplate, want) {
			t.Errorf("CanonicalTemplate missing expected token %q", want)
		}
	}
}

func TestPromptDescribesEvaluativeMerge(t *testing.T) {
	p := Prompt("")
	// The prompt must instruct an evaluative, clause-by-clause merge rather than a
	// verbatim paste, and must carry the canonical clauses as a reference.
	for _, want := range []string{
		"CONTEXT FIRST", "EVALUATE CLAUSE BY CLAUSE", "MERGE, DON'T REPLACE",
		"do NOT paste", "MANDATORY", "kb_search_documents", "kb_add_document",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("Prompt missing expected guidance %q", want)
		}
	}
}

func TestPromptStripsSentinelMarkers(t *testing.T) {
	p := Prompt("")
	// The canonical clauses are shown as a checklist; the raw sentinel markers must
	// not leak into the prompt so the agent never copies them into a target file.
	if strings.Contains(p, BeginMarker) || strings.Contains(p, EndMarker) {
		t.Error("Prompt must not contain the raw sentinel markers")
	}
	// But the substantive clause content must be present.
	if !strings.Contains(p, "gather context BEFORE starting any task") {
		t.Error("Prompt should include the canonical clause content")
	}
}

func TestPromptAppendsExtraGuidance(t *testing.T) {
	if strings.Contains(Prompt(""), "ADDITIONAL USER GUIDANCE") {
		t.Error("empty extra should not add the additional-guidance section")
	}
	p := Prompt("focus on the docs/ folder")
	if !strings.Contains(p, "ADDITIONAL USER GUIDANCE") || !strings.Contains(p, "focus on the docs/ folder") {
		t.Error("non-empty extra should append the additional-guidance section")
	}
}
