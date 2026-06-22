package tools

import (
	"strings"
	"testing"
)

func TestFormatJSONLikeContentPrefersTOON(t *testing.T) {
	got := FormatJSONLikeContent(`{"status":"ok","count":2}`)
	if got == `{"status":"ok","count":2}` {
		t.Fatalf("expected formatted structured output, got raw JSON")
	}
	if strings.Contains(got, "status = 'ok'") {
		t.Fatalf("expected TOON output instead of TOML, got %q", got)
	}
	if !strings.Contains(got, "status: ok") {
		t.Fatalf("expected TOON status field, got %q", got)
	}
	if !strings.Contains(got, "count: 2") {
		t.Fatalf("expected TOON numeric field, got %q", got)
	}
}

func TestFormatJSONLikeContentUsesTOONArraySyntax(t *testing.T) {
	got := FormatJSONLikeContent(`[1,2,3]`)
	if got == `[1,2,3]` {
		t.Fatalf("expected formatted structured output, got raw JSON")
	}
	if !strings.HasPrefix(got, "[3]:") {
		t.Fatalf("expected TOON root array header, got %q", got)
	}
	if !strings.Contains(got, "1,2,3") {
		t.Fatalf("expected TOON inline primitive array values, got %q", got)
	}
}

func TestNewStructuredResponsePrefersTOONTabularArrays(t *testing.T) {
	resp := NewStructuredResponse(map[string]any{
		"users": []map[string]any{
			{"id": 1, "name": "Ada"},
			{"id": 2, "name": "Bob"},
		},
	})
	if strings.Contains(resp.Content, "[[users]]") {
		t.Fatalf("expected TOON output instead of TOML array-of-tables, got %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "users[2]{id,name}:") {
		t.Fatalf("expected TOON tabular header, got %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "1,Ada") || !strings.Contains(resp.Content, "2,Bob") {
		t.Fatalf("expected TOON tabular rows, got %q", resp.Content)
	}
}
