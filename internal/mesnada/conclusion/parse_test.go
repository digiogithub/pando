package conclusion

import (
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
