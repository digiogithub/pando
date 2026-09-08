package telemetry

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/redact"
)

// SkipAttrKey marks a slog record that must never reach the telemetry sink.
// The shipper's own diagnostic logs (e.g. the one-time "shipping disabled"
// warning) carry it so a tee handler wired in front of the shipper does not
// re-ship them, which would otherwise create a feedback loop. A record
// carrying this key anywhere in its attrs is dropped by Shipper.Handle
// before it is even built. Matching is on the key's last dot-separated
// segment (see internal/logging's containsSkipAttr / isSkipAttrKey), so the
// guard still works when the key arrives prefixed by an open slog group
// (e.g. logger.WithGroup("g") turns it into "g.$_telemetry_skip").
const SkipAttrKey = "$_telemetry_skip"

// internalPersistAttrKey is internal/logging's own marker for messages that
// must be persisted to the in-memory log buffer (internal/logging/writer.go:16,
// persistKeyArg). It is an internal bookkeeping detail, not something worth
// shipping, so it is stripped from every record's attrs. Hardcoded rather
// than imported: internal/telemetry must not depend on internal/logging (see
// the package's dependency rule) to avoid an import cycle with the sink
// interface logging will implement against.
const internalPersistAttrKey = "$_persist"

// maxMessageBytes caps how much of a record's own message is shipped.
// maxAttrStringBytes caps each individual string value found anywhere
// (including nested) inside a record's attrs — deliberately tighter than
// maxMessageBytes, since a single attr (a tool result, a file dump, an HTTP
// body) is where an unredacted secret or an oversized blob is most likely to
// hide. maxAttrsBudgetBytes bounds the total serialized size of a record's
// attrs map: once adding another attr would cross it, every remaining attr
// is dropped and replaced by a single attrsTruncatedKey marker, rather than
// letting one record with many/huge attrs balloon past Better Stack's
// per-record guidance.
const (
	maxMessageBytes     = 8 * 1024
	maxAttrStringBytes  = 2 * 1024
	maxAttrsBudgetBytes = 16 * 1024
)

// attrsTruncatedKey is set to true on a record's attrs map when one or more
// attrs were dropped to stay within maxAttrsBudgetBytes.
const attrsTruncatedKey = "attrs_truncated"

// AppInfo describes the running binary. It is attached to every Record.
type AppInfo struct {
	Version string `json:"version"`
	Variant string `json:"variant,omitempty"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Go      string `json:"go"`
	Mode    string `json:"mode"`
}

// Record is one JSON log line shipped to Better Stack. Field names match the
// ingest API: "dt" is the RFC3339Nano timestamp Better Stack expects
// (time.Time's default JSON encoding already uses that layout).
type Record struct {
	Time    time.Time `json:"dt"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	// DebugID is the anonymous debug id in the same dashed display format
	// users see in the settings UI and copy into a support issue
	// ("1234-5678-9012-3456"), not the raw undashed form config stores —
	// this is the value a support search will actually be pasted, so
	// storing/shipping it pre-formatted avoids a format mismatch between
	// what the user has and what the record carries.
	DebugID   string         `json:"debug_id,omitempty"`
	App       AppInfo        `json:"app"`
	Source    string         `json:"source,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	// Dropped is the number of records dropped (queue full) since the last
	// record that carried a nonzero Dropped count. The Shipper stamps this
	// on the next record it pulls off the queue, not the constructor.
	Dropped int `json:"dropped,omitempty"`
}

// NewRecord builds a Record from a raw slog call: it flattens attribute
// groups into dotted keys, and for every value:
//   - redacts it whole when its key looks like a secret (redact.IsSecretKey);
//   - otherwise converts it to a JSON-safe, recursively redacted value via
//     redact.Value — which itself scrubs known secret patterns and the
//     user's home directory out of every string, and round-trips any
//     non-scalar value (a struct, a pointer, a typed map/slice, []byte, an
//     error, a fmt.Stringer, ...) into the same safe shape rather than
//     passing it through unredacted (see internal/redact.Value's doc);
//   - truncates every resulting string to maxAttrStringBytes.
//
// The message is separately scrubbed and truncated to maxMessageBytes, and
// the whole attrs map is capped to maxAttrsBudgetBytes (see capAttrsBudget)
// so one record can never carry an unbounded amount of data regardless of
// how many/how large its individual attrs are.
//
// As a convenience, a top-level "session_id" or "source" attr (no group
// prefix) is promoted to the Record's own SessionID/Source field instead of
// staying nested in Attrs, matching the documented record shape.
// DebugID, App and Dropped are left zero-valued: the Shipper fills those in
// from its own state when it builds a Record for shipping.
func NewRecord(t time.Time, level slog.Level, msg string, attrs []slog.Attr) Record {
	flat := make(map[string]any)
	flattenAttrs(flat, "", attrs)

	rec := Record{
		Time:    t,
		Level:   levelString(level),
		Message: redact.Truncate(redact.Path(redact.String(msg)), maxMessageBytes),
	}

	if sid, ok := popString(flat, "session_id"); ok {
		rec.SessionID = sid
	}
	if src, ok := popString(flat, "source"); ok {
		rec.Source = src
	}

	if len(flat) > 0 {
		rec.Attrs = capAttrsBudget(flat)
	}
	return rec
}

// levelString renders a slog.Level the way telemetry records report it:
// lowercase, e.g. "info", "warn", "error", "debug".
func levelString(level slog.Level) string {
	return strings.ToLower(level.String())
}

// flattenAttrs walks attrs, writing leaf values into dst under a
// dot-joined key ("group.subgroup.key"). Groups (slog.KindGroup) recurse;
// the internal/logging persist marker is dropped entirely.
func flattenAttrs(dst map[string]any, prefix string, attrs []slog.Attr) {
	for _, a := range attrs {
		if a.Key == "" || a.Key == internalPersistAttrKey {
			continue
		}
		key := a.Key
		if prefix != "" {
			key = prefix + "." + a.Key
		}
		if key == internalPersistAttrKey {
			continue
		}

		v := a.Value.Resolve()
		if v.Kind() == slog.KindGroup {
			flattenAttrs(dst, key, v.Group())
			continue
		}
		dst[key] = attrValueToAny(key, v.Any())
	}
}

// attrValueToAny converts one resolved slog attribute value to a JSON-safe,
// redacted, size-bounded value. redact.Value does the redaction (key-based
// whole-value redaction, string pattern scrubbing, and JSON-round-trip
// normalization of any non-scalar Go value into map[string]any/[]any/
// string); truncateStrings then caps every string found anywhere in the
// result to maxAttrStringBytes.
func attrValueToAny(key string, raw any) any {
	return truncateStrings(redact.Value(key, raw), maxAttrStringBytes)
}

// truncateStrings recursively truncates every string found in v (which is
// always one of redact.Value's output shapes: map[string]any, []any, a
// scalar, or a bare string) to at most max bytes.
func truncateStrings(v any, max int) any {
	switch val := v.(type) {
	case string:
		return redact.Truncate(val, max)
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = truncateStrings(child, max)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, child := range val {
			out[i] = truncateStrings(child, max)
		}
		return out
	default:
		return val
	}
}

// capAttrsBudget returns flat unchanged when its serialized size is within
// maxAttrsBudgetBytes. Otherwise it rebuilds the map by adding attrs one at
// a time, in sorted key order for determinism, stopping (and setting
// attrsTruncatedKey) as soon as adding another would cross the budget — so
// a record with many or huge attrs is capped rather than shipped whole
// (and rather than silently dropping the record entirely).
func capAttrsBudget(flat map[string]any) map[string]any {
	if full, err := json.Marshal(flat); err == nil && len(full) <= maxAttrsBudgetBytes {
		return flat
	}

	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(flat))
	total := 2 // "{}"
	for _, k := range keys {
		size := len(k) + 8 // approximate per-entry JSON overhead (quotes, colon, comma)
		if entry, err := json.Marshal(flat[k]); err == nil {
			size += len(entry)
		}
		if total+size > maxAttrsBudgetBytes {
			out[attrsTruncatedKey] = true
			break
		}
		out[k] = flat[k]
		total += size
	}
	return out
}

// popString removes key from m and returns its value if it was a non-empty
// string, leaving m untouched otherwise.
func popString(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	delete(m, key)
	return s, true
}
