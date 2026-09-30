package modelrouter

import (
	"fmt"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// Skip reasons reported by FilterCandidates.
const (
	SkipUnknown       = "unknown model"
	SkipDisabled      = "provider disabled or unconfigured"
	SkipNoAttachments = "no attachment support"
	SkipContextWindow = "context window too small"
	SkipDuplicate     = "duplicate"
)

// CandidateInfo describes a candidate model for filtering.
type CandidateInfo struct {
	ID                  models.ModelID
	Known               bool
	Enabled             bool
	SupportsAttachments bool
	ContextWindow       int
}

// FilterCandidates removes duplicates, unknown models, models whose provider is
// disabled or unconfigured, models without attachment support when the prompt
// carries attachments, and models whose context window cannot hold
// historyTokens. The order of cands is preserved. skipped maps each removed
// model to the reason. A ContextWindow <= 0 means "unknown" and is never
// skipped.
func FilterCandidates(cands []models.ModelID, lookup func(models.ModelID) (CandidateInfo, bool), hasAttachments bool, historyTokens int) (usable []models.ModelID, skipped map[models.ModelID]string) {
	skipped = map[models.ModelID]string{}
	seen := map[models.ModelID]bool{}
	for _, id := range cands {
		if id == "" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		var info CandidateInfo
		ok := false
		if lookup != nil {
			info, ok = lookup(id)
		}
		switch {
		case !ok || !info.Known:
			skipped[id] = SkipUnknown
		case !info.Enabled:
			skipped[id] = SkipDisabled
		case hasAttachments && !info.SupportsAttachments:
			skipped[id] = SkipNoAttachments
		case info.ContextWindow > 0 && historyTokens > info.ContextWindow:
			skipped[id] = fmt.Sprintf("%s (%d > %d)", SkipContextWindow, historyTokens, info.ContextWindow)
		default:
			usable = append(usable, id)
		}
	}
	return usable, skipped
}

// DefaultLookup returns a lookup backed by the models registry and the provider
// enabled/disabled state in cfg. A nil cfg treats every known model as enabled.
func DefaultLookup(cfg *config.Config) func(models.ModelID) (CandidateInfo, bool) {
	return func(id models.ModelID) (CandidateInfo, bool) {
		m, ok := models.SupportedModels()[id]
		if !ok {
			return CandidateInfo{ID: id}, false
		}
		return CandidateInfo{
			ID:                  id,
			Known:               true,
			Enabled:             providerEnabled(cfg, m),
			SupportsAttachments: m.SupportsAttachments,
			ContextWindow:       int(m.ContextWindow),
		}, true
	}
}

func providerEnabled(cfg *config.Config, m models.Model) bool {
	if cfg == nil {
		return true
	}
	if m.AccountID != "" {
		for _, acc := range cfg.ProviderAccounts {
			if acc.ID == m.AccountID {
				return !acc.Disabled
			}
		}
		return false
	}
	for _, acc := range cfg.ProviderAccounts {
		if acc.Type == m.Provider && !acc.Disabled {
			return true
		}
	}
	if p, ok := cfg.Providers[m.Provider]; ok {
		if p.Disabled {
			return false
		}
		if p.APIKey != "" || p.BaseURL != "" || !providerNeedsKey(m.Provider) {
			return true
		}
		return false
	}
	// No entry at all: only key-less providers are usable.
	return !providerNeedsKey(m.Provider)
}

func providerNeedsKey(p models.ModelProvider) bool {
	return p != models.ProviderCopilot && p != models.ProviderOllama && p != models.ProviderLlamaCpp
}
