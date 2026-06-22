// Package conclusion implements the delegated-task conclusion protocol: parsing
// the sentinel <pando:conclusion> block emitted by a subagent and enriching it
// with software-owned launch metadata.
//
// The package intentionally depends only on the domain models (plus stdlib and a
// YAML parser) so it can be reused by the orchestrator and app wiring without
// creating import cycles with internal/config or internal/project. Config and
// project resolution are threaded in via plain function/struct values.
package conclusion

import (
	"fmt"
	"strings"

	"github.com/digiogithub/pando/pkg/mesnada/models"
	"gopkg.in/yaml.v3"
)

const (
	// openTag and closeTag delimit the conclusion block in a subagent's output.
	openTag  = "<pando:conclusion>"
	closeTag = "</pando:conclusion>"
)

// rawConclusion mirrors the YAML body of the sentinel block. Only the
// model-known fields are accepted; software-owned metadata is filled by the
// enricher. All fields are optional and tolerant of being absent.
type rawConclusion struct {
	Status     string   `yaml:"status"`
	Summary    string   `yaml:"summary"`
	Artifacts  []string `yaml:"artifacts"`
	MemoryRefs []string `yaml:"memory_refs"`
	FollowUp   string   `yaml:"follow_up"`
	Confidence float64  `yaml:"confidence"`
}

// Parse scans raw for the sentinel block delimited by <pando:conclusion> ...
// </pando:conclusion>. When multiple blocks are present the LAST one is used (it
// is the final summary). The inner body is parsed as YAML; parsing is tolerant:
// missing fields default to their zero value, and an unparseable body with a
// present sentinel still returns a Conclusion whose Summary is the salvaged raw
// inner text.
//
// Soft-schema validation accumulates human-readable warnings in Conclusion.Warnings
// describing what was malformed/missing/out-of-range. Data is never discarded due
// to warnings — the principle is warn, don't discard.
//
// It returns (conclusion, true) when a block was found, (nil, false) otherwise.
func Parse(raw string) (*models.Conclusion, bool) {
	inner, ok := lastBlock(raw)
	if !ok {
		return nil, false
	}

	c := &models.Conclusion{}
	var warns []string

	var body rawConclusion
	if err := yaml.Unmarshal([]byte(inner), &body); err != nil {
		// Tolerant fallback: the block exists but the body is not valid YAML.
		// Salvage the raw inner text as the summary so the conclusion is not lost.
		c.Summary = strings.TrimSpace(inner)
		warns = append(warns, "conclusion body was not valid YAML; salvaged raw text")
		c.Warnings = warns
		return c, true
	}

	// Validate and assign status.
	rawStatus := strings.TrimSpace(body.Status)
	c.Status = normalizeStatus(rawStatus)
	if rawStatus != "" && c.Status == "" {
		warns = append(warns, fmt.Sprintf("unknown status %q; left for software default", rawStatus))
	}

	// Validate and assign confidence.
	if body.Confidence < 0 || body.Confidence > 1 {
		warns = append(warns, fmt.Sprintf("confidence %v out of range [0,1]; clamped", body.Confidence))
	}
	c.Confidence = clampConfidence(body.Confidence)

	c.Summary = strings.TrimSpace(body.Summary)
	c.FollowUp = strings.TrimSpace(body.FollowUp)

	// Filter blank entries from Artifacts and MemoryRefs.
	c.Artifacts, warns = filterBlanks(body.Artifacts, "artifacts", warns)
	c.MemoryRefs, warns = filterBlanks(body.MemoryRefs, "memory_refs", warns)

	// If YAML parsed but produced no usable content, salvage the raw inner text
	// (e.g. the body was a bare string, not a mapping).
	if c.Summary == "" && c.Status == "" && len(c.Artifacts) == 0 &&
		len(c.MemoryRefs) == 0 && c.FollowUp == "" {
		c.Summary = strings.TrimSpace(inner)
		warns = append(warns, "conclusion body was not a YAML mapping; salvaged raw text")
	} else if c.Summary == "" {
		// Structured body was parsed but summary is missing.
		warns = append(warns, "missing summary")
	}

	if len(warns) > 0 {
		c.Warnings = warns
	}
	return c, true
}

// filterBlanks removes whitespace-only entries from entries and appends a
// warning to warns when any entries were dropped. It returns the filtered slice
// and the (possibly extended) warns slice.
func filterBlanks(entries []string, field string, warns []string) ([]string, []string) {
	if len(entries) == 0 {
		return entries, warns
	}
	var kept []string
	for _, e := range entries {
		if strings.TrimSpace(e) != "" {
			kept = append(kept, e)
		}
	}
	dropped := len(entries) - len(kept)
	if dropped > 0 {
		noun := "entry"
		if dropped != 1 {
			noun = "entries"
		}
		warns = append(warns, fmt.Sprintf("dropped %d blank %s %s", dropped, field, noun))
	}
	return kept, warns
}

// lastBlock returns the inner body of the LAST sentinel block in raw.
func lastBlock(raw string) (string, bool) {
	closeIdx := strings.LastIndex(raw, closeTag)
	if closeIdx < 0 {
		return "", false
	}
	// Find the opening tag that precedes this closing tag.
	openIdx := strings.LastIndex(raw[:closeIdx], openTag)
	if openIdx < 0 {
		return "", false
	}
	inner := raw[openIdx+len(openTag) : closeIdx]
	return inner, true
}

// normalizeStatus lowercases and trims the status, mapping it to one of the four
// known values. Unknown or empty input returns "" so the enricher can decide a
// default based on the task's terminal state.
func normalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success":
		return "success"
	case "partial":
		return "partial"
	case "failed":
		return "failed"
	case "blocked":
		return "blocked"
	default:
		return ""
	}
}

// clampConfidence constrains a confidence value to the [0,1] range.
func clampConfidence(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}
