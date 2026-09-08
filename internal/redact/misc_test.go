package redact

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPathRewritesHomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		t.Skip("no usable home directory in this environment")
	}
	input := "loaded config from " + home + "/.pando/config.toml"
	want := "loaded config from ~/.pando/config.toml"
	if got := Path(input); got != want {
		t.Errorf("Path(%q) = %q, want %q", input, got, want)
	}

	untouched := "no home dir reference here"
	if got := Path(untouched); got != untouched {
		t.Errorf("Path(%q) = %q, want unchanged", untouched, got)
	}
}

func TestValueRedactsSecretKeysRecursively(t *testing.T) {
	tree := map[string]any{
		"apiKey": "sk-abcdefghijklmnop",
		"nested": map[string]any{
			"password": "hunter2",
			"keep":     "fine",
		},
		"list": []any{
			map[string]any{"token": "tok-abcd"},
			"plain string",
		},
		"count": 42,
	}

	got := Value("", tree).(map[string]any)

	if got["apiKey"] != "[REDACTED]" {
		t.Errorf("apiKey = %v, want [REDACTED]", got["apiKey"])
	}
	nested := got["nested"].(map[string]any)
	if nested["password"] != "[REDACTED]" {
		t.Errorf("nested.password = %v, want [REDACTED]", nested["password"])
	}
	if nested["keep"] != "fine" {
		t.Errorf("nested.keep = %v, want unchanged", nested["keep"])
	}
	list := got["list"].([]any)
	if m := list[0].(map[string]any); m["token"] != "[REDACTED]" {
		t.Errorf("list[0].token = %v, want [REDACTED]", m["token"])
	}
	if list[1] != "plain string" {
		t.Errorf("list[1] = %v, want unchanged", list[1])
	}
	if got["count"] != 42 {
		t.Errorf("count = %v, want unchanged", got["count"])
	}
}

func TestValueScrubsStringPatternsEvenUnderSafeKeys(t *testing.T) {
	tree := map[string]any{
		"message": "connecting with Authorization: Bearer abcDEF123456",
	}
	got := Value("", tree).(map[string]any)
	if strings.Contains(got["message"].(string), "abcDEF123456") {
		t.Errorf("message still contains the raw token: %v", got["message"])
	}
}

// TestValueHandlesHTTPHeader verifies Value walks an http.Header (a typed
// map[string][]string, not map[string]any) via the JSON round-trip fallback
// and redacts every credential-named header whole.
func TestValueHandlesHTTPHeader(t *testing.T) {
	h := http.Header{
		"Authorization": {"Bearer abcdefghijklmnop"},
		"X-Api-Key":     {"plainkey123"},
		"Content-Type":  {"application/json"},
	}
	got, ok := Value("headers", h).(map[string]any)
	if !ok {
		t.Fatalf("Value(http.Header) = %T, want map[string]any", Value("headers", h))
	}
	if got["Authorization"] != "[REDACTED]" {
		t.Errorf("Authorization = %v, want [REDACTED]", got["Authorization"])
	}
	if got["X-Api-Key"] != "[REDACTED]" {
		t.Errorf("X-Api-Key = %v, want [REDACTED]", got["X-Api-Key"])
	}
	if ct, _ := got["Content-Type"].([]any); len(ct) != 1 || ct[0] != "application/json" {
		t.Errorf("Content-Type = %v, want unchanged", got["Content-Type"])
	}
}

// TestValueHandlesTypedStringMap verifies a plain map[string]string (not
// map[string]any) is redacted the same way.
func TestValueHandlesTypedStringMap(t *testing.T) {
	m := map[string]string{"api_key": "plainsecret", "name": "pando"}
	got, ok := Value("extra", m).(map[string]any)
	if !ok {
		t.Fatalf("Value(map[string]string) = %T, want map[string]any", Value("extra", m))
	}
	if got["api_key"] != "[REDACTED]" {
		t.Errorf("api_key = %v, want [REDACTED]", got["api_key"])
	}
	if got["name"] != "pando" {
		t.Errorf("name = %v, want unchanged", got["name"])
	}
}

// TestValueHandlesStruct verifies an arbitrary struct (not a pre-decoded
// JSON tree) is redacted field-by-field via the JSON round trip.
func TestValueHandlesStruct(t *testing.T) {
	type provCfg struct {
		Name   string
		APIKey string
	}
	got, ok := Value("provider", provCfg{Name: "x", APIKey: "structsecret"}).(map[string]any)
	if !ok {
		t.Fatalf("Value(struct) = %T, want map[string]any", Value("provider", provCfg{}))
	}
	if got["APIKey"] != "[REDACTED]" {
		t.Errorf("APIKey = %v, want [REDACTED]", got["APIKey"])
	}
	if got["Name"] != "x" {
		t.Errorf("Name = %v, want unchanged", got["Name"])
	}
}

// TestValueHandlesPointerToStruct verifies a pointer round-trips the same
// as its pointee (and a nil pointer becomes nil, not a panic).
func TestValueHandlesPointerToStruct(t *testing.T) {
	type creds struct{ Token string }
	got, ok := Value("", &creds{Token: "abc"}).(map[string]any)
	if !ok {
		t.Fatalf("Value(*struct) = %T, want map[string]any", Value("", &creds{}))
	}
	if got["Token"] != "[REDACTED]" {
		t.Errorf("Token = %v, want [REDACTED]", got["Token"])
	}

	var nilPtr *creds
	if got := Value("", nilPtr); got != nil {
		t.Errorf("Value(nil *struct) = %v, want nil", got)
	}
}

// TestValueBytesNeverBase64Encoded verifies []byte becomes a byte-count
// marker, never the base64 string encoding/json would otherwise produce
// (which would just be the secret content re-encoded, not redacted).
func TestValueBytesNeverBase64Encoded(t *testing.T) {
	got := Value("raw", []byte("password=hunter2"))
	s, ok := got.(string)
	if !ok {
		t.Fatalf("Value([]byte) = %T, want string", got)
	}
	if strings.Contains(s, "password") || strings.Contains(s, "hunter2") {
		t.Errorf("Value([]byte) leaked content: %q", s)
	}
	if s != "[16 bytes]" {
		t.Errorf("Value([]byte) = %q, want a byte-count marker", s)
	}
}

// TestValueHandlesError verifies an error value is redacted via its
// message text (pattern-scrubbed), not silently dropped to "{}" the way a
// bare json.Marshal(error) would.
func TestValueHandlesError(t *testing.T) {
	err := errors.New("failed with token=zzzsecretzzz")
	got := Value("err", err)
	s, ok := got.(string)
	if !ok {
		t.Fatalf("Value(error) = %T, want string", got)
	}
	if strings.Contains(s, "zzzsecretzzz") {
		t.Errorf("Value(error) leaked the token: %q", s)
	}
}

// TestValueRedactsFlagStyleArgs verifies a []string shaped like a CLI
// argument list has the value following a secret-named flag redacted, both
// as a separate token ("--token X") and inline ("--api-key=X").
func TestValueRedactsFlagStyleArgs(t *testing.T) {
	got, ok := Value("args", []string{"--token", "zzzsecretzzz", "--api-key=inlinesecret", "--verbose"}).([]any)
	if !ok {
		t.Fatalf("Value([]string) = %T, want []any", Value("args", []string{}))
	}
	want := []any{"--token", redactedPlaceholder, "--api-key=" + redactedPlaceholder, "--verbose"}
	if len(got) != len(want) {
		t.Fatalf("Value(flag args) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Value(flag args)[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("short", 100); got != "short" {
		t.Errorf("Truncate under limit = %q, want unchanged", got)
	}
	if got := Truncate("anything", 0); got != "" {
		t.Errorf("Truncate with max<=0 = %q, want empty", got)
	}

	long := strings.Repeat("a", 20)
	got := Truncate(long, 10)
	if !strings.HasSuffix(got, truncatedSuffix) {
		t.Errorf("Truncate(long) = %q, want suffix %q", got, truncatedSuffix)
	}
	if len(got) > 10+len(truncatedSuffix) {
		t.Errorf("Truncate(long) length = %d, too long", len(got))
	}

	// Rune-safety: cutting mid multi-byte rune must not corrupt UTF-8.
	multibyte := strings.Repeat("é", 10) // each 'é' is 2 bytes in UTF-8
	got = Truncate(multibyte, 5)         // odd byte budget forces a mid-rune cut
	if !strings.HasSuffix(got, truncatedSuffix) {
		t.Errorf("Truncate(multibyte) = %q, want suffix %q", got, truncatedSuffix)
	}
	body := strings.TrimSuffix(got, truncatedSuffix)
	if !utf8.ValidString(body) {
		t.Errorf("Truncate(multibyte) produced invalid UTF-8: %q", body)
	}
}
