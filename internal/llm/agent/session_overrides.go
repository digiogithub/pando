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
//
// These overrides are the mechanism that makes a single process safe to run
// several agent loops in parallel (e.g. an ACP server serving multiple
// concurrent sessions, or a warm delegation instance): instead of mutating
// shared/global agent state per prompt, each session threads its model,
// persona and inference settings through the request context so two
// concurrent runs never clobber each other.
type SessionLLMOverrides struct {
	// Model, when non-empty, replaces the agent's configured model for this
	// session only (request-scoped; does not touch global config).
	Model models.ModelID
	// ReasoningEffort / ThinkingMode override inference settings per session.
	ReasoningEffort string
	ThinkingMode    config.ThinkingMode
	// Persona, when non-empty, selects a specific persona for this session.
	// It is only honored when PersonaScoped is true.
	Persona string
	// PersonaScoped marks the session as managing its own persona
	// authoritatively: when true, the per-session Persona (or, if empty,
	// auto-selection) takes precedence over the package-global active persona.
	// This is what lets concurrent sessions use different personas safely.
	PersonaScoped bool
}

func (o SessionLLMOverrides) isEmpty() bool {
	return o.Model == "" &&
		o.ReasoningEffort == "" &&
		o.ThinkingMode == "" &&
		o.Persona == "" &&
		!o.PersonaScoped
}

var sessionLLMOverrides sync.Map

// sessionLLMOverridesMu serializes writers. Reads stay lock-free through the
// sync.Map; the mutex only exists so read-modify-write updates of a single field
// (SetSessionModelOverride) cannot lose a concurrent full-struct write.
var sessionLLMOverridesMu sync.Mutex

func SetSessionLLMOverrides(sessionID string, overrides SessionLLMOverrides) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	sessionLLMOverridesMu.Lock()
	defer sessionLLMOverridesMu.Unlock()
	storeSessionLLMOverrides(sessionID, overrides)
}

// SessionLLMOverridesFor returns the overrides stored for a session, or the zero
// value when none are set. Unlike sessionLLMOverridesForContext it does not need
// a request context, so callers outside the agent loop (the pando_setup bridge)
// can read the session's effective model.
func SessionLLMOverridesFor(sessionID string) SessionLLMOverrides {
	sessionID = strings.TrimSpace(sessionID)
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

// SetSessionModelOverride replaces only the model of a session's overrides,
// keeping persona and inference settings intact. Writing the whole struct here
// would silently drop the persona ACP or the Web UI installed for the session.
// An empty model clears just the model override.
func SetSessionModelOverride(sessionID string, model models.ModelID) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	sessionLLMOverridesMu.Lock()
	defer sessionLLMOverridesMu.Unlock()

	current := SessionLLMOverridesFor(sessionID)
	current.Model = model
	storeSessionLLMOverrides(sessionID, current)
}

// storeSessionLLMOverrides normalizes and persists the overrides. Callers must
// hold sessionLLMOverridesMu.
func storeSessionLLMOverrides(sessionID string, overrides SessionLLMOverrides) {
	normalized := SessionLLMOverrides{
		Model:           overrides.Model,
		ReasoningEffort: normalizeSessionReasoningEffort(overrides.ReasoningEffort),
		ThinkingMode:    normalizeSessionThinkingMode(overrides.ThinkingMode),
		Persona:         strings.TrimSpace(overrides.Persona),
		PersonaScoped:   overrides.PersonaScoped,
	}
	if normalized.isEmpty() {
		sessionLLMOverrides.Delete(sessionID)
		return
	}

	sessionLLMOverrides.Store(sessionID, normalized)
}

func sessionLLMOverridesForContext(ctx context.Context) SessionLLMOverrides {
	return SessionLLMOverridesFor(sessionIDFromContext(ctx))
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
