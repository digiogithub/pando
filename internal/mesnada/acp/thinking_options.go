package acp

import (
	"fmt"
	"strings"

	llmmodels "github.com/digiogithub/pando/internal/llm/models"
)

type acpThinkingSettings struct {
	ThinkingStreamMode string
	ReasoningEffort    string
	ThinkingMode       string
}

func currentACPThinkingSettings(session *ACPServerSession) acpThinkingSettings {
	if session == nil {
		return acpThinkingSettings{}
	}
	return acpThinkingSettings{
		ThinkingStreamMode: session.ThinkingStreamMode(),
		ReasoningEffort:    session.ReasoningEffort(),
		ThinkingMode:       session.ThinkingMode(),
	}
}

func selectedACPModelForSession(svc AgentService, session *ACPServerSession) (llmmodels.Model, bool) {
	if session == nil {
		return llmmodels.Model{}, false
	}
	return selectedACPModel(svc, session.Model())
}

func normalizedACPThinkingSettingsForSession(svc AgentService, session *ACPServerSession) acpThinkingSettings {
	model, ok := selectedACPModelForSession(svc, session)
	return normalizeACPThinkingSettings(model, ok, currentACPThinkingSettings(session))
}

// sessionLLMOverridesFor builds the full set of per-session LLM overrides
// (model, inference settings and persona) for an ACP session. These are applied
// per session — never as global agent state — so a single ACP server can run
// several concurrent sessions with different models/personas without one run
// clobbering another. Clean mode is handled separately via the clean-mode
// context flag, so here PersonaScoped is always set and an empty Persona means
// "no manual persona for this session" (auto-selection).
func sessionLLMOverridesFor(session *ACPServerSession) SessionLLMOverrides {
	if session == nil {
		return SessionLLMOverrides{}
	}
	return SessionLLMOverrides{
		Model:           session.Model(),
		ReasoningEffort: session.ReasoningEffort(),
		ThinkingMode:    session.ThinkingMode(),
		Persona:         session.Persona(),
		PersonaScoped:   true,
	}
}

// reconcileACPSessionModel adopts a model the agent switched to at runtime
// (pando_setup model) as the ACP session's model, reporting whether anything
// changed.
//
// It is required in both directions. Without it the ACP session keeps the model
// the client picked, so (a) the model picker and the persisted session state
// report a model that is not the one answering, and (b) the next prompt would
// hand that stale model back to the agent through sessionLLMOverridesFor and
// silently undo the switch the user confirmed.
//
// Only a real override is adopted (never CurrentModelID), so a session running
// on a model that simply differs from the global default is left alone.
func reconcileACPSessionModel(svc AgentService, session *ACPServerSession) bool {
	if svc == nil || session == nil {
		return false
	}
	override := strings.TrimSpace(svc.SessionModelOverrideID(session.PandoSessionID()))
	if override == "" || override == strings.TrimSpace(session.Model()) {
		return false
	}
	session.SetModel(override)
	reconcileACPThinkingSession(svc, session)
	return true
}

func reconcileACPThinkingSession(svc AgentService, session *ACPServerSession) acpThinkingSettings {
	settings := normalizedACPThinkingSettingsForSession(svc, session)
	applyACPThinkingSettings(session, settings)
	return settings
}

func applyACPThinkingSettings(session *ACPServerSession, settings acpThinkingSettings) {
	if session == nil {
		return
	}
	session.SetThinkingStreamMode(settings.ThinkingStreamMode)
	session.SetReasoningEffort(settings.ReasoningEffort)
	session.SetThinkingMode(settings.ThinkingMode)
}

func normalizeACPThinkingSettings(model llmmodels.Model, hasModel bool, settings acpThinkingSettings) acpThinkingSettings {
	normalized := acpThinkingSettings{
		ThinkingStreamMode: effectiveACPThinkingStreamMode(settings.ThinkingStreamMode),
	}
	if hasModel && supportsACPReasoningEffort(model) {
		normalized.ReasoningEffort = effectiveACPReasoningEffort(settings.ReasoningEffort)
	}
	if hasModel && supportsACPThinkingMode(model) {
		normalized.ThinkingMode = effectiveACPThinkingMode(settings.ThinkingMode)
	}
	return normalized
}

func validateACPThinkingConfigValue(model llmmodels.Model, hasModel bool, modelID, configID, value string) (string, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = string(model.ID)
	}
	switch configID {
	case sessionConfigThinkingStreamModeID:
		return parseThinkingStreamModeValue(value)
	case sessionConfigReasoningEffortID:
		if !hasModel || !supportsACPReasoningEffort(model) {
			return "", fmt.Errorf("reasoning_effort is not supported for model: %s", modelID)
		}
		return parseReasoningEffortValue(value)
	case sessionConfigThinkingModeID:
		if !hasModel || !supportsACPThinkingMode(model) {
			return "", fmt.Errorf("thinking_mode is not supported for model: %s", modelID)
		}
		return parseThinkingModeValue(value)
	default:
		return "", fmt.Errorf("unknown thinking config option: %s", configID)
	}
}

func effectiveACPThinkingStreamMode(value string) string {
	return normalizeThinkingStreamMode(value)
}

func normalizeThinkingStreamMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case thinkingStreamModeOff:
		return thinkingStreamModeOff
	case thinkingStreamModeFull:
		return thinkingStreamModeFull
	default:
		return thinkingStreamModeGrouped
	}
}

func parseThinkingStreamModeValue(value string) (string, error) {
	switch normalizeThinkingStreamMode(value) {
	case thinkingStreamModeGrouped:
		if normalized := strings.ToLower(strings.TrimSpace(value)); normalized == "" || normalized == thinkingStreamModeGrouped {
			return thinkingStreamModeGrouped, nil
		}
	case thinkingStreamModeOff:
		return thinkingStreamModeOff, nil
	case thinkingStreamModeFull:
		return thinkingStreamModeFull, nil
	}
	return "", fmt.Errorf("invalid thinking_stream_mode value: %s", value)
}

func normalizeReasoningEffortValue(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case reasoningEffortLow:
		return reasoningEffortLow
	case reasoningEffortMedium:
		return reasoningEffortMedium
	case reasoningEffortHigh:
		return reasoningEffortHigh
	default:
		return ""
	}
}

func effectiveACPReasoningEffort(value string) string {
	if normalized := normalizeReasoningEffortValue(value); normalized != "" {
		return normalized
	}
	return defaultACPReasoningEffort
}

func parseReasoningEffortValue(value string) (string, error) {
	if normalized := normalizeReasoningEffortValue(value); normalized != "" {
		return normalized, nil
	}
	return "", fmt.Errorf("invalid reasoning_effort value: %s", value)
}

func normalizeThinkingModeValue(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case thinkingModeDisabled:
		return thinkingModeDisabled
	case thinkingModeLow:
		return thinkingModeLow
	case thinkingModeMedium:
		return thinkingModeMedium
	case thinkingModeHigh:
		return thinkingModeHigh
	default:
		return ""
	}
}

func effectiveACPThinkingMode(value string) string {
	if normalized := normalizeThinkingModeValue(value); normalized != "" {
		return normalized
	}
	return defaultACPThinkingMode
}

func parseThinkingModeValue(value string) (string, error) {
	if normalized := normalizeThinkingModeValue(value); normalized != "" {
		return normalized, nil
	}
	return "", fmt.Errorf("invalid thinking_mode value: %s", value)
}

func supportsACPReasoningEffort(model llmmodels.Model) bool {
	return model.CanReason && model.SupportsReasoningEffort
}

func supportsACPThinkingMode(model llmmodels.Model) bool {
	return model.CanReason && model.Provider == llmmodels.ProviderAnthropic
}

func supportsACPThinkingStreaming(model llmmodels.Model) bool {
	return model.CanReason
}
