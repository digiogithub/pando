package agent

import (
	"context"
	"errors"

	"github.com/digiogithub/pando/internal/superpowers"
)

// ErrSuperpowersNotActive is returned when a Superpowers-only operation (the
// closing turn) is requested for a session that never enabled the mode.
var ErrSuperpowersNotActive = errors.New("superpowers mode is not active for this session")

// SetSuperpowersMode enables or disables the Superpowers workflow policy for a
// session. It is the entry point used by every surface that exposes the
// /superpowers commands (ACP, Web UI, TUI), mirroring SetPonytailMode. The state
// itself lives in internal/superpowers so tools can read it without importing
// this package.
func SetSuperpowersMode(sessionID string, enabled bool) {
	superpowers.SetEnabled(sessionID, enabled)
}

// SuperpowersMode reports whether the Superpowers policy is active for a session.
func SuperpowersMode(sessionID string) bool {
	return superpowers.Enabled(sessionID)
}

// superpowersEnabledForContext resolves the mode from the session id carried by
// the request context (same keys used by sessionLLMOverridesForContext).
//
// The context resolution lives here rather than in internal/superpowers on
// purpose: the context keys belong to internal/llm/prompt and internal/llm/tools,
// and internal/llm/tools reaches internal/mesnada/acp, which needs to import
// internal/superpowers for the slash commands. Keeping the core package free of
// those imports is what breaks that cycle.
func superpowersEnabledForContext(ctx context.Context) bool {
	return superpowers.Enabled(sessionIDFromContext(ctx))
}

// RunSuperpowersFinish runs the closing turn for /superpowers-finish as a normal
// agent turn (so it streams, persists and is cancellable like any other), and
// disables the mode only once that turn reaches a successful terminal response.
// A cancelled or failed run keeps the mode on, so the workflow is never silently
// abandoned.
//
// Keeping the success-only rule here — rather than in each of the ACP, Web UI and
// TUI command handlers — is what guarantees all three surfaces behave identically.
// The returned channel must be drained by the caller, exactly like Service.Run's.
func RunSuperpowersFinish(ctx context.Context, svc Service, sessionID string) (<-chan AgentEvent, error) {
	if !SuperpowersMode(sessionID) {
		return nil, ErrSuperpowersNotActive
	}

	source, err := svc.Run(ctx, sessionID, superpowers.FinishPrompt())
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
			SetSuperpowersMode(sessionID, false)
		}
	}()

	return out, nil
}
