package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/digiogithub/pando/internal/llm/agent"
)

const (
	// resumeBusyRetryWindow bounds how long a resumed run waits for the previous
	// background run of the session to be marked done. The agent frees the session
	// slightly before the pump goroutine closes the old run, so a resume landing in
	// that gap would otherwise be refused as busy.
	resumeBusyRetryWindow = 2 * time.Second
	resumeBusyRetryStep   = 20 * time.Millisecond

	// brokerRunPollInterval is how often a broker-backed stream re-checks that the
	// agent is still running the session.
	brokerRunPollInterval = 500 * time.Millisecond
)

// registerResumeHandler makes the server the owner of the runs the delegation
// supervisor resumes for idle sessions, so they flow through bgRunner like a run
// started by a user prompt: replay buffer, live stream, busy flag, steer and
// cancel all work unchanged. It registers as the fallback owner: any session no
// more specific surface claimed is taken, because the WebUI can display any
// session.
func (s *Server) registerResumeHandler() {
	if s.app == nil || s.app.ResumeHandlers == nil {
		return
	}
	s.unregisterResume = s.app.ResumeHandlers.Register(agent.ResumePriorityFallback, s.takeResumedRun)
}

// takeResumedRun submits the resumed run to bgRunner. It always takes the run;
// an error (ErrSessionBusy when the session became active again) is reported to
// the supervisor, which falls back to live injection.
func (s *Server) takeResumedRun(sessionID string, start agent.ResumeStart) (bool, error) {
	deadline := time.Now().Add(resumeBusyRetryWindow)
	for {
		err := s.bgRunner.Submit(sessionID, func(ctx context.Context) (<-chan agent.AgentEvent, error) {
			return start(ctx)
		})
		if !errors.Is(err, agent.ErrSessionBusy) || s.agentBusy(sessionID) || time.Now().After(deadline) {
			return true, err
		}
		// bgRunner still holds the previous run of the session but the agent is
		// already idle: the old run is only moments from being marked done.
		time.Sleep(resumeBusyRetryStep)
	}
}

// agentBusy reports whether the agent itself has a run active for the session,
// whoever started it (bgRunner, the TUI, ACP, a resumed run).
func (s *Server) agentBusy(sessionID string) bool {
	return s.app != nil && s.app.CoderAgent != nil && s.app.CoderAgent.IsSessionBusy(sessionID)
}

// sessionRunning is the server's truth about whether a session is being worked
// on. bgRunner alone is not enough: a run started outside it (for instance one
// resumed with no handler) is only visible to the agent.
func (s *Server) sessionRunning(sessionID string) bool {
	return s.bgRunner.IsBusy(sessionID) || s.agentBusy(sessionID)
}

// brokerRunEvents adapts the agent's pubsub broker into an event channel for a
// session whose run bgRunner does not own, so streamSessionEvents can serve it.
// There is no replay buffer: the stream starts at the moment of attachment. The
// channel is closed when the agent stops running the session or ctx ends. The
// returned func stops the forwarding.
func (s *Server) brokerRunEvents(ctx context.Context, sessionID string) (<-chan agent.AgentEvent, func()) {
	ctx, cancel := context.WithCancel(ctx)
	sub := s.app.CoderAgent.Subscribe(ctx)
	out := make(chan agent.AgentEvent, 256)

	go func() {
		defer close(out)
		ticker := time.NewTicker(brokerRunPollInterval)
		defer ticker.Stop()
		forward := func(ev agent.AgentEvent) bool {
			if ev.SessionID != sessionID {
				return true
			}
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub:
				if !ok || !forward(ev.Payload) {
					return
				}
			case <-ticker.C:
				if s.agentBusy(sessionID) {
					continue
				}
				// Idle: flush what the broker already queued, then finish.
				for {
					select {
					case ev, ok := <-sub:
						if !ok || !forward(ev.Payload) {
							return
						}
					default:
						return
					}
				}
			}
		}
	}()
	return out, cancel
}

// handleSessionCancel stops whatever run is active for the session: the
// bgRunner run (user prompt or resumed run it owns) and, as a safety net, any run
// the agent holds for it that bgRunner does not know about.
// POST /api/v1/sessions/{id}/cancel
func (s *Server) handleSessionCancel(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}
	s.bgRunner.Cancel(sessionID)
	if s.app != nil && s.app.CoderAgent != nil {
		s.app.CoderAgent.Cancel(sessionID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sessionID, "cancelled": true})
}
