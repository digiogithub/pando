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

func TestPromptIncludesCanonicalBlockAndSteps(t *testing.T) {
	p := Prompt("")
	if !strings.Contains(p, BeginMarker) || !strings.Contains(p, EndMarker) {
		t.Error("Prompt should embed the canonical block with its markers")
	}
	for _, want := range []string{"CONTEXT FIRST", "create vs reinforce", "DOCUMENT", "kb_search_documents"} {
		if !strings.Contains(p, want) {
			t.Errorf("Prompt missing expected guidance %q", want)
		}
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
