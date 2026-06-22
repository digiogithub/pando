package conclusion

import (
	"strings"
	"testing"
)

func TestParse_HappyBlock(t *testing.T) {
	raw := `Some preamble.
<pando:conclusion>
status: success
summary: Did the thing well.
artifacts:
  - a/b.go
  - c/d.go
memory_refs:
  - kb-1
follow_up: nothing
confidence: 0.8
</pando:conclusion>
trailing text`

	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected a block to be found")
	}
	if c.Status != "success" {
		t.Errorf("status = %q, want success", c.Status)
	}
	if c.Summary != "Did the thing well." {
		t.Errorf("summary = %q", c.Summary)
	}
	if len(c.Artifacts) != 2 || c.Artifacts[0] != "a/b.go" {
		t.Errorf("artifacts = %v", c.Artifacts)
	}
	if len(c.MemoryRefs) != 1 || c.MemoryRefs[0] != "kb-1" {
		t.Errorf("memory_refs = %v", c.MemoryRefs)
	}
	if c.FollowUp != "nothing" {
		t.Errorf("follow_up = %q", c.FollowUp)
	}
	if c.Confidence != 0.8 {
		t.Errorf("confidence = %v", c.Confidence)
	}
}

func TestParse_MissingBlock(t *testing.T) {
	if c, ok := Parse("no sentinel here at all"); ok || c != nil {
		t.Errorf("expected (nil,false), got (%v,%v)", c, ok)
	}
}

func TestParse_PartialFields(t *testing.T) {
	raw := `<pando:conclusion>
summary: only a summary
</pando:conclusion>`
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	if c.Summary != "only a summary" {
		t.Errorf("summary = %q", c.Summary)
	}
	if c.Status != "" {
		t.Errorf("status should default to empty, got %q", c.Status)
	}
	if c.Confidence != 0 {
		t.Errorf("confidence = %v, want 0", c.Confidence)
	}
}

func TestParse_MultipleBlocksLastWins(t *testing.T) {
	raw := `<pando:conclusion>
status: partial
summary: first
</pando:conclusion>
more work
<pando:conclusion>
status: success
summary: final
</pando:conclusion>`
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	if c.Summary != "final" || c.Status != "success" {
		t.Errorf("expected last block (final/success), got %q/%q", c.Summary, c.Status)
	}
}

func TestParse_MalformedYAMLTolerant(t *testing.T) {
	raw := `<pando:conclusion>
this is: not: valid: yaml: at all: ][
  - broken
</pando:conclusion>`
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found even when body is malformed")
	}
	if c.Summary == "" {
		t.Error("expected raw inner text salvaged into summary")
	}
}

func TestParse_StatusNormalization(t *testing.T) {
	cases := map[string]string{
		"  SUCCESS ": "success",
		"Partial":    "partial",
		"FAILED":     "failed",
		"blocked":    "blocked",
		"weird":      "",
		"":           "",
	}
	for in, want := range cases {
		raw := "<pando:conclusion>\nstatus: \"" + in + "\"\nsummary: s\n</pando:conclusion>"
		c, ok := Parse(raw)
		if !ok {
			t.Fatalf("block not found for input %q", in)
		}
		if c.Status != want {
			t.Errorf("status normalization for %q = %q, want %q", in, c.Status, want)
		}
	}
}

func TestParse_ConfidenceClamping(t *testing.T) {
	high := "<pando:conclusion>\nsummary: s\nconfidence: 5.0\n</pando:conclusion>"
	c, _ := Parse(high)
	if c.Confidence != 1 {
		t.Errorf("confidence clamp high = %v, want 1", c.Confidence)
	}
	low := "<pando:conclusion>\nsummary: s\nconfidence: -3\n</pando:conclusion>"
	c, _ = Parse(low)
	if c.Confidence != 0 {
		t.Errorf("confidence clamp low = %v, want 0", c.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Warning accumulation tests (warn-don't-discard principle)
// ---------------------------------------------------------------------------

// hasWarning returns true when at least one warning in c.Warnings contains substr.
func hasWarning(warns []string, substr string) bool {
	for _, w := range warns {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestParse_WarnUnknownStatus(t *testing.T) {
	raw := "<pando:conclusion>\nstatus: done\nsummary: finished\n</pando:conclusion>"
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	// Data preserved: summary is intact, status left empty.
	if c.Summary != "finished" {
		t.Errorf("summary = %q, want %q", c.Summary, "finished")
	}
	if c.Status != "" {
		t.Errorf("status = %q, want empty for unknown", c.Status)
	}
	if !hasWarning(c.Warnings, "unknown status") {
		t.Errorf("expected unknown-status warning, got %v", c.Warnings)
	}
	if !hasWarning(c.Warnings, `"done"`) {
		t.Errorf("expected raw status value in warning, got %v", c.Warnings)
	}
}

func TestParse_WarnConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name        string
		confidence  string
		wantClamped float64
	}{
		{"too high", "1.5", 1.0},
		{"too low", "-0.1", 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := "<pando:conclusion>\nstatus: success\nsummary: s\nconfidence: " + tt.confidence + "\n</pando:conclusion>"
			c, ok := Parse(raw)
			if !ok {
				t.Fatal("expected block found")
			}
			// Data preserved: confidence is clamped, not discarded.
			if c.Confidence != tt.wantClamped {
				t.Errorf("confidence = %v, want %v", c.Confidence, tt.wantClamped)
			}
			if !hasWarning(c.Warnings, "out of range") {
				t.Errorf("expected out-of-range warning, got %v", c.Warnings)
			}
			if !hasWarning(c.Warnings, "clamped") {
				t.Errorf("expected clamped mention in warning, got %v", c.Warnings)
			}
		})
	}
}

func TestParse_WarnMissingSummary(t *testing.T) {
	raw := "<pando:conclusion>\nstatus: success\nconfidence: 0.9\n</pando:conclusion>"
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	// Status and confidence are still present.
	if c.Status != "success" {
		t.Errorf("status = %q, want success", c.Status)
	}
	if c.Confidence != 0.9 {
		t.Errorf("confidence = %v, want 0.9", c.Confidence)
	}
	if !hasWarning(c.Warnings, "missing summary") {
		t.Errorf("expected missing-summary warning, got %v", c.Warnings)
	}
}

func TestParse_WarnBlankArtifactsDropped(t *testing.T) {
	raw := `<pando:conclusion>
status: success
summary: done
artifacts:
  - valid/file.go
  - "   "
  - another/file.go
  - ""
</pando:conclusion>`
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	// Non-blank entries are kept.
	if len(c.Artifacts) != 2 {
		t.Errorf("artifacts = %v, want 2 entries", c.Artifacts)
	}
	if c.Artifacts[0] != "valid/file.go" || c.Artifacts[1] != "another/file.go" {
		t.Errorf("artifacts = %v, unexpected values", c.Artifacts)
	}
	if !hasWarning(c.Warnings, "dropped") || !hasWarning(c.Warnings, "artifacts") {
		t.Errorf("expected dropped-artifacts warning, got %v", c.Warnings)
	}
}

func TestParse_WarnBlankMemoryRefsDropped(t *testing.T) {
	raw := `<pando:conclusion>
status: success
summary: done
memory_refs:
  - kb-1
  - ""
</pando:conclusion>`
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	// Non-blank entries are kept.
	if len(c.MemoryRefs) != 1 || c.MemoryRefs[0] != "kb-1" {
		t.Errorf("memory_refs = %v, want [kb-1]", c.MemoryRefs)
	}
	if !hasWarning(c.Warnings, "dropped") || !hasWarning(c.Warnings, "memory_refs") {
		t.Errorf("expected dropped-memory_refs warning, got %v", c.Warnings)
	}
}

func TestParse_WarnMalformedYAMLSalvaged(t *testing.T) {
	raw := `<pando:conclusion>
this is: not: valid: yaml: ][
  - broken
</pando:conclusion>`
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found even when body is malformed")
	}
	// Raw inner text is salvaged as summary.
	if c.Summary == "" {
		t.Error("expected raw inner text salvaged into summary")
	}
	if !hasWarning(c.Warnings, "not valid YAML") {
		t.Errorf("expected malformed-YAML warning, got %v", c.Warnings)
	}
	if !hasWarning(c.Warnings, "salvaged raw text") {
		t.Errorf("expected salvaged-raw-text mention in warning, got %v", c.Warnings)
	}
}

func TestParse_WarnNonMappingYAMLSalvaged(t *testing.T) {
	// Valid YAML mapping but with only unknown keys — all known fields remain
	// zero, which triggers the non-mapping fallback (same effect as a bare
	// scalar from a domain perspective: no usable structured data).
	raw := "<pando:conclusion>\nunknown_field: some value\nanother_field: 42\n</pando:conclusion>"
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	// Raw text is salvaged.
	if c.Summary == "" {
		t.Error("expected raw inner text salvaged into summary")
	}
	if !hasWarning(c.Warnings, "not a YAML mapping") {
		t.Errorf("expected non-mapping warning, got %v", c.Warnings)
	}
}

func TestParse_HappyBlockNoWarnings(t *testing.T) {
	// A well-formed block must produce zero warnings.
	raw := `<pando:conclusion>
status: success
summary: Everything went fine.
artifacts:
  - internal/foo.go
memory_refs:
  - kb-1
follow_up: nothing
confidence: 0.9
</pando:conclusion>`
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	if len(c.Warnings) != 0 {
		t.Errorf("expected no warnings for well-formed block, got %v", c.Warnings)
	}
}

func TestParse_MultipleWarningsAccumulate(t *testing.T) {
	// Unknown status + out-of-range confidence → both warnings present.
	raw := "<pando:conclusion>\nstatus: done\nsummary: s\nconfidence: 99\n</pando:conclusion>"
	c, ok := Parse(raw)
	if !ok {
		t.Fatal("expected block found")
	}
	if !hasWarning(c.Warnings, "unknown status") {
		t.Errorf("expected unknown-status warning, got %v", c.Warnings)
	}
	if !hasWarning(c.Warnings, "out of range") {
		t.Errorf("expected out-of-range warning, got %v", c.Warnings)
	}
	// Both data fields still have best-effort values.
	if c.Summary != "s" {
		t.Errorf("summary = %q, want s", c.Summary)
	}
	if c.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0 (clamped)", c.Confidence)
	}
}
