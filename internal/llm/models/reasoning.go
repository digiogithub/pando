package models

import (
	"regexp"
	"strings"
)

// Reasoning-effort value sets, ordered weakest to strongest. They mirror the
// per-model-family reasoning controls opencode models (see
// packages/opencode/src/provider/transform.ts in the opencode project) so that
// Pando never sends an effort value a model rejects — the root cause of issue #6.
//
// Where the models.dev catalog supplies a per-model value set (copied onto
// Model.ReasoningEfforts), that list is authoritative. These tables only back
// models the catalog misses or that advertise no effort-style control.
var (
	widelySupportedEfforts = []string{"low", "medium", "high"}

	openaiGPT51Efforts     = []string{"none", "low", "medium", "high"}
	openaiGPT52PlusEfforts = []string{"none", "low", "medium", "high", "xhigh"}
	openaiGPT5ProEfforts   = []string{"high"}
	openaiGPT5Pro2Plus     = []string{"medium", "high", "xhigh"}
	openaiGPT5ChatEfforts  = []string{"medium"}
	openaiGPT5CodexXHigh   = []string{"low", "medium", "high", "xhigh"}
	openaiGPT5Codex3Plus   = []string{"none", "low", "medium", "high", "xhigh"}

	anthropicAdaptiveEfforts = []string{"low", "medium", "high", "xhigh", "max"}
	anthropic46Efforts       = []string{"low", "medium", "high", "max"}
	anthropic45Efforts       = []string{"low", "medium", "high"}
)

var (
	gpt5VersionRE       = regexp.MustCompile(`(?:^|/)gpt-5[.-](\d+)(?:[.-]|$)`)
	gpt5ProRE           = regexp.MustCompile(`(?:^|/)gpt-5[.-]?pro(?:[.-]|$)`)
	gpt5VersionedProRE  = regexp.MustCompile(`(?:^|/)gpt-5[.-]\d+[.-]pro(?:[.-]|$)`)
	gpt5FamilyRE        = regexp.MustCompile(`(?:^|/)gpt-5(?:[.-]|$)`)
	anthropicOpus47RE   = regexp.MustCompile(`opus-(\d+)[.-](\d+)(?:[.@-]|$)|claude-(\d+)[.-](\d+)-opus(?:[.@-]|$)`)
	anthropicSonnet5RE  = regexp.MustCompile(`sonnet-(\d+)(?:[.@-]|$)|claude-(\d+)-sonnet(?:[.@-]|$)`)
	anthropic46IDRE     = regexp.MustCompile(`(?:opus|sonnet)-4[.-]6|4[.-]6-(?:opus|sonnet)`)
)

// ReasoningEffortsFor returns the ordered reasoning-effort values a model
// accepts. A non-empty list on Model.ReasoningEfforts (from the models.dev
// catalog) is authoritative; otherwise a per-provider/family fallback table is
// consulted. It returns nil when the model's effort set is unknown.
func ReasoningEffortsFor(model Model) []string {
	if len(model.ReasoningEfforts) > 0 {
		return model.ReasoningEfforts
	}
	id := reasoningEffortID(model)
	switch model.Provider {
	case ProviderAnthropic:
		return cloneEfforts(anthropicReasoningEfforts(id))
	case ProviderOpenAI, ProviderCopilot, ProviderAzure, ProviderOpenAICompatible, ProviderOpenRouter:
		return cloneEfforts(openaiReasoningEfforts(id))
	case ProviderGROQ:
		return cloneEfforts([]string{"none", "low", "medium", "high"})
	default:
		return nil
	}
}

// DefaultReasoningEffort returns a reasoning-effort value that is always valid
// for the model: "medium" when acceptable, else "high" (the most common safe
// strong default), else the weakest value the model supports. It never returns a
// value absent from the model's allowed set; for a model with no known set it
// falls back to "medium" to preserve prior behaviour.
func DefaultReasoningEffort(model Model) string {
	efforts := ReasoningEffortsFor(model)
	if len(efforts) == 0 {
		return "medium"
	}
	for _, preferred := range []string{"medium", "high"} {
		if effortContains(efforts, preferred) {
			return preferred
		}
	}
	return efforts[0]
}

// NormalizeReasoningEffort returns the canonical form of value when it is one of
// the model's accepted reasoning efforts, or "" otherwise. Passing an empty
// value also returns "" (callers distinguish "unset" from "invalid").
func NormalizeReasoningEffort(model Model, value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	for _, effort := range ReasoningEffortsFor(model) {
		if strings.EqualFold(effort, value) {
			return strings.ToLower(effort)
		}
	}
	return ""
}

func reasoningEffortID(model Model) string {
	if s := strings.TrimSpace(model.APIModel); s != "" {
		return strings.ToLower(s)
	}
	return strings.ToLower(string(model.ID))
}

func anthropicReasoningEfforts(id string) []string {
	if anthropicOpus47OrLater(id) || anthropicSonnet5OrLater(id) || strings.Contains(id, "fable-5") {
		return anthropicAdaptiveEfforts
	}
	if anthropic46IDRE.MatchString(id) {
		return anthropic46Efforts
	}
	return anthropic45Efforts
}

func anthropicOpus47OrLater(id string) bool {
	m := anthropicOpus47RE.FindStringSubmatch(id)
	if m == nil {
		return false
	}
	major, minor := atoi(m[1], m[3]), atoi(m[2], m[4])
	return major > 4 || (major == 4 && minor >= 7)
}

func anthropicSonnet5OrLater(id string) bool {
	m := anthropicSonnet5RE.FindStringSubmatch(id)
	if m == nil {
		return false
	}
	return atoi(m[1], m[2]) >= 5
}

func openaiReasoningEfforts(id string) []string {
	if strings.Contains(id, "deep-research") {
		return []string{"medium"}
	}
	if gpt5Chat(id) {
		return openaiGPT5ChatEfforts
	}
	if gpt5ProRE.MatchString(id) {
		if gpt5VersionedProRE.MatchString(id) {
			return openaiGPT5Pro2Plus
		}
		return openaiGPT5ProEfforts
	}
	if efforts := gpt5CodexEfforts(id); efforts != nil {
		return efforts
	}
	if efforts := versionedGpt5Efforts(id); efforts != nil {
		return efforts
	}
	return widelySupportedEfforts
}

func versionedGpt5Efforts(id string) []string {
	m := gpt5VersionRE.FindStringSubmatch(id)
	if m == nil {
		return nil
	}
	if atoi(m[1], "") == 1 {
		return openaiGPT51Efforts
	}
	return openaiGPT52PlusEfforts
}

func gpt5CodexEfforts(id string) []string {
	if !gpt5FamilyRE.MatchString(id) || !strings.Contains(id, "codex") {
		return nil
	}
	version := 0
	if m := gpt5VersionRE.FindStringSubmatch(id); m != nil {
		version = atoi(m[1], "")
	}
	if version >= 3 {
		return openaiGPT5Codex3Plus
	}
	if strings.Contains(id, "codex-max") || version >= 2 {
		return openaiGPT5CodexXHigh
	}
	return widelySupportedEfforts
}

func gpt5Chat(id string) bool {
	return gpt5FamilyRE.MatchString(id) && strings.Contains(id, "-chat")
}

func cloneEfforts(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func effortContains(efforts []string, value string) bool {
	for _, e := range efforts {
		if strings.EqualFold(e, value) {
			return true
		}
	}
	return false
}

// atoi parses a decimal string, returning the second choice when the first is
// empty. It returns 0 for non-numeric input.
func atoi(primary, fallback string) int {
	s := primary
	if s == "" {
		s = fallback
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}