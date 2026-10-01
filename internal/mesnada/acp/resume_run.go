package acp

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/logging"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// resumePostRunTimeout bounds the post-run bookkeeping of a resumed run, which
// runs on a context detached from the (possibly cancelled) run context.
const resumePostRunTimeout = 30 * time.Second

// ResumeRunStart starts the system-initiated run of a resumed session with the
// given context and returns its (already translated) event channel. Cancelling
// the context cancels the run.
type ResumeRunStart func(ctx context.Context) (<-chan AgentEvent, error)

// sessionByPandoID returns the live ACP session bound to a Pando session id. The
// two ids differ for sessions created by other means than session/new, so the
// lookup compares PandoSessionID instead of using the map key.
func (a *PandoACPAgent) sessionByPandoID(pandoSessionID string) *ACPServerSession {
	a.sessionsMu.RLock()
	defer a.sessionsMu.RUnlock()
	for _, sess := range a.sessions {
		if sess.PandoSessionID() == pandoSessionID {
			return sess
		}
	}
	return nil
}

// TakeResumedRun offers the agent a run that the delegation supervisor resumes
// for an idle Pando session (after a delegated task concluded). ACP only
// forwards events of the run that belongs to a session/prompt, so without this
// the client would see nothing of the resumed turn.
//
// It returns taken == false, without calling start, unless this agent has a live
// session bound to pandoSessionID and a client connection to stream to; the
// caller then tries the next surface or drains the run. When it takes the run it
// starts it with a context cancelled by session/cancel and streams the events
// through the same translation and SendUpdate path a prompt uses, then sends the
// post-run updates a prompt sends (usage, title, status meta) without a
// PromptResponse, since no request is pending. A start error is returned with
// taken == true (ErrSessionBusy lets the supervisor fall back to injection).
func (a *PandoACPAgent) TakeResumedRun(pandoSessionID string, start ResumeRunStart) (bool, error) {
	acpSession := a.sessionByPandoID(pandoSessionID)
	if acpSession == nil || !acpSession.HasAgentConnection() {
		return false, nil
	}

	// Derived from the session context: session/cancel (and session/close) cancel
	// it, so a resumed run stops even though no prompt is in flight. Cancel also
	// reaches the agent service directly (see Cancel).
	runCtx, cancel := context.WithCancel(acpSession.Context())
	runCtx = a.prepareRun(runCtx, acpSession)
	_, cleanupPermissions := a.applyRunMode(acpSession.ID, acpSession)

	events, err := start(runCtx)
	if err != nil {
		cleanupPermissions()
		cancel()
		return true, err
	}
	logging.Info("acp: resumed run forwarding", "session_id", string(acpSession.ID), "pando_session_id", pandoSessionID)

	a.resumeWG.Add(1)
	go func() {
		defer a.resumeWG.Done()
		defer cancel()
		defer cleanupPermissions()
		a.forwardResumedRun(runCtx, acpSession, events)
	}()
	return true, nil
}

// forwardResumedRun consumes a resumed run's events, sharing
// processAgentEventStream with session/prompt, and finishes with the post-run
// updates.
func (a *PandoACPAgent) forwardResumedRun(ctx context.Context, acpSession *ACPServerSession, events <-chan AgentEvent) {
	stopReason, err := a.processAgentEventStream(ctx, acpSession, events)
	if err != nil && !isPromptCancellation(ctx, err) {
		a.logger.Printf("[ACP AGENT] Resumed run failed for session %s: %v", acpSession.ID, err)
		logging.Error("acp: resumed run failed", "session_id", string(acpSession.ID), "error", err)
		// No prompt response carries the failure, so surface it as a message.
		if sendErr := acpSession.SendUpdate(acpsdk.UpdateAgentMessageText(
			fmt.Sprintf("The resumed run failed: %v", err))); sendErr != nil {
			a.logger.Printf("[ACP AGENT] Failed to send resumed run error: %v", sendErr)
		}
	} else {
		logging.Info("acp: resumed run completed", "session_id", string(acpSession.ID), "stop_reason", string(stopReason))
	}

	// The run context is already cancelled after a session/cancel; the
	// bookkeeping still has to read the database.
	postCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resumePostRunTimeout)
	defer cancel()
	a.reconcileModelAfterRun(postCtx, acpSession)
	a.sendPostRunUpdates(postCtx, acpSession.ID, acpSession)
}
