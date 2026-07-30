package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

// defaultModelSwitchesPerRun bounds how many times a single run may change model
// when no explicit cap is configured. Each switch rebuilds the provider and may
// force a compaction, so an agent looping between models would burn the turn.
const defaultModelSwitchesPerRun = 3

// modelSwitchRunState is the per-run state the pando_setup bridge needs to
// decide whether a switch is allowed. It exists because the bridge validates the
// request (and must refuse it with a message the agent can act on) while only
// the agent loop knows what the live conversation looks like.
type modelSwitchRunState struct {
	mu sync.Mutex
	// hasImages reports that the history the next request will carry contains
	// image parts, which a model without attachment support would reject.
	hasImages bool
	switches  int
}

// modelSwitchRunStates maps sessionID -> *modelSwitchRunState. An entry exists
// only while a run is in flight.
var modelSwitchRunStates sync.Map

// beginModelSwitchRun marks the session as running and resets the per-run switch
// counter. Called once per generation, so a long conversation is not capped by
// switches the user asked for in earlier turns.
func beginModelSwitchRun(sessionID string, msgs []message.Message) {
	if sessionID == "" {
		return
	}
	state := &modelSwitchRunState{hasImages: historyHasImages(msgs)}
	modelSwitchRunStates.Store(sessionID, state)
}

// endModelSwitchRun clears the run state. Outside a run the bridge applies no
// per-run cap and no history-compatibility guard: there is no history in flight
// to be incompatible with.
func endModelSwitchRun(sessionID string) {
	if sessionID == "" {
		return
	}
	modelSwitchRunStates.Delete(sessionID)
	// A quote nobody confirmed would otherwise sit in the map for the life of the
	// process; the end of a run is a cheap, bounded moment to collect them.
	pruneExpiredSetupModelProposals()
}

func modelSwitchRunStateFor(sessionID string) *modelSwitchRunState {
	value, ok := modelSwitchRunStates.Load(sessionID)
	if !ok {
		return nil
	}
	state, _ := value.(*modelSwitchRunState)
	return state
}

// setModelSwitchHistoryImages refreshes the image flag as the run appends tool
// results: a screenshot produced mid-loop makes a text-only model unusable from
// that point on.
func setModelSwitchHistoryImages(sessionID string, hasImages bool) {
	state := modelSwitchRunStateFor(sessionID)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.hasImages = hasImages
}

func (s *modelSwitchRunState) historyHasImages() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hasImages
}

// switchAvailable reports whether the per-run budget still has room, without
// consuming it.
func (s *modelSwitchRunState) switchAvailable(max int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.switches < max
}

// reserveSwitch consumes one switch of the per-run budget, reporting whether it
// was available.
func (s *modelSwitchRunState) reserveSwitch(max int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.switches >= max {
		return false
	}
	s.switches++
	return true
}

// historyHasImages reports whether any message carries binary image data, either
// as a user attachment or inside a tool result.
func historyHasImages(msgs []message.Message) bool {
	for _, msg := range msgs {
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case message.BinaryContent:
				return true
			case message.ImageURLContent:
				return true
			case message.ToolResult:
				if len(p.Images) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// applyPendingModelSwitch rebuilds the request provider when the session's model
// changed since the current one was built, and returns the history to send with
// it. It never aborts the run, hence no error return: a switch that cannot be
// carried out drops the override and the run continues on the model it already
// had, with the failure reported to the user.
func (a *agent) applyPendingModelSwitch(
	ctx context.Context,
	sessionID string,
	current provider.Provider,
	userPrompt string,
	personaContent string,
	msgHistory []message.Message,
	eventCh chan<- AgentEvent,
) (provider.Provider, []message.Message) {
	// Refresh what the guards see before deciding: tool results appended in the
	// previous iteration may have introduced images.
	setModelSwitchHistoryImages(sessionID, historyHasImages(msgHistory))

	if current == nil {
		return current, msgHistory
	}
	desired, _ := effectiveSessionModel(sessionID)
	if desired == "" || desired == current.Model().ID {
		return current, msgHistory
	}

	logging.Debug("model switch requested mid-run", "sessionID", sessionID,
		"from", current.Model().ID, "to", desired)

	// prepareProvider resolves the override from the context, so make sure the
	// session travels with it even if the caller's context does not carry it.
	next, err := a.prepareProvider(
		context.WithValue(ctx, prompt.SessionIDKey, sessionID), userPrompt, personaContent)
	if err == nil && (next == nil || next.Model().ID != desired) {
		// Defensive: a provider that came back on another model would make every
		// following iteration attempt the same switch again.
		rebuilt := models.ModelID("nothing")
		if next != nil {
			rebuilt = next.Model().ID
		}
		err = fmt.Errorf("the rebuilt provider still runs %s", rebuilt)
	}
	if err != nil {
		// Drop the override so the next iteration does not retry the same broken
		// switch, and keep the run alive on the current model.
		SetSessionModelOverride(sessionID, "")
		a.emitModelSwitchMessage(sessionID, eventCh, fmt.Sprintf(
			"\n\n⚠ Could not switch to %s: %v. Continuing with %s.\n", desired, err, current.Model().ID))
		return current, msgHistory
	}

	// Reasoning blocks and thought signatures belong to the model that produced
	// them; replaying them to another model is rejected by the provider APIs.
	msgHistory = sanitizeHistoryForModelSwitch(msgHistory)

	// The new model may have a smaller context window than the old one.
	fitted, err := a.ensureHistoryFitsBeforeSend(ctx, sessionID, msgHistory, next, eventCh)
	if err != nil {
		SetSessionModelOverride(sessionID, "")
		a.emitModelSwitchMessage(sessionID, eventCh, fmt.Sprintf(
			"\n\n⚠ Could not switch to %s: %v. Continuing with %s.\n", desired, err, current.Model().ID))
		return current, msgHistory
	}

	a.emitModelSwitchMessage(sessionID, eventCh, fmt.Sprintf(
		"\n\n⇄ Model switched to %s%s.\n", next.Model().ID, describeModelPrice(next.Model())))
	return next, fitted
}

func describeModelPrice(model models.Model) string {
	if model.CostPer1MIn <= 0 && model.CostPer1MOut <= 0 {
		return " (price unknown)"
	}
	return fmt.Sprintf(" ($%.2f in / $%.2f out per 1M)", model.CostPer1MIn, model.CostPer1MOut)
}

// emitModelSwitchMessage reports the switch on both the pubsub broker and the
// run channel, the way auto-compaction does, so every surface shows which model
// is answering.
func (a *agent) emitModelSwitchMessage(sessionID string, eventCh chan<- AgentEvent, text string) {
	event := AgentEvent{Type: AgentEventTypeSystemMessage, SessionID: sessionID, SystemMessage: text}
	a.publishEvent(event)
	if eventCh == nil {
		return
	}
	select {
	case eventCh <- event:
	default:
	}
}

// sanitizeHistoryForModelSwitch strips the reasoning artefacts of the previous
// model from the assistant turns. It copies every message it changes: the same
// backing slices are shared with the caller's message list and with what was
// loaded from the database, which must keep the original reasoning.
func sanitizeHistoryForModelSwitch(msgs []message.Message) []message.Message {
	result := make([]message.Message, len(msgs))
	copy(result, msgs)

	for i, msg := range result {
		if msg.Role != message.Assistant {
			continue
		}
		if !messageNeedsReasoningStrip(msg) {
			continue
		}
		parts := make([]message.ContentPart, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case message.ReasoningContent:
				continue
			case message.ToolCall:
				p.ThoughtSignature = nil
				parts = append(parts, p)
			default:
				parts = append(parts, part)
			}
		}
		msg.Parts = parts
		result[i] = msg
	}
	return result
}

func messageNeedsReasoningStrip(msg message.Message) bool {
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case message.ReasoningContent:
			return true
		case message.ToolCall:
			if len(p.ThoughtSignature) > 0 {
				return true
			}
		}
	}
	return false
}

// modelSwitchCapForRun returns the configured per-run cap.
func modelSwitchCapForRun() int {
	return config.SetupModelSwitchMaxPerRun(defaultModelSwitchesPerRun)
}
