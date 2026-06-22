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
// It returns (conclusion, true) when a block was found, (nil, false) otherwise.
func Parse(raw string) (*models.Conclusion, bool) {
	inner, ok := lastBlock(raw)
	if !ok {
		return nil, false
	}

	c := &models.Conclusion{}

	var body rawConclusion
	if err := yaml.Unmarshal([]byte(inner), &body); err != nil {
		// Tolerant fallback: the block exists but the body is not valid YAML.
		// Salvage the raw inner text as the summary so the conclusion is not lost.
		c.Summary = strings.TrimSpace(inner)
		return c, true
	}

	c.Status = normalizeStatus(body.Status)
	c.Summary = strings.TrimSpace(body.Summary)
	c.Artifacts = body.Artifacts
	c.MemoryRefs = body.MemoryRefs
	c.FollowUp = strings.TrimSpace(body.FollowUp)
	c.Confidence = clampConfidence(body.Confidence)

	// If YAML parsed but produced no usable content, salvage the raw inner text
	// (e.g. the body was a bare string, not a mapping).
	if c.Summary == "" && c.Status == "" && len(c.Artifacts) == 0 &&
		len(c.MemoryRefs) == 0 && c.FollowUp == "" {
		c.Summary = strings.TrimSpace(inner)
	}

	return c, true
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
