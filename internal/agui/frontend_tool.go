package agui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
)

// Frontend tools (P3).
//
// A CopilotKit page can declare tools that run in the browser
// (useCopilotAction / useFrontendTool / useHumanInTheLoop): rendering a chart,
// filling a form, asking the user to confirm. AG-UI models this as a handoff —
// the agent emits TOOL_CALL_START/ARGS/END, the run ends with
// outcome "interrupt", the browser executes the tool, and the next POST on the
// same thread carries the result as a tool message.
//
// Pando's side of that handoff needs no core change: a frontend tool is an
// ordinary tools.BaseTool whose Run blocks on a channel until the result comes
// back, exactly like internal/permission.Request and userinput.Ask already do.
// The agent loop is unaware that the tool executes in a browser a continent
// away; it is simply a slow tool.

// Errors surfaced to the model when a handoff cannot complete.
var (
	// errFrontendToolTimeout is reported to the model (not to the caller) when
	// the browser never came back.
	errFrontendToolTimeout = errors.New("the client never returned a result for this tool call")
)

const (
	// defaultFrontendToolTimeout bounds how long a run may stay suspended waiting
	// for the browser. Without it, a closed tab would leak a blocked agent
	// goroutine and hold the session busy forever.
	defaultFrontendToolTimeout = 10 * time.Minute
	// suspendNotifyBuffer sizes the per-session notification channel. Parallel
	// tool calls in one assistant turn are the only reason it needs more than 1.
	suspendNotifyBuffer = 16
)

// suspension is what the streaming handler learns when a run blocks on the
// client.
type suspension struct {
	// callID is the AG-UI tool call the client must answer.
	callID string
	// events, when set, are the tool-call events the handler must emit itself.
	// A permission prompt has no counterpart in the agent's stream, so it
	// synthesizes them (see hitl.go).
	events []Event
	// call describes the real tool call behind this suspension so the handler
	// can emit its START/ARGS/END when the agent's stream did not.
	//
	// It is not a redundant copy of what the stream carries: agent.processEvent
	// only publishes tool_call events for providers that stream tool use
	// incrementally. A provider that reports its tool calls in one final
	// response leaves the message correct but the event stream silent — and a
	// client that receives RUN_FINISHED{interrupt} with no TOOL_CALL_START has
	// no call to execute, which strands the run. Nil for synthetic calls, whose
	// events field already carries them.
	call *toolCallInfo
}

// toolCallInfo is the minimum a client needs to execute a call: which tool, with
// which arguments.
type toolCallInfo struct {
	name  string
	input string
}

// pendingRegistry tracks the calls that are waiting for the browser: frontend
// tools, permission prompts and questions alike.
//
// It is owned by the Runtime and shared with every pooled agent, keyed by
// session so two threads can suspend on the same tool name independently.
type pendingRegistry struct {
	mu       sync.Mutex
	sessions map[string]*sessionPending
}

type sessionPending struct {
	// calls carries the raw client message, not a tool response: a permission
	// answer and a tool result are decoded differently by their waiters.
	calls map[string]chan Message
	// notify tells the streaming handler that a call is now blocked on the
	// client, which is what turns the run into an "interrupt".
	notify chan suspension
}

func newPendingRegistry() *pendingRegistry {
	return &pendingRegistry{sessions: make(map[string]*sessionPending)}
}

// watch returns the session's suspension notifications, creating the session
// entry if the handler runs before the first tool call.
func (p *pendingRegistry) watch(sessionID string) <-chan suspension {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sessionLocked(sessionID).notify
}

func (p *pendingRegistry) sessionLocked(sessionID string) *sessionPending {
	sp, ok := p.sessions[sessionID]
	if !ok {
		sp = &sessionPending{
			calls:  make(map[string]chan Message),
			notify: make(chan suspension, suspendNotifyBuffer),
		}
		p.sessions[sessionID] = sp
	}
	return sp
}

// register arms a waiting slot for a call and announces the suspension. s
// describes what the handler must emit: synthetic events for a call the agent
// stream knows nothing about, or the call's identity so the handler can fill in
// events the stream did not produce.
func (p *pendingRegistry) register(sessionID string, s suspension) chan Message {
	p.mu.Lock()
	sp := p.sessionLocked(sessionID)
	ch := make(chan Message, 1)
	sp.calls[s.callID] = ch
	notify := sp.notify
	p.mu.Unlock()

	select {
	case notify <- s:
	default:
		// The handler is not streaming (client disconnected mid-run). The call
		// still waits; it will time out or be cancelled with the run.
		logging.Debug("agui: suspension notification dropped", "session", sessionID, "call", s.callID)
	}
	return ch
}

// forget drops a waiting slot, whether it was resolved, cancelled or timed out.
func (p *pendingRegistry) forget(sessionID, callID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sp, ok := p.sessions[sessionID]; ok {
		delete(sp.calls, callID)
	}
}

// resolve delivers a browser-produced result. It reports false when no call is
// waiting under that id, which is how a stale or duplicated tool message from
// the client is ignored instead of corrupting a later run.
func (p *pendingRegistry) resolve(sessionID, callID string, msg Message) bool {
	p.mu.Lock()
	ch, ok := func() (chan Message, bool) {
		sp, exists := p.sessions[sessionID]
		if !exists {
			return nil, false
		}
		c, exists := sp.calls[callID]
		return c, exists
	}()
	p.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- msg:
		return true
	default:
		// Already resolved by another delivery.
		return false
	}
}

// waiting reports whether the session has any call blocked on the client.
func (p *pendingRegistry) waiting(sessionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	sp, ok := p.sessions[sessionID]
	return ok && len(sp.calls) > 0
}

// discard tears down a session's bookkeeping when its run ends.
func (p *pendingRegistry) discard(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sessions, sessionID)
}

// ------------------------------------------------------------------ the proxy

// frontendTool is the server-side stand-in for a tool the browser owns.
type frontendTool struct {
	info    tools.ToolInfo
	pending *pendingRegistry
	timeout time.Duration
}

func (f *frontendTool) Info() tools.ToolInfo { return f.info }

// Run suspends the agent loop until the client returns a result for this call.
//
// It never returns a Go error for a client-side failure: the model must see the
// outcome as a tool result it can reason about, not as a crashed run.
func (f *frontendTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	sessionID, _ := tools.GetContextValues(ctx)
	if sessionID == "" {
		return tools.NewTextErrorResponse("this tool can only run inside an AG-UI session"), nil
	}

	ch := f.pending.register(sessionID, suspension{
		callID: params.ID,
		call:   &toolCallInfo{name: f.info.Name, input: params.Input},
	})
	defer f.pending.forget(sessionID, params.ID)

	timeout := f.timeout
	if timeout <= 0 {
		timeout = defaultFrontendToolTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case msg := <-ch:
		return toolResponseFromMessage(msg), nil
	case <-timer.C:
		logging.Warn("agui: frontend tool timed out", "tool", f.info.Name, "call", params.ID)
		return tools.NewTextErrorResponse(errFrontendToolTimeout.Error()), nil
	case <-ctx.Done():
		return tools.ToolResponse{}, ctx.Err()
	}
}

// newFrontendTools builds the proxies for a client-declared toolset.
//
// A declared tool whose name collides with a Pando tool is dropped: letting a
// page shadow `bash` or `edit` would turn a rendering hint into a way of
// silently disabling a real capability.
func newFrontendTools(declared []Tool, reserved map[string]bool, pending *pendingRegistry) []tools.BaseTool {
	out := make([]tools.BaseTool, 0, len(declared))
	for _, d := range declared {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			continue
		}
		if reserved[name] {
			logging.Warn("agui: ignoring frontend tool that shadows a Pando tool", "tool", name)
			continue
		}
		properties, required := toolSchema(d.Parameters)
		out = append(out, &frontendTool{
			info: tools.ToolInfo{
				Name:        name,
				Description: frontendToolDescription(d),
				Parameters:  properties,
				Required:    required,
			},
			pending: pending,
			timeout: defaultFrontendToolTimeout,
		})
	}
	return out
}

// frontendToolDescription tells the model where the tool actually runs. Without
// it, a model that sees "showChart" has no reason to expect the call to take
// seconds and to be executed by someone else.
func frontendToolDescription(d Tool) string {
	desc := strings.TrimSpace(d.Description)
	if desc == "" {
		desc = "Client-side tool provided by the connected web application."
	}
	return desc + "\n\nThis tool executes in the user's browser; the result is returned by the web application."
}

// toolSchema splits a JSON-Schema object into the shape ToolInfo expects: the
// properties map and the required list. AG-UI clients send a full schema
// object, but a few send the properties map directly, so both are accepted.
func toolSchema(params any) (map[string]any, []string) {
	schema, ok := params.(map[string]any)
	if !ok || len(schema) == 0 {
		return map[string]any{}, nil
	}

	properties, hasProperties := schema["properties"].(map[string]any)
	if !hasProperties {
		if _, looksLikeSchema := schema["type"]; looksLikeSchema {
			// A schema object with no properties: an argument-less tool.
			return map[string]any{}, nil
		}
		return schema, nil
	}

	var required []string
	if raw, ok := schema["required"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				required = append(required, s)
			}
		}
	}
	return properties, required
}

// toolResponseFromMessage converts a client tool message into the response the
// blocked proxy hands back to the model.
func toolResponseFromMessage(m Message) tools.ToolResponse {
	if m.Error != "" {
		return tools.NewTextErrorResponse(fmt.Sprintf("the client failed to run this tool: %s", m.Error))
	}
	content := m.Content.String()
	if strings.TrimSpace(content) == "" {
		content = "(the client returned no output)"
	}
	return tools.NewTextResponse(content)
}
