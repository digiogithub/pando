package config

import (
	"fmt"
	"sort"
	"strings"
)

// ParseHeaderPairs parses a user-typed list of HTTP header pairs into a map.
//
// The canonical separator is a newline or a comma, so header VALUES may contain
// spaces — which real credentials require ("Authorization: Bearer <token>").
// For backwards compatibility with the previous space-separated syntax, an
// input with neither commas nor newlines is still split on whitespace, but only
// when every resulting token carries its own ':' — otherwise it is treated as a
// single "Key: Value with spaces" pair.
func ParseHeaderPairs(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	segments := splitHeaderSegments(s)
	parsed := make(map[string]string, len(segments))
	for _, segment := range segments {
		key, value, err := parseHeaderPair(segment)
		if err != nil {
			return nil, err
		}
		parsed[key] = value
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	return parsed, nil
}

// splitHeaderSegments splits the raw input into one string per header pair.
func splitHeaderSegments(s string) []string {
	var raw []string
	if strings.ContainsAny(s, ",\n") {
		raw = strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' })
	} else if fields := strings.Fields(s); allContainColon(fields) {
		// Legacy "Key1:Value1 Key2:Value2" form, kept working.
		raw = fields
	} else {
		raw = []string{s}
	}

	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		if segment = strings.TrimSpace(segment); segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

// allContainColon reports whether every field carries its own ':' separator,
// which is what makes the legacy whitespace-separated form unambiguous.
func allContainColon(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	for _, field := range fields {
		if strings.IndexByte(field, ':') <= 0 {
			return false
		}
	}
	return true
}

// parseHeaderPair splits one "Key: Value" segment; the value keeps its spaces.
func parseHeaderPair(segment string) (string, string, error) {
	idx := strings.IndexByte(segment, ':')
	if idx <= 0 {
		return "", "", fmt.Errorf("invalid header pair %q: expected Key: Value format", segment)
	}
	key := strings.TrimSpace(segment[:idx])
	if key == "" {
		return "", "", fmt.Errorf("invalid header pair %q: key cannot be empty", segment)
	}
	return key, strings.TrimSpace(segment[idx+1:]), nil
}

// FormatHeaderPairs renders a header map back into the comma-separated form
// understood by ParseHeaderPairs, sorted by key for a stable display.
func FormatHeaderPairs(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+headers[k])
	}
	return strings.Join(parts, ", ")
}

// ParseEnvPairs parses a user-typed list of "KEY=value" environment entries
// into the []string form stdio MCP servers expect.
//
// It mirrors ParseHeaderPairs: commas and newlines are the canonical
// separators so a VALUE may contain spaces, while a plain space-separated list
// still parses as long as every token carries its own '='.
func ParseEnvPairs(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	var raw []string
	if strings.ContainsAny(s, ",\n") {
		raw = strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' })
	} else if fields := strings.Fields(s); allContainEquals(fields) {
		raw = fields
	} else {
		raw = []string{s}
	}

	entries := make([]string, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		idx := strings.IndexByte(entry, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("invalid environment entry %q: expected KEY=value format", entry)
		}
		key := strings.TrimSpace(entry[:idx])
		if key == "" {
			return nil, fmt.Errorf("invalid environment entry %q: key cannot be empty", entry)
		}
		entries = append(entries, key+"="+strings.TrimSpace(entry[idx+1:]))
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return entries, nil
}

// allContainEquals reports whether every field carries its own '=' separator.
func allContainEquals(fields []string) bool {
	if len(fields) < 2 {
		return false
	}
	for _, field := range fields {
		if strings.IndexByte(field, '=') <= 0 {
			return false
		}
	}
	return true
}

// FormatEnvPairs renders environment entries in the comma-separated form
// understood by ParseEnvPairs.
func FormatEnvPairs(env []string) string {
	if len(env) == 0 {
		return ""
	}
	return strings.Join(env, ", ")
}
