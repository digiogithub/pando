package tools

import (
	"strings"
	"testing"
)

func TestFormatJSONLikeContentPrefersTOONTOML(t *testing.T) {
	got := FormatJSONLikeContent(`{"status":"ok","count":2}`)
	if got == `{"status":"ok","count":2}` {
		t.Fatalf("expected formatted structured output, got raw JSON")
	}
	if !strings.Contains(got, "status = 'ok'") {
		t.Fatalf("expected TOON/TOML status field, got %q", got)
	}
	if !strings.Contains(got, "count = 2.0") {
		t.Fatalf("expected TOON/TOML numeric field, got %q", got)
	}
}

func TestFormatJSONLikeContentFallsBackToJSONWhenTOONFails(t *testing.T) {
	got := FormatJSONLikeContent(`[1,2,3]`)
	if got == `[1,2,3]` {
		t.Fatalf("expected formatted fallback output, got raw JSON")
	}
	if !strings.Contains(got, "1.0") || !strings.HasPrefix(got, "[") {
		t.Fatalf("expected array fallback rendering, got %q", got)
	}
}

func TestNewStructuredResponseFallsBackToJSONWhenTOONFails(t *testing.T) {
	resp := NewStructuredResponse([]int{1, 2, 3})
	if resp.Content == `[1,2,3]` {
		t.Fatalf("expected formatted fallback output, got raw JSON")
	}
	if !strings.Contains(resp.Content, "1") || !strings.HasPrefix(resp.Content, "[") {
		t.Fatalf("unexpected fallback output: %q", resp.Content)
	}
}
