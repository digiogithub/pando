package toonconv_test

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/toonconv"
)

func TestConvertIfJSON_EmptyString(t *testing.T) {
	result := toonconv.ConvertIfJSON("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestConvertIfJSON_PlainText(t *testing.T) {
	input := "This is plain text output"
	result := toonconv.ConvertIfJSON(input)
	if result != input {
		t.Errorf("plain text should be unchanged, got %q", result)
	}
}

func TestConvertIfJSON_Markdown(t *testing.T) {
	input := "## Header\n\n- item 1\n- item 2"
	result := toonconv.ConvertIfJSON(input)
	if result != input {
		t.Errorf("markdown should be unchanged, got %q", result)
	}
}

func TestConvertIfJSON_InvalidJSON(t *testing.T) {
	input := `{"key": value_without_quotes}`
	result := toonconv.ConvertIfJSON(input)
	if result != input {
		t.Errorf("invalid JSON should be unchanged, got %q", result)
	}
}

func TestConvertIfJSON_SimpleObject(t *testing.T) {
	input := `{"name":"Ada","active":true}`
	result := toonconv.ConvertIfJSON(input)
	if result == input {
		t.Error("valid JSON object should be converted to TOON")
	}
	// Keys are sorted alphabetically in TOON output
	if !strings.Contains(result, "name: Ada") {
		t.Errorf("expected TOON format with 'name: Ada', got %q", result)
	}
	if !strings.Contains(result, "active: true") {
		t.Errorf("expected TOON format with 'active: true', got %q", result)
	}
}

func TestConvertIfJSON_NestedObject(t *testing.T) {
	input := `{"user":{"id":123,"name":"Bob"}}`
	result := toonconv.ConvertIfJSON(input)
	if result == input {
		t.Error("valid JSON should be converted to TOON")
	}
	// TOON uses indented key: value instead of braces
	if strings.Contains(result, "{") || strings.Contains(result, "}") {
		t.Errorf("TOON output should not contain JSON braces, got %q", result)
	}
	if !strings.Contains(result, "user:") {
		t.Errorf("expected 'user:' section in TOON output, got %q", result)
	}
	if !strings.Contains(result, "name: Bob") {
		t.Errorf("expected 'name: Bob' in TOON output, got %q", result)
	}
}

func TestConvertIfJSON_Array(t *testing.T) {
	// Array with length markers: [#3]: 1,2,3
	input := `[1,2,3]`
	result := toonconv.ConvertIfJSON(input)
	if result == input {
		t.Error("valid JSON array should be converted to TOON")
	}
	// TOON array format with length marker: [#3]: 1,2,3
	if !strings.Contains(result, "#3") {
		t.Errorf("expected length marker '#3' in TOON array output, got %q", result)
	}
}

func TestConvertIfJSON_NullValue(t *testing.T) {
	input := `null`
	result := toonconv.ConvertIfJSON(input)
	// null is valid JSON — TOON renders it as "null"
	if result == "" {
		t.Error("should return non-empty result for null JSON")
	}
	if result != "null" {
		t.Errorf("expected 'null', got %q", result)
	}
}

func TestConvertIfJSON_StringValue(t *testing.T) {
	// A JSON string primitive like `"hello"` is valid JSON
	// TOON renders it without quotes: hello
	input := `"hello"`
	result := toonconv.ConvertIfJSON(input)
	if result == input {
		t.Errorf("JSON string primitive should be converted, got %q", result)
	}
	if result != "hello" {
		t.Errorf("expected 'hello' without quotes, got %q", result)
	}
}

func TestConvertIfJSON_ObjectArray(t *testing.T) {
	// Array of uniform objects → TOON tabular format: [#2]{id,name}:\n  1,Alice\n  2,Bob
	input := `[{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]`
	result := toonconv.ConvertIfJSON(input)
	if result == input {
		t.Error("JSON array of objects should be converted to TOON")
	}
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "Bob") {
		t.Errorf("TOON output should contain the values, got %q", result)
	}
	// Tabular TOON format uses column headers
	if !strings.Contains(result, "id") || !strings.Contains(result, "name") {
		t.Errorf("TOON tabular output should contain column names, got %q", result)
	}
}

func TestConvertIfJSON_ConcurrentSafe(t *testing.T) {
	input := `{"key":"value"}`
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			result := toonconv.ConvertIfJSON(input)
			if result == "" {
				t.Error("unexpected empty result in concurrent call")
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
