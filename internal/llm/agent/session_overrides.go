package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// SessionLLMOverrides holds per-session runtime overrides that should take
// precedence over agent config when building a request-scoped provider.
type SessionLLMOverrides struct {
	ReasoningEffort string
	ThinkingMode    config.ThinkingMode
}

var sessionLLMOverrides sync.Map

func SetSessionLLMOverrides(sessionID string, overrides SessionLLMOverrides) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	normalized := SessionLLMOverrides{
		ReasoningEffort: normalizeSessionReasoningEffort(overrides.ReasoningEffort),
		ThinkingMode:    normalizeSessionThinkingMode(overrides.ThinkingMode),
	}
	if normalized.ReasoningEffort == "" && normalized.ThinkingMode == "" {
		sessionLLMOverrides.Delete(sessionID)
		return
	}

	sessionLLMOverrides.Store(sessionID, normalized)
}

func sessionLLMOverridesForContext(ctx context.Context) SessionLLMOverrides {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		return SessionLLMOverrides{}
	}

	value, ok := sessionLLMOverrides.Load(sessionID)
	if !ok {
		return SessionLLMOverrides{}
	}

	overrides, ok := value.(SessionLLMOverrides)
	if !ok {
		return SessionLLMOverrides{}
	}
	return overrides
}

func sessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if sessionID, ok := ctx.Value(prompt.SessionIDKey).(string); ok && strings.TrimSpace(sessionID) != "" {
		return strings.TrimSpace(sessionID)
	}
	if sessionID, ok := ctx.Value(tools.SessionIDContextKey).(string); ok && strings.TrimSpace(sessionID) != "" {
		return strings.TrimSpace(sessionID)
	}
	return ""
}

func effectiveReasoningEffort(agentConfig config.Agent, overrides SessionLLMOverrides) string {
	if overrides.ReasoningEffort != "" {
		return overrides.ReasoningEffort
	}
	return agentConfig.ReasoningEffort
}

func effectiveAnthropicThinkingMode(model models.Model, agentConfig config.Agent, overrides SessionLLMOverrides) config.ThinkingMode {
	if overrides.ThinkingMode != "" {
		return overrides.ThinkingMode
	}
	return defaultAnthropicThinkingMode(model, agentConfig.ThinkingMode)
}

func normalizeSessionReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	default:
		return ""
	}
}

func normalizeSessionThinkingMode(value config.ThinkingMode) config.ThinkingMode {
	switch config.ThinkingMode(strings.ToLower(strings.TrimSpace(string(value)))) {
	case config.ThinkingDisabled:
		return config.ThinkingDisabled
	case config.ThinkingLow:
		return config.ThinkingLow
	case config.ThinkingMedium:
		return config.ThinkingMedium
	case config.ThinkingHigh:
		return config.ThinkingHigh
	default:
		return ""
	}
}
