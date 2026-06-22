package tools

import (
	"bytes"
	"encoding/json"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// NewStructuredResponse formats structured data for tool consumers.
// It prefers TOML output for readability and falls back to pretty JSON when
// TOML encoding fails or the value cannot be decoded as JSON-compatible data.
func NewStructuredResponse(value any) ToolResponse {
	return NewTextResponse(FormatStructuredData(value))
}

// FormatStructuredData renders structured values as TOML when possible and
// otherwise falls back to indented JSON.
func FormatStructuredData(value any) string {
	if tomlText, ok := tryFormatAsTOML(value); ok {
		return tomlText
	}
	return formatAsIndentedJSON(value)
}

// FormatJSONLikeContent attempts to interpret textual content as JSON and render
// it as TOON/TOML. If parsing fails, the original content is returned unchanged.
// If TOON rendering fails after successful JSON parsing, it falls back to
// indented JSON instead of returning raw JSON text directly.
func FormatJSONLikeContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content
	}

	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return content
	}
	return FormatStructuredData(value)
}

func tryFormatAsTOML(value any) (string, bool) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return "", false
	}

	var normalized any
	if err := json.Unmarshal(jsonBytes, &normalized); err != nil {
		return "", false
	}

	tomlBytes, err := toml.Marshal(normalized)
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(tomlBytes)), true
}

func formatAsIndentedJSON(value any) string {
	jsonBytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func tryFormatRawJSONAsStructured(jsonText []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(jsonText))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return string(jsonText)
	}
	if decoder.More() {
		return string(jsonText)
	}

	if tomlText, ok := tryFormatAsTOML(value); ok {
		return tomlText
	}
	return formatAsIndentedJSON(value)
}
