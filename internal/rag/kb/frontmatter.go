package kb

import (
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	frontMatterDelimiter = "---"
	timeLayout           = time.RFC3339
)

// FrontMatter holds the YAML front matter fields for KB documents.
type FrontMatter struct {
	CreatedAt time.Time `yaml:"created_at,omitempty"`
	UpdatedAt time.Time `yaml:"updated_at,omitempty"`
	Tags      []string  `yaml:"tags,omitempty"`
}

// ParseFrontMatter splits YAML front matter from the body of a document.
// If no valid front matter is found, it returns a zero FrontMatter and the
// original content as body.
func ParseFrontMatter(raw string) (FrontMatter, string, error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, frontMatterDelimiter) {
		return FrontMatter{}, raw, nil
	}

	// Find the closing delimiter after the opening one.
	rest := trimmed[len(frontMatterDelimiter):]
	// The opening delimiter must be followed by a newline.
	nlIdx := strings.Index(rest, "\n")
	if nlIdx == -1 {
		return FrontMatter{}, raw, nil
	}
	// Everything before the newline after "---" should be empty or whitespace.
	if strings.TrimSpace(rest[:nlIdx]) != "" {
		return FrontMatter{}, raw, nil
	}
	rest = rest[nlIdx+1:]

	// Handle case where closing delimiter is at the very start (empty YAML block).
	var yamlBlock, afterClose string
	if strings.HasPrefix(rest, frontMatterDelimiter+"\n") || rest == frontMatterDelimiter {
		yamlBlock = ""
		afterClose = strings.TrimPrefix(rest[len(frontMatterDelimiter):], "\n")
	} else {
		closeIdx := strings.Index(rest, "\n"+frontMatterDelimiter)
		if closeIdx == -1 {
			return FrontMatter{}, raw, nil
		}
		yamlBlock = rest[:closeIdx]
		afterClose = rest[closeIdx+1+len(frontMatterDelimiter):]
		afterClose = strings.TrimPrefix(afterClose, "\n")
	}
	body := afterClose

	var fm FrontMatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return FrontMatter{}, raw, err
	}

	return fm, body, nil
}

// SerializeFrontMatter produces a document string with YAML front matter
// prepended to the body content.
func SerializeFrontMatter(fm FrontMatter, body string) string {
	var sb strings.Builder

	sb.WriteString(frontMatterDelimiter)
	sb.WriteString("\n")

	yamlBytes, err := yaml.Marshal(&fm)
	if err != nil {
		// Fallback: return body without front matter on marshal error.
		return body
	}
	sb.Write(yamlBytes)

	sb.WriteString(frontMatterDelimiter)
	sb.WriteString("\n")

	if body != "" {
		sb.WriteString(body)
	}

	return sb.String()
}

// MergeFrontMatter combines existing and incoming front matter.
// It preserves created_at from existing, sets updated_at to now,
// and replaces tags with new if provided (otherwise keeps existing).
func MergeFrontMatter(existing, incoming FrontMatter) FrontMatter {
	merged := FrontMatter{
		CreatedAt: existing.CreatedAt,
		UpdatedAt: time.Now().UTC(),
	}

	// Preserve original creation time; if missing, use now.
	if merged.CreatedAt.IsZero() {
		merged.CreatedAt = merged.UpdatedAt
	}

	// If incoming has tags, use them; otherwise keep existing.
	if len(incoming.Tags) > 0 {
		merged.Tags = incoming.Tags
	} else {
		merged.Tags = existing.Tags
	}

	return merged
}

// NewFrontMatter creates a fresh FrontMatter with created_at/updated_at set to now.
func NewFrontMatter(tags []string) FrontMatter {
	now := time.Now().UTC()
	return FrontMatter{
		CreatedAt: now,
		UpdatedAt: now,
		Tags:      tags,
	}
}

// StripFrontMatter removes any YAML front matter from raw content and returns
// only the body. This is a convenience wrapper around ParseFrontMatter.
func StripFrontMatter(raw string) string {
	_, body, err := ParseFrontMatter(raw)
	if err != nil {
		return raw
	}
	return body
}

// ExtractTagsFromMetadata reads the "tags" key from a metadata map and returns
// them as a string slice. Returns nil if no tags are present.
func ExtractTagsFromMetadata(meta map[string]interface{}) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta["tags"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		tags := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				tags = append(tags, s)
			}
		}
		if len(tags) == 0 {
			return nil
		}
		return tags
	}
	return nil
}

// InjectTagsIntoMetadata ensures tags are stored in the metadata map under "tags".
// If tags is nil/empty and no existing tags, the key is left absent.
func InjectTagsIntoMetadata(meta map[string]interface{}, tags []string) map[string]interface{} {
	if meta == nil {
		meta = make(map[string]interface{})
	}
	if len(tags) > 0 {
		meta["tags"] = tags
	}
	return meta
}
