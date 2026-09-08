package telemetry

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestNewRecordBasicFields(t *testing.T) {
	now := time.Now()
	rec := NewRecord(now, slog.LevelInfo, "hello world", nil)

	if rec.Level != "info" {
		t.Errorf("Level = %q, want %q", rec.Level, "info")
	}
	if rec.Message != "hello world" {
		t.Errorf("Message = %q, want %q", rec.Message, "hello world")
	}
	if !rec.Time.Equal(now) {
		t.Errorf("Time = %v, want %v", rec.Time, now)
	}

	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	dtStr, ok := raw["dt"].(string)
	if !ok {
		t.Fatalf("missing/invalid dt field: %v", raw["dt"])
	}
	if _, err := time.Parse(time.RFC3339Nano, dtStr); err != nil {
		t.Errorf("dt %q is not RFC3339Nano: %v", dtStr, err)
	}
}

func TestNewRecordFlattensGroups(t *testing.T) {
	attrs := []slog.Attr{
		slog.String("top", "value"),
		slog.Group("http", slog.String("method", "GET"), slog.Int("status", 200)),
	}
	rec := NewRecord(time.Now(), slog.LevelInfo, "msg", attrs)

	if rec.Attrs["top"] != "value" {
		t.Errorf("top = %v, want value", rec.Attrs["top"])
	}
	if rec.Attrs["http.method"] != "GET" {
		t.Errorf("http.method = %v, want GET", rec.Attrs["http.method"])
	}
	if rec.Attrs["http.status"] != int64(200) {
		t.Errorf("http.status = %v (%T), want int64(200)", rec.Attrs["http.status"], rec.Attrs["http.status"])
	}
}

func TestNewRecordRedactsSecretKeyAttr(t *testing.T) {
	attrs := []slog.Attr{slog.String("apiKey", "sk-abcdefghijklmno")}
	rec := NewRecord(time.Now(), slog.LevelInfo, "msg", attrs)
	if rec.Attrs["apiKey"] != "[REDACTED]" {
		t.Errorf("apiKey = %v, want [REDACTED]", rec.Attrs["apiKey"])
	}
}

func TestNewRecordScrubsMessageAndStringAttrs(t *testing.T) {
	attrs := []slog.Attr{slog.String("detail", "connect to postgres://admin:s3cr3t@db:5432/x")}
	rec := NewRecord(time.Now(), slog.LevelInfo, "token leaked: Bearer abcDEF123456", attrs)

	if strings.Contains(rec.Message, "abcDEF123456") {
		t.Errorf("message still contains raw token: %q", rec.Message)
	}
	detail, _ := rec.Attrs["detail"].(string)
	if strings.Contains(detail, "s3cr3t") {
		t.Errorf("detail attr still contains raw password: %q", detail)
	}
}

func TestNewRecordTruncatesLongValues(t *testing.T) {
	long := strings.Repeat("a", 20000)
	rec := NewRecord(time.Now(), slog.LevelInfo, long, []slog.Attr{slog.String("big", long)})

	if len(rec.Message) >= 20000 {
		t.Errorf("message not truncated, len=%d", len(rec.Message))
	}
	if !strings.Contains(rec.Message, "truncated") {
		t.Errorf("message missing truncation marker: %q", rec.Message[:50])
	}
	big, _ := rec.Attrs["big"].(string)
	if len(big) >= 20000 {
		t.Errorf("attr not truncated, len=%d", len(big))
	}
}

func TestNewRecordExcludesPersistMarker(t *testing.T) {
	attrs := []slog.Attr{slog.Bool("$_persist", true), slog.String("keep", "yes")}
	rec := NewRecord(time.Now(), slog.LevelInfo, "msg", attrs)

	if _, ok := rec.Attrs["$_persist"]; ok {
		t.Errorf("internal persist marker leaked into attrs: %v", rec.Attrs)
	}
	if rec.Attrs["keep"] != "yes" {
		t.Errorf("keep = %v, want yes", rec.Attrs["keep"])
	}
}

func TestNewRecordPromotesSessionIDAndSource(t *testing.T) {
	attrs := []slog.Attr{
		slog.String("session_id", "abc123"),
		slog.String("source", "agent-loop"),
		slog.String("other", "x"),
	}
	rec := NewRecord(time.Now(), slog.LevelInfo, "msg", attrs)

	if rec.SessionID != "abc123" {
		t.Errorf("SessionID = %q, want abc123", rec.SessionID)
	}
	if rec.Source != "agent-loop" {
		t.Errorf("Source = %q, want agent-loop", rec.Source)
	}
	if _, ok := rec.Attrs["session_id"]; ok {
		t.Errorf("session_id should have been promoted out of Attrs")
	}
	if rec.Attrs["other"] != "x" {
		t.Errorf("other = %v, want x", rec.Attrs["other"])
	}
}

// TestNewRecordRedactsNonScalarAttrValues covers the CRITICAL privacy gap
// from code review: a *message.Message-shaped (or any struct/map) attr value
// logged via slog.Any used to pass through record building completely
// unredacted and untruncated. Structs, maps, []byte and errors nested in an
// attr must now come out redacted/bounded the same as a plain string would.
func TestNewRecordRedactsNonScalarAttrValues(t *testing.T) {
	type toolResult struct {
		Content string
		APIKey  string
	}
	// The OpenAI-shaped key is built by concatenation, not a contiguous
	// literal, so it cannot trip a secret scanner on this source file while
	// still exercising the real redaction pattern (see internal/redact's
	// reOpenAIKey).
	fakeKey := "sk-" + "abcdefghijklmno"
	attrs := []slog.Attr{
		slog.Any("toolResults", []toolResult{
			{Content: "some file content", APIKey: fakeKey},
		}),
		slog.Any("raw", []byte("password=hunter2")),
		slog.Any("headers", map[string]string{"Authorization": "Bearer zzzz"}),
	}
	rec := NewRecord(time.Now(), slog.LevelInfo, "Result", attrs)

	results, ok := rec.Attrs["toolResults"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("toolResults = %v (%T), want a 1-element []any", rec.Attrs["toolResults"], rec.Attrs["toolResults"])
	}
	entry, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("toolResults[0] = %v (%T), want map[string]any", results[0], results[0])
	}
	if entry["APIKey"] != "[REDACTED]" {
		t.Errorf("toolResults[0].APIKey = %v, want [REDACTED]", entry["APIKey"])
	}
	if entry["Content"] != "some file content" {
		t.Errorf("toolResults[0].Content = %v, want unchanged", entry["Content"])
	}

	raw, ok := rec.Attrs["raw"].(string)
	if !ok || strings.Contains(raw, "hunter2") || strings.Contains(raw, "password") {
		t.Fatalf("raw = %v, want a byte-count marker with no leaked content", rec.Attrs["raw"])
	}

	headers, ok := rec.Attrs["headers"].(map[string]any)
	if !ok || headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("headers = %v, want Authorization redacted", rec.Attrs["headers"])
	}
}

// TestNewRecordCapsIndividualAttrStringsTighter verifies each attr string is
// capped at 2 KiB (tighter than the 8 KiB message cap), since an attr is
// where an oversized tool result/file dump is most likely to appear.
func TestNewRecordCapsIndividualAttrStringsTighter(t *testing.T) {
	long := strings.Repeat("a", 10000)
	rec := NewRecord(time.Now(), slog.LevelInfo, "msg", []slog.Attr{slog.String("big", long)})

	big, _ := rec.Attrs["big"].(string)
	if len(big) > maxAttrStringBytes+len(`…[truncated]`) {
		t.Errorf("attr string len = %d, want <= ~%d (maxAttrStringBytes)", len(big), maxAttrStringBytes)
	}
	if len(rec.Message) < len(long) {
		// sanity: message cap (8 KiB) is looser than the attr cap (2 KiB) —
		// not exercised further here, TestNewRecordTruncatesLongValues covers it.
		_ = rec.Message
	}
}

// TestNewRecordCapsTotalAttrsBudget verifies a record with many/huge attrs
// is bounded to roughly maxAttrsBudgetBytes total, with the remainder
// replaced by a single attrs_truncated marker rather than shipping an
// unbounded record.
func TestNewRecordCapsTotalAttrsBudget(t *testing.T) {
	var attrs []slog.Attr
	chunk := strings.Repeat("b", maxAttrStringBytes) // each attr is already at the per-string cap
	for i := 0; i < 20; i++ {
		attrs = append(attrs, slog.String(strings.Repeat("k", 1)+string(rune('a'+i)), chunk))
	}
	rec := NewRecord(time.Now(), slog.LevelInfo, "msg", attrs)

	b, err := json.Marshal(rec.Attrs)
	if err != nil {
		t.Fatalf("Marshal(rec.Attrs): %v", err)
	}
	if len(b) > maxAttrsBudgetBytes+512 { // small slack for the marker/overhead itself
		t.Errorf("attrs serialized size = %d, want roughly <= %d", len(b), maxAttrsBudgetBytes)
	}
	if rec.Attrs[attrsTruncatedKey] != true {
		t.Errorf("attrs_truncated marker missing, attrs = %v", rec.Attrs)
	}
}

func TestRecordOmitsEmptyAttrs(t *testing.T) {
	rec := NewRecord(time.Now(), slog.LevelInfo, "msg", nil)
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), `"attrs"`) {
		t.Errorf("attrs should be omitted when empty: %s", b)
	}
}
