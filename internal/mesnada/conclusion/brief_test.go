package conclusion

import (
	"strings"
	"testing"
)

func TestBriefInstruction_NonEmptyMentionsSentinel(t *testing.T) {
	b := BriefInstruction()
	if strings.TrimSpace(b) == "" {
		t.Fatal("brief instruction must not be empty")
	}
	if !strings.Contains(b, "<pando:conclusion>") {
		t.Error("brief must mention the opening sentinel")
	}
	if !strings.Contains(b, "</pando:conclusion>") {
		t.Error("brief must mention the closing sentinel")
	}
	for _, field := range []string{"status", "summary", "artifacts", "memory_refs", "follow_up", "confidence"} {
		if !strings.Contains(b, field) {
			t.Errorf("brief should mention model-known field %q", field)
		}
	}
}
