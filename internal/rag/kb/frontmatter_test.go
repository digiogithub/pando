package kb

import (
	"strings"
	"testing"
	"time"
)

func TestParseFrontMatter_Valid(t *testing.T) {
	raw := "---\ncreated_at: \"2026-01-15T10:30:00Z\"\nupdated_at: \"2026-02-20T14:00:00Z\"\ntags:\n  - plan\n  - kb\n---\n# Hello\nThis is the body."

	fm, body, err := ParseFrontMatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fm.CreatedAt.Year() != 2026 || fm.CreatedAt.Month() != 1 || fm.CreatedAt.Day() != 15 {
		t.Errorf("unexpected created_at: %v", fm.CreatedAt)
	}
	if fm.UpdatedAt.Year() != 2026 || fm.UpdatedAt.Month() != 2 {
		t.Errorf("unexpected updated_at: %v", fm.UpdatedAt)
	}
	if len(fm.Tags) != 2 || fm.Tags[0] != "plan" || fm.Tags[1] != "kb" {
		t.Errorf("unexpected tags: %v", fm.Tags)
	}
	if !strings.HasPrefix(body, "# Hello") {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestParseFrontMatter_NoFrontMatter(t *testing.T) {
	raw := "# Just a document\nNo front matter here."

	fm, body, err := ParseFrontMatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fm.CreatedAt.IsZero() {
		t.Errorf("expected zero created_at, got %v", fm.CreatedAt)
	}
	if body != raw {
		t.Errorf("expected body == raw, got %q", body)
	}
}

func TestParseFrontMatter_EmptyContent(t *testing.T) {
	fm, body, err := ParseFrontMatter("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fm.CreatedAt.IsZero() {
		t.Errorf("expected zero created_at")
	}
	if body != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestParseFrontMatter_OnlyDelimiter(t *testing.T) {
	raw := "---\n---\nBody content"
	fm, body, err := ParseFrontMatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty YAML block is valid, returns zero FrontMatter.
	if !fm.CreatedAt.IsZero() {
		t.Errorf("expected zero created_at")
	}
	if body != "Body content" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestParseFrontMatter_TagsOnly(t *testing.T) {
	raw := "---\ntags:\n  - feature\n---\nContent here."
	fm, body, err := ParseFrontMatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.Tags) != 1 || fm.Tags[0] != "feature" {
		t.Errorf("unexpected tags: %v", fm.Tags)
	}
	if body != "Content here." {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestSerializeFrontMatter(t *testing.T) {
	fm := FrontMatter{
		CreatedAt: time.Date(2026, 3, 10, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 15, 12, 30, 0, 0, time.UTC),
		Tags:      []string{"plan", "implementation"},
	}
	body := "# My Document\nSome content."

	result := SerializeFrontMatter(fm, body)

	if !strings.HasPrefix(result, "---\n") {
		t.Errorf("expected front matter prefix, got: %q", result[:20])
	}
	if !strings.Contains(result, "tags:") {
		t.Errorf("expected tags in output: %q", result)
	}
	if !strings.Contains(result, "plan") {
		t.Errorf("expected 'plan' tag in output")
	}
	if !strings.HasSuffix(result, "Some content.") {
		t.Errorf("expected body at end: %q", result)
	}
}

func TestSerializeFrontMatter_NoTags(t *testing.T) {
	fm := FrontMatter{
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	result := SerializeFrontMatter(fm, "Body")
	if strings.Contains(result, "tags:") {
		t.Errorf("expected no tags field when empty, got: %q", result)
	}
}

func TestRoundTrip(t *testing.T) {
	fm := FrontMatter{
		CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 6, 5, 15, 30, 0, 0, time.UTC),
		Tags:      []string{"alpha", "beta"},
	}
	body := "# Title\n\nParagraph content."

	serialized := SerializeFrontMatter(fm, body)
	parsedFM, parsedBody, err := ParseFrontMatter(serialized)
	if err != nil {
		t.Fatalf("round-trip parse error: %v", err)
	}

	if !parsedFM.CreatedAt.Equal(fm.CreatedAt) {
		t.Errorf("created_at mismatch: %v vs %v", parsedFM.CreatedAt, fm.CreatedAt)
	}
	if !parsedFM.UpdatedAt.Equal(fm.UpdatedAt) {
		t.Errorf("updated_at mismatch: %v vs %v", parsedFM.UpdatedAt, fm.UpdatedAt)
	}
	if len(parsedFM.Tags) != 2 || parsedFM.Tags[0] != "alpha" || parsedFM.Tags[1] != "beta" {
		t.Errorf("tags mismatch: %v", parsedFM.Tags)
	}
	if parsedBody != body {
		t.Errorf("body mismatch: %q vs %q", parsedBody, body)
	}
}

func TestMergeFrontMatter_PreservesCreatedAt(t *testing.T) {
	existing := FrontMatter{
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Tags:      []string{"old"},
	}
	incoming := FrontMatter{
		Tags: []string{"new", "updated"},
	}

	merged := MergeFrontMatter(existing, incoming)

	if !merged.CreatedAt.Equal(existing.CreatedAt) {
		t.Errorf("created_at should be preserved: %v", merged.CreatedAt)
	}
	if merged.UpdatedAt.Before(existing.UpdatedAt) {
		t.Errorf("updated_at should be newer: %v", merged.UpdatedAt)
	}
	if len(merged.Tags) != 2 || merged.Tags[0] != "new" {
		t.Errorf("tags should use incoming: %v", merged.Tags)
	}
}

func TestMergeFrontMatter_KeepsExistingTagsWhenNoIncoming(t *testing.T) {
	existing := FrontMatter{
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Tags:      []string{"keep-me"},
	}
	incoming := FrontMatter{}

	merged := MergeFrontMatter(existing, incoming)

	if len(merged.Tags) != 1 || merged.Tags[0] != "keep-me" {
		t.Errorf("expected existing tags preserved: %v", merged.Tags)
	}
}

func TestNewFrontMatter(t *testing.T) {
	before := time.Now().UTC()
	fm := NewFrontMatter([]string{"test"}, nil)
	after := time.Now().UTC()

	if fm.CreatedAt.Before(before) || fm.CreatedAt.After(after) {
		t.Errorf("created_at out of range: %v", fm.CreatedAt)
	}
	if !fm.CreatedAt.Equal(fm.UpdatedAt) {
		t.Errorf("new front matter should have created_at == updated_at")
	}
	if len(fm.Tags) != 1 || fm.Tags[0] != "test" {
		t.Errorf("unexpected tags: %v", fm.Tags)
	}
}

func TestStripFrontMatter(t *testing.T) {
	raw := "---\ntags:\n  - x\n---\nBody only"
	body := StripFrontMatter(raw)
	if body != "Body only" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestStripFrontMatter_NoFrontMatter(t *testing.T) {
	raw := "Just text"
	body := StripFrontMatter(raw)
	if body != raw {
		t.Errorf("expected raw returned as-is")
	}
}
