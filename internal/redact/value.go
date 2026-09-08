package redact

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// reFlagName matches a single CLI-style flag token: "-x", "--foo" or
// "--foo=value". Group 1 is the flag name, group 2 (if present) is the
// "=value" suffix including the "=", group 3 is the value alone.
var reFlagName = regexp.MustCompile(`^-{1,2}([A-Za-z][A-Za-z0-9_-]*)(=(.*))?$`)

// Value redacts v for safe logging/shipping and returns a JSON-safe result
// (map[string]any, []any, string, or a scalar).
//
// v may be an already-decoded JSON tree (map[string]any/[]any/scalars, e.g.
// from json.Unmarshal into `any`) — those are walked directly, preserving
// their original scalar types (so an int stays an int, not a float64) — or
// any other Go value: a struct, a pointer, a typed map (map[string]string,
// http.Header, ...), a typed slice ([]string, ...), []byte, an error or a
// fmt.Stringer. Anything not natively recognized is converted to the same
// generic map[string]any/[]any/scalar shape via a round trip through
// encoding/json (falling back to fmt.Sprint for a value json.Marshal
// rejects, e.g. a channel or a function), and then walked the same way.
//
// Redaction rules, applied recursively:
//   - any value found under a key that looks like a secret (IsSecretKey)
//     becomes "[REDACTED]", whole — including a nested array/object, not
//     just a scalar (so `{"api_keys":["a","b"]}` redacts to
//     `{"api_keys":"[REDACTED]"}`, not a partially-redacted array);
//   - every remaining string has known secret patterns scrubbed (String)
//     and the user's home directory rewritten to "~" (Path);
//   - []byte becomes "[N bytes]" — its content is never shown, not even
//     base64-encoded, since JSON's default []byte encoding is base64 and
//     would otherwise round-trip a redacted-looking value straight back
//     into recoverable bytes;
//   - a []string (or []any of strings) shaped like a CLI argument list
//     ("--token", "X", "--api-key=Y", ...) has the value following a
//     secret-named flag redacted, inline or as the next element;
//   - other scalars (numbers, bools, nil) pass through unchanged.
//
// key is the field name v was found under; pass "" for the root of a tree
// or for slice/array elements, which have no key of their own.
func Value(key string, v any) any {
	return redactAny(key, v)
}

func redactAny(key string, v any) any {
	if key != "" && IsSecretKey(key) {
		return redactedPlaceholder
	}
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return Path(String(val))
	case []byte:
		return fmt.Sprintf("[%d bytes]", len(val))
	case error:
		return Path(String(val.Error()))
	case fmt.Stringer:
		return Path(String(val.String()))
	case map[string]any:
		return redactMap(val)
	case []any:
		return redactSlice(val)
	case bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, uintptr,
		float32, float64:
		return val
	default:
		return redactViaJSON(val)
	}
}

func redactMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, child := range m {
		out[k] = redactAny(k, child)
	}
	return out
}

func redactSlice(s []any) []any {
	if isFlagArgs(s) {
		return redactFlagArgs(s)
	}
	out := make([]any, len(s))
	for i, child := range s {
		out[i] = redactAny("", child)
	}
	return out
}

// redactViaJSON handles any type not already recognized natively in
// redactAny — structs, pointers, typed maps (map[string]string,
// http.Header, ...), typed slices ([]string, ...) and anything else
// json.Marshal accepts — by round-tripping it through encoding/json into
// the same generic map[string]any/[]any/scalar shape the rest of this file
// already knows how to walk. A value json.Marshal rejects (a channel, a
// function, a value with a cyclic pointer chain, ...) falls back to
// fmt.Sprint, scrubbed the same way a plain string is.
func redactViaJSON(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return Path(String(fmt.Sprint(v)))
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return Path(String(fmt.Sprint(v)))
	}
	return redactAny("", decoded)
}

// isFlagArgs reports whether s looks like a CLI argument list — the shape a
// []string takes when it comes from logging a subprocess command line
// (os/exec args, MCP server args, ...): at least one element matches
// reFlagName.
func isFlagArgs(s []any) bool {
	for _, v := range s {
		if str, ok := v.(string); ok && reFlagName.MatchString(str) {
			return true
		}
	}
	return false
}

// redactFlagArgs walks a CLI-argument-shaped slice and redacts the value
// that follows a secret-named flag: "--token X" becomes "--token
// [REDACTED]" (X, the next element, is replaced whole) and "--api-key=X"
// becomes "--api-key=[REDACTED]" (redacted in place, keeping the
// flag=value shape). Elements that are not flag tokens are still
// individually pattern-scrubbed.
func redactFlagArgs(s []any) []any {
	out := make([]any, len(s))
	skipNext := false
	for i, v := range s {
		if skipNext {
			out[i] = redactedPlaceholder
			skipNext = false
			continue
		}
		str, ok := v.(string)
		if !ok {
			out[i] = redactAny("", v)
			continue
		}
		m := reFlagName.FindStringSubmatch(str)
		if m == nil {
			out[i] = Path(String(str))
			continue
		}
		name, inlineSuffix := m[1], m[2]
		if !IsSecretKey(name) {
			out[i] = Path(String(str))
			continue
		}
		if inlineSuffix != "" {
			prefixLen := len(str) - len(inlineSuffix)
			out[i] = str[:prefixLen] + "=" + redactedPlaceholder
			continue
		}
		out[i] = str // the flag name itself is not a secret
		skipNext = true
	}
	return out
}
