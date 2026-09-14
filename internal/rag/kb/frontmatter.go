package kb

import (
	"fmt"
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
	// Aliases are extra names a document can be addressed by from a [[wiki link]],
	// on top of its path and basename.
	Aliases []string `yaml:"aliases,omitempty"`
	// Memory fields — only serialized when non-zero/non-empty
	Key        string     `yaml:"key,omitempty"`
	Scope      string     `yaml:"scope,omitempty"`
	Outdated   bool       `yaml:"outdated,omitempty"`
	ExpiresAt  *time.Time `yaml:"expires_at,omitempty"`
	Hits       int        `yaml:"hits,omitempty"`
	Importance float64    `yaml:"importance,omitempty"`
	Source     string     `yaml:"source,omitempty"`
}

// MemoryOptions carries optional memory-layer settings for NewFrontMatter.
type MemoryOptions struct {
	Key        string
	Scope      string
	Source     string
	Importance float64
	TTLDays    int // 0 = no expiry
}

// ParseFrontMatter splits YAML front matter from the body of a document.
// If no valid front matter is found, it returns a zero FrontMatter and the
// original content as body.
func ParseFrontMatter(raw string) (FrontMatter, string, error) {
	fm, _, body, err := ParseFrontMatterWithRaw(raw)
	return fm, body, err
}

// ParseFrontMatterWithRaw splits YAML front matter from the body of a document,
// like ParseFrontMatter, but also unmarshals the same YAML block a second time
// into a generic map so callers can recover front-matter keys the typed
// FrontMatter struct does not name (e.g. host-defined fields such as "status"
// or "project"). If no valid front matter is found, it returns a zero
// FrontMatter, a nil map, and the original content as body.
func ParseFrontMatterWithRaw(raw string) (FrontMatter, map[string]interface{}, string, error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, frontMatterDelimiter) {
		return FrontMatter{}, nil, raw, nil
	}

	// Find the closing delimiter after the opening one.
	rest := trimmed[len(frontMatterDelimiter):]
	// The opening delimiter must be followed by a newline.
	nlIdx := strings.Index(rest, "\n")
	if nlIdx == -1 {
		return FrontMatter{}, nil, raw, nil
	}
	// Everything before the newline after "---" should be empty or whitespace.
	if strings.TrimSpace(rest[:nlIdx]) != "" {
		return FrontMatter{}, nil, raw, nil
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
			return FrontMatter{}, nil, raw, nil
		}
		yamlBlock = rest[:closeIdx]
		afterClose = rest[closeIdx+1+len(frontMatterDelimiter):]
		afterClose = strings.TrimPrefix(afterClose, "\n")
	}
	body := afterClose

	var fm FrontMatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return FrontMatter{}, nil, raw, err
	}

	var rawFM map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlBlock), &rawFM); err != nil {
		// The typed unmarshal above already succeeded on the same bytes, so
		// this should not normally happen. Degrade gracefully: keep the
		// typed result and drop only the raw key set rather than failing
		// the whole parse.
		return fm, nil, body, nil
	}

	return fm, rawFM, body, nil
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

	// Same rule for aliases.
	if len(incoming.Aliases) > 0 {
		merged.Aliases = incoming.Aliases
	} else {
		merged.Aliases = existing.Aliases
	}

	// Preserve identity fields from existing when caller doesn't supply them.
	if incoming.Key != "" {
		merged.Key = incoming.Key
	} else {
		merged.Key = existing.Key
	}
	if incoming.Scope != "" {
		merged.Scope = incoming.Scope
	} else {
		merged.Scope = existing.Scope
	}
	if incoming.Source != "" {
		merged.Source = incoming.Source
	} else {
		merged.Source = existing.Source
	}

	// Outdated is managed explicitly by the caller.
	merged.Outdated = incoming.Outdated

	// Hits are managed by the store, never reset by a merge.
	merged.Hits = existing.Hits

	// ExpiresAt: keep existing when incoming doesn't specify.
	if incoming.ExpiresAt != nil {
		merged.ExpiresAt = incoming.ExpiresAt
	} else {
		merged.ExpiresAt = existing.ExpiresAt
	}

	// Importance: use incoming when caller supplies a value > 0.
	if incoming.Importance > 0 {
		merged.Importance = incoming.Importance
	} else {
		merged.Importance = existing.Importance
	}

	return merged
}

// NewFrontMatter creates a fresh FrontMatter with created_at/updated_at set to now.
// When opts is non-nil, memory fields are populated from it.
func NewFrontMatter(tags []string, opts *MemoryOptions) FrontMatter {
	now := time.Now().UTC()
	fm := FrontMatter{
		CreatedAt: now,
		UpdatedAt: now,
		Tags:      tags,
	}
	if opts != nil {
		fm.Key = opts.Key
		fm.Scope = opts.Scope
		fm.Source = opts.Source
		if opts.Importance > 0 {
			fm.Importance = opts.Importance
		}
		if opts.TTLDays > 0 {
			exp := now.AddDate(0, 0, opts.TTLDays)
			fm.ExpiresAt = &exp
		}
	}
	return fm
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
	return stringSliceFromMetadata(meta, "tags")
}

// InjectTagsIntoMetadata ensures tags are stored in the metadata map under "tags".
// If tags is nil/empty and no existing tags, the key is left absent.
func InjectTagsIntoMetadata(meta map[string]interface{}, tags []string) map[string]interface{} {
	return injectStringSliceIntoMetadata(meta, "tags", tags)
}

// ExtractAliasesFromMetadata reads the "aliases" key from a metadata map.
// Aliases are the extra slugs a document can be reached by from a [[wiki link]].
func ExtractAliasesFromMetadata(meta map[string]interface{}) []string {
	return stringSliceFromMetadata(meta, "aliases")
}

// InjectAliasesIntoMetadata stores aliases in the metadata map under "aliases"
// so link resolution can read them without re-parsing the document body.
func InjectAliasesIntoMetadata(meta map[string]interface{}, aliases []string) map[string]interface{} {
	return injectStringSliceIntoMetadata(meta, "aliases", aliases)
}

// stringSliceFromMetadata reads a string list stored under key. Metadata round-trips
// through JSON, so the value may come back as []interface{} instead of []string.
func stringSliceFromMetadata(meta map[string]interface{}, key string) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				items = append(items, s)
			}
		}
		if len(items) == 0 {
			return nil
		}
		return items
	}
	return nil
}

func injectStringSliceIntoMetadata(meta map[string]interface{}, key string, values []string) map[string]interface{} {
	if meta == nil {
		meta = make(map[string]interface{})
	}
	if len(values) > 0 {
		meta[key] = values
	}
	return meta
}

// reservedFrontMatterKeys are front-matter keys that already have typed
// handling elsewhere: the sync-owned metadata fields, tags/aliases injection,
// and the memory-layer fields on FrontMatter. A key in this set is never
// copied from a document's raw front matter into its metadata, so a
// host-written front-matter key can never shadow the authoritative value
// already stored under that name.
var reservedFrontMatterKeys = map[string]struct{}{
	"source_path":       {},
	"source_mtime_unix": {},
	"source_format":     {},
	"converted":         {},
	"tags":              {},
	"aliases":           {},
	"created_at":        {},
	"updated_at":        {},
	// Memory fields.
	"key":        {},
	"scope":      {},
	"source":     {},
	"outdated":   {},
	"expires_at": {},
	"hits":       {},
	"importance": {},
}

// MergeUnknownFrontMatterKeys copies every key from rawFM into meta that is
// not part of reservedFrontMatterKeys, so host-defined front-matter fields
// (e.g. "status", "project", "milestone") survive into a document's stored
// metadata instead of being silently dropped by the typed FrontMatter struct.
// Values that aren't JSON-representable scalars, lists or maps are
// stringified, since metadata is marshalled to JSON when persisted.
func MergeUnknownFrontMatterKeys(meta map[string]interface{}, rawFM map[string]interface{}) map[string]interface{} {
	if len(rawFM) == 0 {
		return meta
	}
	if meta == nil {
		meta = make(map[string]interface{})
	}
	for k, v := range rawFM {
		if _, reserved := reservedFrontMatterKeys[k]; reserved {
			continue
		}
		meta[k] = jsonSafeValue(v)
	}
	return meta
}

// jsonSafeValue recursively normalizes a YAML-decoded value so it marshals
// cleanly to JSON: nested mappings become map[string]interface{}, slices are
// walked element-wise, and any value JSON cannot represent natively (e.g. a
// time.Time or other exotic scalar) is stringified rather than dropped.
func jsonSafeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case nil, string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return val
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = jsonSafeValue(item)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[k] = jsonSafeValue(item)
		}
		return out
	case map[interface{}]interface{}:
		// yaml.v3 decodes nested mappings as map[string]interface{} when the
		// keys are strings, but fall back to stringifying keys defensively.
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[fmt.Sprintf("%v", k)] = jsonSafeValue(item)
		}
		return out
	default:
		return fmt.Sprintf("%v", val)
	}
}
