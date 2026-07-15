package agent

import (
	"context"
	"errors"

	"github.com/digiogithub/pando/internal/learning"
)

// ErrLearningNotActive is returned when a Learning-only operation (the closing
// turn) is requested for a session that never enabled the mode.
var ErrLearningNotActive = errors.New("learning mode is not active for this session")

// SetLearningMode enables or disables the Learning learner/documentarian policy
// for a session. It is the entry point used by every surface that exposes the
// /learning commands (ACP, Web UI, TUI), mirroring SetSuperpowersMode. The state
// itself lives in internal/learning so tools can read it without importing this
// package.
func SetLearningMode(sessionID string, enabled bool) {
	learning.SetEnabled(sessionID, enabled)
}

// LearningMode reports whether the Learning policy is active for a session.
func LearningMode(sessionID string) bool {
	return learning.Enabled(sessionID)
}

// learningEnabledForContext resolves the mode from the session id carried by the
// request context (same keys used by sessionLLMOverridesForContext).
//
// The context resolution lives here rather than in internal/learning on purpose:
// the context keys belong to internal/llm/prompt and internal/llm/tools, and
// internal/llm/tools reaches internal/mesnada/acp, which needs to import
// internal/learning for the slash commands. Keeping the core package free of those
// imports is what breaks that cycle.
func learningEnabledForContext(ctx context.Context) bool {
	return learning.Enabled(sessionIDFromContext(ctx))
}

// RunLearningFinish runs the closing turn for /learning-finish as a normal agent
// turn (so it streams, persists and is cancellable like any other), consolidating
// what was learned into KB/memory, then disables the mode only once that turn
// reaches a successful terminal response. A cancelled or failed run keeps the mode
// on, so the workflow is never silently abandoned.
//
// Keeping the success-only rule here — rather than in each of the ACP, Web UI and
// TUI command handlers — is what guarantees all three surfaces behave identically.
// The returned channel must be drained by the caller, exactly like Service.Run's.
func RunLearningFinish(ctx context.Context, svc Service, sessionID string) (<-chan AgentEvent, error) {
	if !LearningMode(sessionID) {
		return nil, ErrLearningNotActive
	}

	source, err := svc.Run(ctx, sessionID, learning.FinishPrompt())
	if err != nil {
		return nil, err
	}

	buffer := cap(source)
	if buffer < 1 {
		buffer = 1
	}
	out := make(chan AgentEvent, buffer)

	go func() {
		defer close(out)
		succeeded := false
		for event := range source {
			if event.Type == AgentEventTypeResponse && event.Done && event.Error == nil {
				succeeded = true
			}
			out <- event
		}
		if succeeded {
			SetLearningMode(sessionID, false)
		}
	}()

	return out, nil
}
