package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/digiogithub/pando/internal/extensions"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/pkg/extension"
)

// The host's implementation of extension.PromptRunner.
//
// It is the non-interactive run without the terminal: no spinner, no stdout,
// no global mode switch. What it shares with RunNonInteractive is the only
// part that matters, the agent turn itself, so an automated caller gets the
// same behaviour a person gets from `pando -p` and no separate code path can
// drift away from it.

// promptTitleLimit bounds the generated session title, which exists for the
// session list and the logs rather than for the model.
const promptTitleLimit = 100

// runExtensionPrompt runs one turn for an extension. It blocks until the turn
// ends; the caller's context cancels it.
func (a *App) runExtensionPrompt(ctx context.Context, req extension.PromptRequest) (extension.PromptResult, error) {
	if a == nil || a.CoderAgent == nil || a.Sessions == nil {
		return extension.PromptResult{}, errors.New("app: no agent to run a prompt with")
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sess, err := a.Sessions.Create(ctx, promptTitle(req))
		if err != nil {
			return extension.PromptResult{}, fmt.Errorf("create session for extension prompt: %w", err)
		}
		sessionID = sess.ID
	}

	// Auto-approval is scoped to this session and lifted when the turn ends:
	// an extension asking for an unattended run must not leave the process
	// approving everything afterwards.
	if req.AutoApprove && a.Permissions != nil {
		a.Permissions.AutoApproveSession(sessionID)
		defer a.Permissions.RemoveAutoApproveSession(sessionID)
	}

	var progressDone chan struct{}
	if req.OnProgress != nil {
		streamCtx, stopStream := context.WithCancel(ctx)
		defer stopStream()
		events := a.CoderAgent.Subscribe(streamCtx)
		progressDone = make(chan struct{})
		go func() {
			defer close(progressDone)
			forwardPromptProgress(events, sessionID, req.OnProgress)
		}()
		defer func() {
			stopStream()
			<-progressDone
		}()
	}

	done, err := a.CoderAgent.Run(ctx, sessionID, req.Prompt)
	if err != nil {
		return extension.PromptResult{SessionID: sessionID}, fmt.Errorf("start agent turn: %w", err)
	}

	var last agent.AgentEvent
	for ev := range done {
		last = ev
	}

	result := extension.PromptResult{
		SessionID: sessionID,
		Text:      last.Message.Content().String(),
	}
	if reason := last.Message.FinishReason(); reason != "" {
		result.FinishReason = string(reason)
	}
	// Usage is read back from the session rather than accumulated here: the
	// session is what the host itself bills against, so an extension and a
	// human reading the UI see the same numbers.
	if sess, serr := a.Sessions.Get(ctx, sessionID); serr == nil {
		result.PromptTokens = sess.PromptTokens
		result.CompletionTokens = sess.CompletionTokens
		result.CostUSD = sess.Cost
	} else {
		logging.Debug("Extension prompt: session usage unavailable", "session", sessionID, "error", serr)
	}

	if last.Error != nil {
		return result, last.Error
	}
	return result, ctx.Err()
}

// forwardPromptProgress translates the agent's event stream into the public
// progress shape until the subscription closes.
func forwardPromptProgress(events <-chan pubsub.Event[agent.AgentEvent], sessionID string, onProgress func(extension.PromptProgress)) {
	for msg := range events {
		ev := msg.Payload
		if ev.SessionID != "" && ev.SessionID != sessionID {
			continue
		}
		p := extension.PromptProgress{SessionID: sessionID}
		switch ev.Type {
		case agent.AgentEventTypeContentDelta:
			p.Delta = ev.Delta
		case agent.AgentEventTypeToolCall:
			if ev.ToolCall == nil {
				continue
			}
			p.ToolName = ev.ToolCall.Name
		case agent.AgentEventTypeTokenUsage:
			if ev.TokenUsage == nil {
				continue
			}
			p.PromptTokens = ev.TokenUsage.PromptTokens
			p.CompletionTokens = ev.TokenUsage.CompletionTokens
			p.CostUSD = ev.TokenUsage.Cost
		default:
			continue
		}
		onProgress(p)
	}
}

// promptTitle names the session a run creates.
func promptTitle(req extension.PromptRequest) string {
	if req.Title != "" {
		return req.Title
	}
	prompt := req.Prompt
	if len(prompt) > promptTitleLimit {
		prompt = prompt[:promptTitleLimit] + "..."
	}
	return "Extension: " + prompt
}

// registerPromptRunner publishes this app as the host's prompt runner. It must
// run before extensions are loaded, because the manager copies the service
// into HostServices when it is built.
func (a *App) registerPromptRunner() {
	extensions.SetPromptRunner(a.runExtensionPrompt)
}
