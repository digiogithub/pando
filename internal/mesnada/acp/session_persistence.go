package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type persistedACPState struct {
	Model              string `json:"model,omitempty"`
	ReasoningEffort    string `json:"reasoning_effort,omitempty"`
	ThinkingMode       string `json:"thinking_mode,omitempty"`
	ThinkingStreamMode string `json:"thinking_stream_mode,omitempty"`
}

func (a *PandoACPAgent) persistACPState(ctx context.Context, session *ACPServerSession) error {
	if session == nil {
		return nil
	}

	payload, err := marshalPersistedACPState(a.agentService, session)
	if err != nil {
		return fmt.Errorf("marshal ACP session state: %w", err)
	}

	if err := a.sessionService.SaveACPSessionState(ctx, session.PandoSessionID(), payload); err != nil {
		return fmt.Errorf("save ACP session state: %w", err)
	}
	return nil
}

func (a *PandoACPAgent) restoreACPState(ctx context.Context, session *ACPServerSession) (bool, error) {
	if session == nil {
		return false, nil
	}

	rawState, err := a.sessionService.GetACPSessionState(ctx, session.PandoSessionID())
	if err != nil {
		return false, fmt.Errorf("load ACP session state: %w", err)
	}
	if strings.TrimSpace(rawState) == "" {
		return false, nil
	}

	state, err := unmarshalPersistedACPState(rawState)
	if err != nil {
		return false, fmt.Errorf("decode ACP session state: %w", err)
	}

	applyPersistedACPState(a.agentService, session, state)
	reconcileACPThinkingSession(a.agentService, session)
	return true, nil
}

func marshalPersistedACPState(svc AgentService, session *ACPServerSession) (string, error) {
	state := persistedACPState{
		Model:              strings.TrimSpace(resolvedModelValue(svc, session.Model())),
		ReasoningEffort:    session.ReasoningEffort(),
		ThinkingMode:       session.ThinkingMode(),
		ThinkingStreamMode: session.ThinkingStreamMode(),
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func unmarshalPersistedACPState(raw string) (persistedACPState, error) {
	var state persistedACPState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return persistedACPState{}, err
	}
	state.Model = strings.TrimSpace(state.Model)
	state.ReasoningEffort = normalizeReasoningEffortValue(state.ReasoningEffort)
	state.ThinkingMode = normalizeThinkingModeValue(state.ThinkingMode)
	state.ThinkingStreamMode = normalizeThinkingStreamMode(state.ThinkingStreamMode)
	return state, nil
}

func applyPersistedACPState(svc AgentService, session *ACPServerSession, state persistedACPState) {
	if session == nil {
		return
	}

	session.SetModel(normalizePersistedACPModel(svc, state.Model))
	session.SetReasoningEffort(state.ReasoningEffort)
	session.SetThinkingMode(state.ThinkingMode)
	session.SetThinkingStreamMode(state.ThinkingStreamMode)
}

func normalizePersistedACPModel(svc AgentService, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}

	for _, model := range svc.AvailableModels() {
		if strings.TrimSpace(model.ID) == modelID {
			return modelID
		}
	}

	return ""
}
