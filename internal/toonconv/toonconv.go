// Package toonconv provides utilities for converting JSON tool responses to
// TOON (Token-Oriented Object Notation) format before sending them to the AI
// model. TOON is more token-efficient than JSON for LLM prompts.
package toonconv

import (
	"encoding/json"

	"github.com/toon-format/toon-go"
)

// ConvertIfJSON checks whether content is valid JSON. If it is, it converts
// it to TOON format using toon.MarshalString and returns the TOON string.
// If content is not valid JSON (plain text, markdown, etc.) or if conversion
// fails for any reason, it returns the original content unchanged.
// The function is stateless and safe for concurrent use.
func ConvertIfJSON(content string) string {
	if content == "" {
		return content
	}
	if !json.Valid([]byte(content)) {
		return content
	}
	var data interface{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return content
	}
	out, err := toon.MarshalString(data, toon.WithLengthMarkers(true))
	if err != nil {
		return content
	}
	return out
}
