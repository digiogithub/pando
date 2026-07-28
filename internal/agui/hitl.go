package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/userinput"
	"github.com/google/uuid"
)

// Human in the loop (P4).
//
// The adapter owns its own permission and user-input services (invariant I3), so
// a prompt raised by a browser run can never surface in a TUI or Web-UI dialog.
// P4 gives those prompts somewhere to go: the AG-UI client renders them as tool
// calls and answers them, which is exactly what CopilotKit's useHumanInTheLoop
// expects.
//
// Both prompts reuse the P3 suspend/resume machinery — a blocked call, a run
// that ends with outcome "interrupt", and a tool message on the next request —
// but they reach it differently:
//
//   - A question is already a real tool call (tools.AskUserQuestionToolName), so
//     the agent's own stream carries its START/ARGS/END. The adapter only has to
//     substitute a tool that blocks on the client instead of on a local dialog.
//   - A permission request happens *inside* another tool's Run and has no tool
//     call of its own. The adapter synthesizes one, under permissionToolName.
//
// Everything here fails closed: a timeout, a disconnected client or an
// unparseable answer denies the permission and cancels the question.

// permissionToolName is the synthetic AG-UI tool a permission prompt appears as.
// Frontend code binds to this name to render an approval card.
const permissionToolName = "pando_permission_request"

// installPermissionPolicy wires the approval policy for a session.
//
// It uses the per-session handler hook, so it never touches global permission
// state and cannot leak into another surface. The handler is called
// synchronously inside the tool's goroutine, which is what makes blocking here a
// legitimate way to suspend the run.
func (r *Runtime) installPermissionPolicy(sessionID string) {
	r.perms.RegisterSessionHandler(sessionID, func(req permission.CreatePermissionRequest) bool {
		if r.cfg.AutoApprove {
			logging.Debug("agui: auto-approving tool permission",
				"session", sessionID, "tool", req.ToolName, "action", req.Action)
			return true
		}
		if !r.cfg.HumanInTheLoop {
			logging.Info("agui: denying tool permission (human-in-the-loop disabled)",
				"session", sessionID, "tool", req.ToolName, "action", req.Action)
			return false
		}
		return r.askClientForApproval(sessionID, req)
	})
}

// removePermissionPolicy detaches the handler installed above.
func (r *Runtime) removePermissionPolicy(sessionID string) {
	r.perms.UnregisterSessionHandler(sessionID)
}

// askClientForApproval suspends the run on a synthetic tool call and returns the
// client's verdict. Anything other than an explicit approval is a denial.
func (r *Runtime) askClientForApproval(sessionID string, req permission.CreatePermissionRequest) bool {
	callID := "perm-" + uuid.NewString()
	args := permissionArgs(req)

	msg, ok := r.awaitClient(sessionID, suspension{
		callID: callID,
		events: []Event{
			NewToolCallStart(callID, permissionToolName, ""),
			NewToolCallArgs(callID, args),
			NewToolCallEnd(callID),
		},
	})
	if !ok {
		logging.Warn("agui: permission denied, no answer from the client",
			"session", sessionID, "tool", req.ToolName, "action", req.Action)
		return false
	}

	approved := approvalFromMessage(msg)
	logging.Info("agui: permission answered by the client",
		"session", sessionID, "tool", req.ToolName, "action", req.Action, "approved", approved)
	return approved
}

// awaitClient registers a call, lets the streaming handler turn it into an
// interrupt, and waits for the answer.
func (r *Runtime) awaitClient(sessionID string, s suspension) (Message, bool) {
	ch := r.pending.register(sessionID, s)
	defer r.pending.forget(sessionID, s.callID)

	timer := time.NewTimer(defaultFrontendToolTimeout)
	defer timer.Stop()

	select {
	case msg := <-ch:
		return msg, true
	case <-timer.C:
		return Message{}, false
	case <-r.baseCtx.Done():
		return Message{}, false
	}
}

// permissionArgs renders the request as the tool call's arguments. The shape is
// the client's contract for rendering an approval card, so it is spelled out
// here rather than marshalling the internal struct directly.
func permissionArgs(req permission.CreatePermissionRequest) string {
	payload := map[string]any{
		"toolName":    req.ToolName,
		"action":      req.Action,
		"description": req.Description,
		"path":        req.Path,
		"params":      req.Params,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		// Params is arbitrary tool input; if it cannot be encoded the prompt
		// still has to reach the user, so it degrades to the scalar fields.
		encoded, _ = json.Marshal(map[string]any{
			"toolName":    req.ToolName,
			"action":      req.Action,
			"description": req.Description,
			"path":        req.Path,
		})
	}
	return string(encoded)
}

// approvalFromMessage decodes the client's answer.
//
// It accepts the structured form ({"approved":true}) and the handful of literal
// answers a hand-written frontend is likely to send. Everything else — including
// an empty result or a client-side error — is a denial: an approval must be
// explicit.
func approvalFromMessage(msg Message) bool {
	if msg.Error != "" {
		return false
	}
	content := strings.TrimSpace(msg.Content.String())
	if content == "" {
		return false
	}

	var structured struct {
		Approved *bool `json:"approved"`
		Allow    *bool `json:"allow"`
	}
	if err := json.Unmarshal([]byte(content), &structured); err == nil {
		if structured.Approved != nil {
			return *structured.Approved
		}
		if structured.Allow != nil {
			return *structured.Allow
		}
	}

	switch strings.ToLower(strings.Trim(content, `"`)) {
	case "true", "yes", "approve", "approved", "allow", "accept":
		return true
	}
	return false
}

// ---------------------------------------------------------------- questions

// hitlQuestionTool answers AskUserQuestion from the browser instead of from a
// local overlay.
//
// It deliberately keeps the wrapped tool's Info(): the model must see the exact
// same schema it would see anywhere else, and the client must receive the tool
// call under its well-known name so useHumanInTheLoop can bind to it.
type hitlQuestionTool struct {
	inner   tools.BaseTool
	pending *pendingRegistry
	timeout time.Duration
}

func (h *hitlQuestionTool) Info() tools.ToolInfo { return h.inner.Info() }

func (h *hitlQuestionTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	sessionID, _ := tools.GetContextValues(ctx)
	if sessionID == "" {
		return tools.NewTextErrorResponse("this tool can only run inside an AG-UI session"), nil
	}

	// This is a real tool call, so no synthetic events are needed — but the call
	// is still described, because a provider that does not stream tool use never
	// puts it on the agent's event stream (see suspension.call).
	ch := h.pending.register(sessionID, suspension{
		callID: params.ID,
		call:   &toolCallInfo{name: h.inner.Info().Name, input: params.Input},
	})
	defer h.pending.forget(sessionID, params.ID)

	timeout := h.timeout
	if timeout <= 0 {
		timeout = defaultFrontendToolTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case msg := <-ch:
		return tools.NewTextResponse(answerFromMessage(msg)), nil
	case <-timer.C:
		return tools.NewTextResponse(questionCancelled), nil
	case <-ctx.Done():
		return tools.ToolResponse{}, ctx.Err()
	}
}

// questionCancelled is what the model sees when nobody answered. It matches the
// semantics the local AskUserQuestion tool reports on cancellation: proceed
// using your own judgement.
const questionCancelled = "The user did not answer the question. Continue with your best judgement and state the assumption you made."

// answerFromMessage renders the client's selection as text for the model.
//
// The structured form is preferred, but a client that simply returns prose is
// passed through: the model reads this, not a parser.
func answerFromMessage(msg Message) string {
	if msg.Error != "" {
		return fmt.Sprintf("The question could not be answered: %s", msg.Error)
	}
	content := strings.TrimSpace(msg.Content.String())
	if content == "" {
		return questionCancelled
	}

	var structured struct {
		Cancelled bool `json:"cancelled"`
		Answers   []struct {
			QuestionID string   `json:"questionId"`
			Header     string   `json:"header"`
			Selected   []string `json:"selected"`
			OtherText  string   `json:"otherText"`
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(content), &structured); err != nil {
		return content
	}
	if structured.Cancelled {
		return questionCancelled
	}
	if len(structured.Answers) == 0 {
		return content
	}

	var b strings.Builder
	b.WriteString("The user answered:\n")
	for _, answer := range structured.Answers {
		label := answer.Header
		if label == "" {
			label = answer.QuestionID
		}
		selected := strings.Join(answer.Selected, ", ")
		if answer.OtherText != "" {
			if selected != "" {
				selected += "; "
			}
			selected += answer.OtherText
		}
		if selected == "" {
			selected = "(no selection)"
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, selected)
	}
	return strings.TrimRight(b.String(), "\n")
}

// watchQuestions is the safety net for questions raised outside the substituted
// tool. Nothing in the adapter does that today, but leaving the adapter's
// userinput service unattended would mean an unanswerable prompt blocks a run
// forever, so anything that reaches it is cancelled rather than ignored.
func (r *Runtime) watchQuestions(ctx context.Context) {
	events := r.userInput.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			req := ev.Payload
			if req.ID == "" {
				continue
			}
			logging.Warn("agui: cancelling a question raised outside the AG-UI question tool",
				"session", req.SessionID, "questions", len(req.Questions))
			r.userInput.Respond(req.ID, userinput.AskResponse{Cancelled: true})
		}
	}
}
