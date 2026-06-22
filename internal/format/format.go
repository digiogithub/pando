package format

import (
	"fmt"
	"strings"

	llmtools "github.com/digiogithub/pando/internal/llm/tools"
)

// OutputFormat represents the output format type for non-interactive mode
type OutputFormat string

const (
	// Text format outputs the AI response as plain text.
	Text OutputFormat = "text"

	// JSON format outputs the AI response as structured TOON/TOML when possible,
	// falling back to indented JSON when TOON serialization is not possible.
	JSON OutputFormat = "json"
)

// String returns the string representation of the OutputFormat
func (f OutputFormat) String() string {
	return string(f)
}

// SupportedFormats is a list of all supported output formats as strings
var SupportedFormats = []string{
	string(Text),
	string(JSON),
}

// Parse converts a string to an OutputFormat
func Parse(s string) (OutputFormat, error) {
	s = strings.ToLower(strings.TrimSpace(s))

	switch s {
	case string(Text):
		return Text, nil
	case string(JSON):
		return JSON, nil
	default:
		return "", fmt.Errorf("invalid format: %s", s)
	}
}

// IsValid checks if the provided format string is supported
func IsValid(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// GetHelpText returns a formatted string describing all supported formats
func GetHelpText() string {
	return fmt.Sprintf(`Supported output formats:
- %s: Plain text output (default)
- %s: Structured output using TOON/TOML when possible, with JSON fallback`,
		Text, JSON)
}

// FormatOutput formats the AI response according to the specified format
func FormatOutput(content string, formatStr string) string {
	format, err := Parse(formatStr)
	if err != nil {
		// Default to text format on error
		return content
	}

	switch format {
	case JSON:
		return formatAsJSON(content)
	case Text:
		fallthrough
	default:
		return content
	}
}

// formatAsJSON renders the content as TOON/TOML when it is JSON-like and falls
// back to a JSON object wrapper otherwise.
func formatAsJSON(content string) string {
	formatted := llmtools.FormatJSONLikeContent(content)
	if formatted != content {
		return formatted
	}
	return llmtools.FormatStructuredData(map[string]string{"response": content})
}
