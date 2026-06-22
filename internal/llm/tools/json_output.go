package tools

import (
	"bytes"
	"encoding/json"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	toon "github.com/toon-format/toon-go"
)

// NewStructuredResponse formats structured data for tool consumers.
// It prefers TOON output for readability and token efficiency, falls back to
// TOML when TOON encoding fails, and finally falls back to pretty JSON when the
// value cannot be rendered in either structured text format.
func NewStructuredResponse(value any) ToolResponse {
	return NewTextResponse(FormatStructuredData(value))
}

// FormatStructuredData renders structured values as TOON when possible, then
// TOML, and otherwise falls back to indented JSON.
func FormatStructuredData(value any) string {
	normalized, ok := normalizeStructuredValue(value)
	if !ok {
		return formatAsIndentedJSON(value)
	}
	if toonText, ok := tryFormatAsTOON(normalized); ok {
		return toonText
	}
	if tomlText, ok := tryFormatAsTOMLNormalized(normalized); ok {
		return tomlText
	}
	return formatAsIndentedJSON(normalized)
}

// FormatJSONLikeContent attempts to interpret textual content as JSON and render
// it as TOON. If TOON rendering fails, it falls back to TOML and then indented
// JSON. If parsing fails, the original content is returned unchanged.
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

func normalizeStructuredValue(value any) (any, bool) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}

	var normalized any
	if err := json.Unmarshal(jsonBytes, &normalized); err != nil {
		return nil, false
	}
	return normalized, true
}

func tryFormatAsTOON(value any) (string, bool) {
	toonBytes, err := toon.Marshal(value)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(toonBytes)), true
}

func tryFormatAsTOMLNormalized(value any) (string, bool) {
	tomlBytes, err := toml.Marshal(value)
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

	return FormatStructuredData(value)
}
