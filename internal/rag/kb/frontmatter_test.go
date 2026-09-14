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

func TestParseFrontMatterWithRaw_Scalars(t *testing.T) {
	raw := "---\nstatus: backlog\ncount: 3\nratio: 1.5\nactive: true\n---\nBody"
	fm, rawFM, body, err := ParseFrontMatterWithRaw(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body != "Body" {
		t.Errorf("unexpected body: %q", body)
	}
	if fm.Tags != nil {
		t.Errorf("expected no typed tags, got %v", fm.Tags)
	}
	if rawFM["status"] != "backlog" {
		t.Errorf("expected status=backlog, got %v", rawFM["status"])
	}
	if rawFM["count"] != 3 {
		t.Errorf("expected count=3, got %v (%T)", rawFM["count"], rawFM["count"])
	}
	if rawFM["ratio"] != 1.5 {
		t.Errorf("expected ratio=1.5, got %v", rawFM["ratio"])
	}
	if rawFM["active"] != true {
		t.Errorf("expected active=true, got %v", rawFM["active"])
	}
}

func TestParseFrontMatterWithRaw_Lists(t *testing.T) {
	raw := "---\nlabels:\n  - rag\n  - kb\ntags:\n  - plan\n---\nBody"
	fm, rawFM, _, err := ParseFrontMatterWithRaw(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.Tags) != 1 || fm.Tags[0] != "plan" {
		t.Errorf("expected typed tags [plan], got %v", fm.Tags)
	}
	labels, ok := rawFM["labels"].([]interface{})
	if !ok || len(labels) != 2 || labels[0] != "rag" || labels[1] != "kb" {
		t.Errorf("unexpected labels: %#v", rawFM["labels"])
	}
	// tags is present in the raw map too; the reserved-key filtering happens
	// in MergeUnknownFrontMatterKeys, not in ParseFrontMatterWithRaw.
	if _, ok := rawFM["tags"]; !ok {
		t.Errorf("expected raw map to still contain tags key")
	}
}

func TestParseFrontMatterWithRaw_NestedMaps(t *testing.T) {
	raw := "---\nparent: PANDO-EP-0005\nlinks:\n  blocks: PANDO-US-0001\n  related:\n    - PANDO-US-0003\n---\nBody"
	_, rawFM, _, err := ParseFrontMatterWithRaw(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	links, ok := rawFM["links"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected links to be a map[string]interface{}, got %T", rawFM["links"])
	}
	if links["blocks"] != "PANDO-US-0001" {
		t.Errorf("unexpected blocks: %v", links["blocks"])
	}
	related, ok := links["related"].([]interface{})
	if !ok || len(related) != 1 || related[0] != "PANDO-US-0003" {
		t.Errorf("unexpected related: %#v", links["related"])
	}
}

func TestParseFrontMatterWithRaw_EmptyBlock(t *testing.T) {
	raw := "---\n---\nBody content"
	fm, rawFM, body, err := ParseFrontMatterWithRaw(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fm.CreatedAt.IsZero() {
		t.Errorf("expected zero created_at")
	}
	if rawFM != nil {
		t.Errorf("expected nil raw map for empty block, got %v", rawFM)
	}
	if body != "Body content" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestParseFrontMatterWithRaw_NoFrontMatter(t *testing.T) {
	raw := "# No front matter\nJust text."
	fm, rawFM, body, err := ParseFrontMatterWithRaw(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fm.CreatedAt.IsZero() {
		t.Errorf("expected zero created_at")
	}
	if rawFM != nil {
		t.Errorf("expected nil raw map, got %v", rawFM)
	}
	if body != raw {
		t.Errorf("expected body == raw, got %q", body)
	}
}

func TestParseFrontMatterWithRaw_Unparseable(t *testing.T) {
	// Duplicate mapping key at the top level makes this invalid YAML for
	// gopkg.in/yaml.v3, so both unmarshal calls fail.
	raw := "---\nstatus: backlog\nstatus: done\n---\nBody"
	fm, rawFM, body, err := ParseFrontMatterWithRaw(raw)
	if err == nil {
		t.Fatalf("expected a parse error for duplicate key front matter")
	}
	if !fm.CreatedAt.IsZero() || fm.Tags != nil {
		t.Errorf("expected zero FrontMatter on error, got %+v", fm)
	}
	if rawFM != nil {
		t.Errorf("expected nil raw map on error, got %v", rawFM)
	}
	// The existing fallback: on error, body is the original raw content, so
	// callers (sync.go) still index it as-is.
	if body != raw {
		t.Errorf("expected body == raw on parse error, got %q", body)
	}
}

func TestParseFrontMatter_WrapperMatchesWithRaw(t *testing.T) {
	raw := "---\nstatus: backlog\n---\nBody"
	fm, body, err := ParseFrontMatter(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantFM, _, wantBody, wantErr := ParseFrontMatterWithRaw(raw)
	if wantErr != nil {
		t.Fatalf("unexpected error: %v", wantErr)
	}
	if body != wantBody {
		t.Errorf("body mismatch: %q vs %q", body, wantBody)
	}
	if !fm.CreatedAt.Equal(wantFM.CreatedAt) {
		t.Errorf("FrontMatter mismatch: %+v vs %+v", fm, wantFM)
	}
}

func TestMergeUnknownFrontMatterKeys_AddsUnknownKeys(t *testing.T) {
	meta := map[string]interface{}{
		"source_path":       "/tmp/doc.md",
		"source_mtime_unix": int64(123),
		"source_format":     "markdown",
	}
	rawFM := map[string]interface{}{
		"id":        "PANDO-US-0002",
		"status":    "backlog",
		"project":   "pando",
		"milestone": "PANDO-M-0002",
	}
	merged := MergeUnknownFrontMatterKeys(meta, rawFM)

	if merged["status"] != "backlog" {
		t.Errorf("expected status=backlog, got %v", merged["status"])
	}
	if merged["id"] != "PANDO-US-0002" {
		t.Errorf("expected id preserved, got %v", merged["id"])
	}
	if merged["project"] != "pando" {
		t.Errorf("expected project preserved, got %v", merged["project"])
	}
	if merged["milestone"] != "PANDO-M-0002" {
		t.Errorf("expected milestone preserved, got %v", merged["milestone"])
	}
	// Sync-owned fields are untouched.
	if merged["source_path"] != "/tmp/doc.md" {
		t.Errorf("expected source_path preserved, got %v", merged["source_path"])
	}
}

func TestMergeUnknownFrontMatterKeys_ReservedKeysCannotOverwrite(t *testing.T) {
	meta := map[string]interface{}{
		"source_path": "/real/authoritative/path.md",
	}
	rawFM := map[string]interface{}{
		"source_path": "/attacker/controlled/path.md",
		"key":         "should-not-overwrite-memory-key",
		"status":      "backlog",
	}
	merged := MergeUnknownFrontMatterKeys(meta, rawFM)

	if merged["source_path"] != "/real/authoritative/path.md" {
		t.Errorf("reserved key source_path was overwritten: %v", merged["source_path"])
	}
	if _, ok := merged["key"]; ok {
		t.Errorf("reserved key 'key' should not be copied from front matter, got %v", merged["key"])
	}
	if merged["status"] != "backlog" {
		t.Errorf("expected unreserved key status to be copied, got %v", merged["status"])
	}
}

func TestMergeUnknownFrontMatterKeys_EmptyRaw(t *testing.T) {
	meta := map[string]interface{}{"source_path": "/x.md"}
	merged := MergeUnknownFrontMatterKeys(meta, nil)
	if len(merged) != 1 || merged["source_path"] != "/x.md" {
		t.Errorf("expected meta unchanged for nil rawFM, got %v", merged)
	}
}

func TestMergeUnknownFrontMatterKeys_NilMeta(t *testing.T) {
	merged := MergeUnknownFrontMatterKeys(nil, map[string]interface{}{"status": "backlog"})
	if merged["status"] != "backlog" {
		t.Errorf("expected status copied into freshly allocated map, got %v", merged)
	}
}

func TestJSONSafeValue_StringifiesExoticTypes(t *testing.T) {
	ts := time.Date(2026, 9, 13, 21, 14, 28, 0, time.UTC)
	got := jsonSafeValue(ts)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected time.Time to be stringified, got %T", got)
	}
	if s == "" {
		t.Errorf("expected non-empty stringified value")
	}
}

func TestJSONSafeValue_NestedListsAndMaps(t *testing.T) {
	in := []interface{}{
		map[string]interface{}{"a": 1, "b": []interface{}{"x", "y"}},
	}
	got := jsonSafeValue(in)
	out, ok := got.([]interface{})
	if !ok || len(out) != 1 {
		t.Fatalf("expected a 1-element slice, got %#v", got)
	}
	m, ok := out[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map, got %T", out[0])
	}
	if m["a"] != 1 {
		t.Errorf("expected a=1, got %v", m["a"])
	}
	inner, ok := m["b"].([]interface{})
	if !ok || len(inner) != 2 || inner[0] != "x" || inner[1] != "y" {
		t.Errorf("unexpected nested list: %#v", m["b"])
	}
}
