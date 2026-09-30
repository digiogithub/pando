package systemone

import (
	"context"
	"fmt"
	"strings"
)

// ProviderKind identifies a decision backend.
type ProviderKind string

const (
	KindOllama   ProviderKind = "ollama"
	KindTypeSafe ProviderKind = "typesafe"
	KindCustom   ProviderKind = "custom"
)

// ListStatus qualifies the result of ListDecisionModels.
type ListStatus string

const (
	// ListFiltered: the list is authoritative (only decision models).
	ListFiltered ListStatus = "filtered"
	// ListUnfiltered: the backend exposes no decision metadata; the list was
	// narrowed heuristically (or is the raw catalogue) and the UI may offer
	// the full list via showAll.
	ListUnfiltered ListStatus = "unfiltered"
	// ListUnsupported: the backend cannot list models; the id must be typed.
	ListUnsupported ListStatus = "unsupported"
)

// OllamaPullHint is the suggestion shown when no decision model is installed.
const OllamaPullHint = "ollama pull tev1:0.8b"

// SuggestedOllamaModels are the decision models Pando offers to pull.
var SuggestedOllamaModels = []string{"tev1:0.8b", "tev1", "nimble"}

// IsSuggestedOllamaModel reports whether name is a suggested decision model
// (an exact suggestion or another tag of a suggested model family).
func IsSuggestedOllamaModel(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	family := strings.SplitN(name, ":", 2)[0]
	for _, s := range SuggestedOllamaModels {
		if name == s || strings.SplitN(s, ":", 2)[0] == family {
			return true
		}
	}
	return false
}

// MissingSuggestedOllamaModels returns the suggested models absent from
// installed (compared case-insensitively, with or without a ":latest" tag).
func MissingSuggestedOllamaModels(installed []string) []string {
	out := []string{}
	for _, s := range SuggestedOllamaModels {
		found := false
		for _, have := range installed {
			if strings.EqualFold(have, s) || strings.EqualFold(have, s+":latest") {
				found = true
				break
			}
		}
		if !found {
			out = append(out, s)
		}
	}
	return out
}

// DecisionModel describes a model usable as a router.
type DecisionModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	ContextWindow int      `json:"contextWindow,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
}

// HealthReport is the outcome of a provider health probe.
type HealthReport struct {
	OK         bool         `json:"ok"`
	Kind       ProviderKind `json:"kind"`
	Model      string       `json:"model"`
	Version    string       `json:"version,omitempty"`
	Reachable  bool         `json:"reachable"`
	Authorized bool         `json:"authorized"`
	VersionOK  bool         `json:"versionOK"`
	ModelFound bool         `json:"modelPresent"`
	IsDecision bool         `json:"isDecisionModel"`
	Remote     bool         `json:"remote"`
	LatencyMs  int64        `json:"latencyMs"`
	Problems   []string     `json:"problems"`
}

// DecisionProvider is a System One backend.
type DecisionProvider interface {
	Kind() ProviderKind
	Client() *Client
	// ListDecisionModels lists router candidates. showAll returns every model
	// the backend exposes instead of only decision models.
	ListDecisionModels(ctx context.Context, showAll bool) ([]DecisionModel, ListStatus, error)
	// Health probes reachability, auth, version, model presence and latency.
	Health(ctx context.Context, model string) HealthReport
	// ContextBudget returns the router model's context budget in tokens.
	ContextBudget(ctx context.Context, model string) int
}

// NewProvider builds the provider for kind.
func NewProvider(kind ProviderKind, o Options) (DecisionProvider, error) {
	switch kind {
	case KindOllama:
		if o.BaseURL == "" {
			o.BaseURL = "http://localhost:11434"
		}
		return newOllamaProvider(o), nil
	case KindTypeSafe:
		if o.BaseURL == "" {
			o.BaseURL = "https://api.typesafe.ai"
		}
		return newRemoteProvider(KindTypeSafe, o), nil
	case KindCustom:
		if strings.TrimSpace(o.BaseURL) == "" {
			return nil, fmt.Errorf("systemone: custom provider requires a base URL")
		}
		return newRemoteProvider(KindCustom, o), nil
	default:
		return nil, fmt.Errorf("systemone: unknown provider kind %q", kind)
	}
}

// probeRequest is the tiny request used for health probes and warm-up.
func probeRequest(model, keepAlive string) Request {
	return Request{
		Model: model,
		State: "ping",
		Questions: map[string]Question{
			"ok": {
				Type:         "choice",
				Instructions: "Is this a connectivity test?",
				Criteria:     []Criterion{NewCriterion("yes", "it is a test"), NewCriterion("no", "it is not a test")},
			},
		},
		KeepAlive: keepAlive,
	}
}

func finishReport(r *HealthReport) HealthReport {
	if r.Problems == nil {
		r.Problems = []string{}
	}
	r.OK = len(r.Problems) == 0
	return *r
}
