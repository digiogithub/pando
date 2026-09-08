package agent

import (
	"testing"

	"github.com/digiogithub/pando/internal/message"
)

// TestSummarizeToolResultsNeverLeaksContent verifies the fix for the
// CRITICAL code-review finding: the Info-level "Result" log line must never
// carry raw tool output (file contents, bash output, secrets, ...) — only a
// bounded summary (count, name, content size, error flag) per result.
func TestSummarizeToolResultsNeverLeaksContent(t *testing.T) {
	secretContent := "super secret file contents: api_key=zzzsecretzzz"
	msg := &message.Message{
		Role: message.Tool,
		Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "1", Name: "read_file", Content: secretContent, IsError: false},
			message.ToolResult{ToolCallID: "2", Name: "bash", Content: "boom", IsError: true},
		},
	}

	got, ok := summarizeToolResults(msg).(map[string]any)
	if !ok {
		t.Fatalf("summarizeToolResults = %T, want map[string]any", summarizeToolResults(msg))
	}

	if got["count"] != 2 {
		t.Errorf("count = %v, want 2", got["count"])
	}
	entries, ok := got["results"].([]map[string]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("results = %v, want a 2-element []map[string]any", got["results"])
	}

	if entries[0]["name"] != "read_file" {
		t.Errorf("results[0].name = %v, want read_file", entries[0]["name"])
	}
	if entries[0]["content_size"] != len(secretContent) {
		t.Errorf("results[0].content_size = %v, want %d", entries[0]["content_size"], len(secretContent))
	}
	if entries[0]["is_error"] != false {
		t.Errorf("results[0].is_error = %v, want false", entries[0]["is_error"])
	}
	if entries[1]["is_error"] != true {
		t.Errorf("results[1].is_error = %v, want true", entries[1]["is_error"])
	}

	// The actual secret/content text must never appear anywhere in the
	// summary — only its size.
	for _, e := range entries {
		for k, v := range e {
			if s, ok := v.(string); ok && (s == secretContent || (k == "name" && s == secretContent)) {
				t.Errorf("summary leaked raw content under key %q: %v", k, v)
			}
		}
	}
}

// TestSummarizeToolResultsNilMessage verifies a nil toolResults message (no
// tool calls this turn) summarizes to a zero count instead of panicking.
func TestSummarizeToolResultsNilMessage(t *testing.T) {
	got, ok := summarizeToolResults(nil).(map[string]any)
	if !ok {
		t.Fatalf("summarizeToolResults(nil) = %T, want map[string]any", summarizeToolResults(nil))
	}
	if got["count"] != 0 {
		t.Errorf("count = %v, want 0", got["count"])
	}
}
