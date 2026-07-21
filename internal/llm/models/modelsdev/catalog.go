// Package modelsdev provides read-only access to the models.dev catalog
// (https://github.com/anomalyco/models.dev), a community-maintained database of
// model pricing and capabilities per provider.
//
// Providers rarely report pricing through their own model-listing APIs, so
// Pando cannot show a session cost for most dynamically discovered models. The
// catalog fills those gaps. It is strictly best-effort: every failure path
// (no network, malformed payload, unknown provider or model) degrades to "no
// data" so callers keep their original behaviour.
package modelsdev

import (
	"regexp"
	"strings"
)

// Cost holds the per-million-token prices reported by models.dev, in USD.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

// Limit holds the context window and max output tokens reported by models.dev.
type Limit struct {
	Context int64 `json:"context"`
	Output  int64 `json:"output"`
}

// Modalities lists the input/output modalities a model accepts.
type Modalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// ReasoningOption describes one reasoning control a model exposes (e.g. an
// "effort" selector with its allowed values).
type ReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

// Model is a single catalog entry.
type Model struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Family           string            `json:"family"`
	Attachment       bool              `json:"attachment"`
	Reasoning        bool              `json:"reasoning"`
	ReasoningOptions []ReasoningOption `json:"reasoning_options"`
	ToolCall         bool              `json:"tool_call"`
	Temperature      bool              `json:"temperature"`
	Knowledge        string            `json:"knowledge"`
	ReleaseDate      string            `json:"release_date"`
	LastUpdated      string            `json:"last_updated"`
	Modalities       Modalities        `json:"modalities"`
	OpenWeights      bool              `json:"open_weights"`
	Cost             Cost              `json:"cost"`
	Limit            Limit             `json:"limit"`
}

// SupportsReasoningEffort reports whether the model exposes an effort-style
// reasoning control, which is what Pando's UIs gate the effort selector on.
func (m Model) SupportsReasoningEffort() bool {
	for _, opt := range m.ReasoningOptions {
		if strings.EqualFold(opt.Type, "effort") && len(opt.Values) > 0 {
			return true
		}
	}
	return false
}

// Provider is one provider entry of the catalog, keyed by its models.dev id.
type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Doc    string           `json:"doc"`
	Models map[string]Model `json:"models"`
}

// Catalog is the parsed models.dev payload plus the lookup indexes built from
// it. The zero value is a valid, empty catalog: Lookup always reports "not
// found", which is exactly the fallback behaviour callers need.
type Catalog struct {
	providers map[string]Provider
	// index maps providerID -> normalized model key -> model. Each model is
	// registered under several keys (exact, lowercase, vendor-prefix stripped,
	// version-suffix stripped) so provider-specific id spellings still resolve.
	index map[string]map[string]Model
}

// newCatalog builds the lookup indexes for a decoded payload.
func newCatalog(providers map[string]Provider) Catalog {
	c := Catalog{
		providers: providers,
		index:     make(map[string]map[string]Model, len(providers)),
	}
	for providerID, provider := range providers {
		byKey := make(map[string]Model, len(provider.Models)*3)
		for modelID, model := range provider.Models {
			if model.ID == "" {
				model.ID = modelID
			}
			for _, key := range lookupKeys(modelID) {
				// First writer wins: the exact id of one model must never be
				// shadowed by the fuzzier key of another.
				if _, exists := byKey[key]; !exists {
					byKey[key] = model
				}
			}
		}
		c.index[strings.ToLower(providerID)] = byKey
	}
	return c
}

// Providers returns the raw provider map. Callers must not mutate it.
func (c Catalog) Providers() map[string]Provider { return c.providers }

// Lookup resolves a model within one models.dev provider. modelID is the
// provider's own API model identifier (Model.APIModel in Pando), which may
// carry a vendor prefix or a dated/versioned suffix the catalog does not use.
func (c Catalog) Lookup(providerID, modelID string) (Model, bool) {
	byKey, ok := c.index[strings.ToLower(strings.TrimSpace(providerID))]
	if !ok {
		return Model{}, false
	}
	for _, key := range lookupKeys(modelID) {
		if m, ok := byKey[key]; ok {
			return m, true
		}
	}
	return Model{}, false
}

// LookupAny tries several models.dev providers in order and returns the first
// hit. Used for Pando providers that front more than one upstream catalog
// (Antigravity, Bedrock, Vertex AI).
func (c Catalog) LookupAny(providerIDs []string, modelID string) (Model, bool) {
	for _, providerID := range providerIDs {
		if m, ok := c.Lookup(providerID, modelID); ok {
			return m, true
		}
	}
	return Model{}, false
}

// versionSuffix matches the dated/versioned tails providers append to model ids
// but models.dev omits: "-20250219", "-2025-02-19", "-v1:0", "-latest", "-001".
var versionSuffix = regexp.MustCompile(`(?i)[-@:](\d{8}|\d{4}-\d{2}-\d{2}|latest|preview|v\d+([.:]\d+)*|\d{3})$`)

// lookupKeys returns the candidate keys for a model id, most specific first.
// Duplicates are removed so the caller can index and query with the same list.
func lookupKeys(modelID string) []string {
	id := strings.TrimSpace(modelID)
	if id == "" {
		return nil
	}

	candidates := []string{id, strings.ToLower(id)}

	lower := strings.ToLower(id)
	// Vendor-prefixed ids ("anthropic/claude-sonnet-4", "us.anthropic.claude-…").
	if idx := strings.LastIndex(lower, "/"); idx >= 0 && idx+1 < len(lower) {
		candidates = append(candidates, lower[idx+1:])
	}

	// Strip up to two version-ish suffixes ("claude-3-7-sonnet-20250219-v1:0").
	stripped := lower
	for range 2 {
		next := versionSuffix.ReplaceAllString(stripped, "")
		if next == stripped {
			break
		}
		stripped = next
		candidates = append(candidates, stripped)
		if idx := strings.LastIndex(stripped, "/"); idx >= 0 && idx+1 < len(stripped) {
			candidates = append(candidates, stripped[idx+1:])
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	unique := candidates[:0]
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		unique = append(unique, c)
	}
	return unique
}
