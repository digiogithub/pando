package design

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// Critique policies. A policy decides how hard the gate is, not what the audit
// looks for: every rule runs under every policy, so the issue list a user reads
// is the same one either way.
const (
	// PolicyNone scores and reports but never blocks. It is for artifacts
	// where the brief is exploratory and an iteration budget is waste.
	PolicyNone = "none"
	// PolicyStandard gates on the score alone.
	PolicyStandard = "standard"
	// PolicyStrict also refuses to pass while any error-level finding remains,
	// and holds the score to a higher bar.
	PolicyStrict = "strict"
)

// strictThreshold is the floor PolicyStrict imposes. A project may configure a
// higher one; it may not configure its way below this while asking for strict.
const strictThreshold = 9.0

const (
	defaultCritiqueMaxRounds = 3
	defaultCritiqueThreshold = 8.0
	maxCritiqueRounds        = 10
)

// CritiqueSettings bounds one designer/critic loop.
type CritiqueSettings struct {
	Enabled   bool    `json:"enabled"`
	MaxRounds int     `json:"max_rounds"`
	Threshold float64 `json:"threshold"`
	Policy    string  `json:"policy"`
}

// DefaultCritiqueSettings reads the configured bounds, falling back to the
// package defaults when configuration has not been loaded — the CLI and the
// tests both reach the gate without a config in some paths, and a gate that
// panics there is worse than one that uses its defaults.
func DefaultCritiqueSettings() CritiqueSettings {
	settings := CritiqueSettings{
		Enabled:   true,
		MaxRounds: defaultCritiqueMaxRounds,
		Threshold: defaultCritiqueThreshold,
		Policy:    PolicyStandard,
	}
	cfg := config.Get()
	if cfg == nil {
		return settings
	}
	critique := cfg.Design.Critique
	settings.Enabled = critique.Enabled
	if critique.MaxRounds > 0 {
		settings.MaxRounds = critique.MaxRounds
	}
	if critique.Threshold > 0 {
		settings.Threshold = critique.Threshold
	}
	if policy := NormalizePolicy(critique.Policy); policy != "" {
		settings.Policy = policy
	}
	return settings.normalized()
}

// NormalizePolicy maps a written policy onto a known one, returning "" for
// anything unrecognised so the caller can keep its own default rather than
// silently running a policy nobody asked for.
func NormalizePolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case PolicyNone, "off", "disabled":
		return PolicyNone
	case PolicyStandard, "default", "normal":
		return PolicyStandard
	case PolicyStrict:
		return PolicyStrict
	default:
		return ""
	}
}

// WithPolicy applies a per-skill override (od.critique.policy). An unknown or
// empty policy leaves the settings untouched: a template is allowed to say
// nothing about critique.
func (s CritiqueSettings) WithPolicy(policy string) CritiqueSettings {
	if resolved := NormalizePolicy(policy); resolved != "" {
		s.Policy = resolved
	}
	return s.normalized()
}

func (s CritiqueSettings) normalized() CritiqueSettings {
	// A policy nobody recognises is not a policy: fall back rather than run a
	// gate whose behaviour is undefined.
	if resolved := NormalizePolicy(s.Policy); resolved != "" {
		s.Policy = resolved
	} else {
		s.Policy = PolicyStandard
	}
	if s.MaxRounds <= 0 {
		s.MaxRounds = defaultCritiqueMaxRounds
	}
	if s.MaxRounds > maxCritiqueRounds {
		s.MaxRounds = maxCritiqueRounds
	}
	if s.Threshold <= 0 {
		s.Threshold = defaultCritiqueThreshold
	}
	if s.Threshold > 10 {
		s.Threshold = 10
	}
	if s.Policy == PolicyStrict && s.Threshold < strictThreshold {
		s.Threshold = strictThreshold
	}
	return s
}

// GateDecision is the answer to the only question the loop asks: iterate again,
// or stop. It carries the numbers behind the answer so a surface can show why
// without recomputing them.
type GateDecision struct {
	// Pass is true when this version meets the bar.
	Pass bool `json:"pass"`
	// Iterate is true when the loop should produce another version. It is not
	// simply !Pass: a run that has spent its rounds stops without passing.
	Iterate bool `json:"iterate"`
	// Reason is one sentence for a human and for the agent transcript.
	Reason    string  `json:"reason"`
	Round     int     `json:"round"`
	MaxRounds int     `json:"max_rounds"`
	Score     float64 `json:"score"`
	Threshold float64 `json:"threshold"`
	Policy    string  `json:"policy"`
	// Blocking counts the error- and blocking-severity findings that remain,
	// which is what a strict policy refuses to pass with.
	Blocking int `json:"blocking"`
}

// Gate decides whether the loop stops after this critique. round is 1-based:
// the critique of the first version is round 1.
func (s CritiqueSettings) Gate(c Critique, round int) GateDecision {
	s = s.normalized()
	if round < 1 {
		round = 1
	}
	blocking := 0
	for _, issue := range c.Issues {
		if issue.Severity == SeverityError || issue.Severity == SeverityBlocking {
			blocking++
		}
	}
	decision := GateDecision{
		Round:     round,
		MaxRounds: s.MaxRounds,
		Score:     c.Score,
		Threshold: s.Threshold,
		Policy:    s.Policy,
		Blocking:  blocking,
	}

	if !s.Enabled || s.Policy == PolicyNone {
		decision.Pass = true
		decision.Reason = fmt.Sprintf("critique policy %q does not gate: scored %.1f/10, %d finding(s) reported",
			s.Policy, c.Score, len(c.Issues))
		if !s.Enabled {
			decision.Reason = fmt.Sprintf("the critic loop is disabled: scored %.1f/10, %d finding(s) reported",
				c.Score, len(c.Issues))
		}
		return decision
	}

	switch {
	case c.Score < s.Threshold:
		decision.Reason = fmt.Sprintf("scored %.1f/10, below the %.1f threshold", c.Score, s.Threshold)
	case s.Policy == PolicyStrict && blocking > 0:
		decision.Reason = fmt.Sprintf("scored %.1f/10 but %d error-level finding(s) remain, and the strict policy passes none",
			c.Score, blocking)
	default:
		decision.Pass = true
		decision.Reason = fmt.Sprintf("scored %.1f/10, at or above the %.1f threshold", c.Score, s.Threshold)
	}

	if !decision.Pass {
		if round >= s.MaxRounds {
			decision.Reason += fmt.Sprintf("; round %d of %d is the last, so the loop stops here",
				round, s.MaxRounds)
		} else {
			decision.Iterate = true
			decision.Reason += fmt.Sprintf("; round %d of %d, fix the findings and commit another version",
				round, s.MaxRounds)
		}
	}
	return decision
}

// MergeIssues folds a critic's own findings into the deterministic ones,
// dropping a critic finding that only restates a rule already fired on the same
// node. The audit is the evidence; the critic adds judgement, not an echo.
func MergeIssues(audited, written []Issue) []Issue {
	seen := make(map[string]struct{}, len(audited))
	for _, issue := range audited {
		seen[issueKey(issue)] = struct{}{}
	}
	merged := append([]Issue(nil), audited...)
	for _, issue := range written {
		if strings.TrimSpace(issue.Message) == "" {
			continue
		}
		if issue.Severity == "" {
			issue.Severity = SeverityWarning
		}
		key := issueKey(issue)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, issue)
	}
	sortIssues(merged)
	return merged
}

func issueKey(issue Issue) string {
	return strings.ToLower(strings.TrimSpace(issue.Code)) + "|" +
		issue.NodeID + "|" +
		strings.ToLower(strings.TrimSpace(issue.Message))
}

// BlendScore combines the deterministic audit score with the critic's own. The
// deterministic pass cannot see whether a layout reads well and the critic
// cannot count contrast failures reliably, so neither is allowed to be the
// whole verdict. A critic that offers no score leaves the audit score standing.
func BlendScore(audited, written float64) float64 {
	if written <= 0 {
		return audited
	}
	if written > 10 {
		written = 10
	}
	// The audit is weighted slightly higher because it is reproducible: two
	// runs over the same files return the same number.
	blended := audited*0.6 + written*0.4
	return math.Round(blended*10) / 10
}

// CountsBySeverity summarises an issue list for a header line.
func CountsBySeverity(issues []Issue) map[string]int {
	counts := map[string]int{}
	for _, issue := range issues {
		severity := issue.Severity
		if severity == "" {
			severity = SeverityWarning
		}
		counts[severity]++
	}
	return counts
}

// RuleCodes lists the rules that fired, worst-severity first, for a compact
// one-line summary.
func RuleCodes(issues []Issue) []string {
	seen := map[string]struct{}{}
	codes := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.Code == "" {
			continue
		}
		if _, ok := seen[issue.Code]; ok {
			continue
		}
		seen[issue.Code] = struct{}{}
		codes = append(codes, issue.Code)
	}
	sort.Strings(codes)
	return codes
}
