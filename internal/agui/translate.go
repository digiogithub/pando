package agui

import (
	"fmt"

	"github.com/digiogithub/pando/internal/llm/agent"
)

// translator converts Pando's agent event stream into AG-UI events for one run.
//
// It is the only place that knows both vocabularies. It is deliberately a pure,
// stateful value with no I/O: feeding it a recorded []agent.AgentEvent and
// comparing the output is the whole test strategy for protocol conformance.
//
// It also owns the protocol's ordering guarantees: text and reasoning blocks
// are opened lazily and always closed before the run ends, and each tool call
// emits START/ARGS*/END exactly once even though Pando re-emits a growing
// snapshot of the same call while arguments stream in.
type translator struct {
	threadID string
	runID    string

	messageID     string
	textOpen      bool
	reasoningOpen bool

	// toolArgsSent tracks how many bytes of each tool call's input have already
	// been forwarded, so repeated snapshots become incremental deltas.
	toolArgsSent map[string]int
	toolEnded    map[string]bool
	// toolSuppressed marks calls the client already resolved itself, so a
	// resumed run does not echo their results back into its own transcript.
	toolSuppressed map[string]bool

	// state is the thread's shared-state projection (P2). When it is nil the
	// translator falls back to namespaced CUSTOM events, which keeps the
	// translator usable — and testable — on its own.
	state *stateTracker

	seq int
}

func newTranslator(threadID, runID string) *translator {
	return &translator{
		threadID:       threadID,
		runID:          runID,
		messageID:      runID + "-msg",
		toolArgsSent:   make(map[string]int),
		toolEnded:      make(map[string]bool),
		toolSuppressed: make(map[string]bool),
	}
}

// suppressToolCall silences a call the client executed itself. The browser
// already has the result and has rendered it; re-emitting the END/RESULT pair
// would duplicate it in the visible transcript.
func (t *translator) suppressToolCall(callID string) {
	t.toolSuppressed[callID] = true
	t.toolEnded[callID] = true
}

// adoptToolCall records a call whose events the handler emitted itself, so the
// translator does not close it a second time.
func (t *translator) adoptToolCall(callID string) {
	t.toolEnded[callID] = true
}

// completeToolCall emits whatever of a call's START / ARGS / END the agent's
// stream has not already produced, and closes it.
//
// It exists because the agent only publishes tool_call events for providers that
// stream tool use incrementally; with the others the call reaches the tool — and
// therefore suspends the run — without ever appearing on the stream. Emitting
// from what the tool itself was handed is the only source that is always
// available, and it is authoritative: it is the exact call the model made.
func (t *translator) completeToolCall(callID, name, input string) []Event {
	if callID == "" || t.toolEnded[callID] {
		return nil
	}
	var out []Event
	sent, seen := t.toolArgsSent[callID]
	if !seen {
		out = append(out, t.closeReasoning()...)
		out = append(out, NewToolCallStart(callID, name, t.messageID))
		sent = 0
	}
	if len(input) > sent {
		out = append(out, NewToolCallArgs(callID, input[sent:]))
		t.toolArgsSent[callID] = len(input)
	}
	t.toolEnded[callID] = true
	return append(out, NewToolCallEnd(callID))
}

// endedSnapshot copies the calls this translator has already closed.
func (t *translator) endedSnapshot() map[string]bool {
	out := make(map[string]bool, len(t.toolEnded))
	for id, ended := range t.toolEnded {
		out[id] = ended
	}
	return out
}

// inheritEnded carries closed calls across a resumption.
//
// A tool that was mid-flight when the run suspended reports its result to the
// *next* run, whose translator never saw the call open. Without this the client
// would receive a second TOOL_CALL_END for a call it already watched close.
func (t *translator) inheritEnded(ended map[string]bool) *translator {
	for id, closed := range ended {
		if closed {
			t.toolEnded[id] = true
		}
	}
	return t
}

// withState attaches the shared-state tracker whose deltas are interleaved with
// the protocol events.
func (t *translator) withState(s *stateTracker) *translator {
	t.state = s
	return t
}

// nextID mints ids for synthetic messages (tool results have their own message
// in the AG-UI transcript).
func (t *translator) nextID(prefix string) string {
	t.seq++
	return fmt.Sprintf("%s-%s-%d", t.runID, prefix, t.seq)
}

// Start returns the events that open the run.
func (t *translator) Start() []Event {
	return []Event{NewRunStarted(t.threadID, t.runID)}
}

// Translate maps one Pando event onto the protocol, then folds the same event
// into the shared-state document. Both streams are interleaved on purpose: a
// STATE_DELTA that lands after the tool result it describes would make the page
// render a stale document for one frame.
func (t *translator) Translate(ev agent.AgentEvent) []Event {
	out := t.protocol(ev)
	if t.state != nil {
		out = append(out, t.state.Observe(ev)...)
	}
	return out
}

// protocol maps one Pando event to the AG-UI message stream. It returns nil for
// events with no counterpart worth emitting.
func (t *translator) protocol(ev agent.AgentEvent) []Event {
	switch ev.Type {
	case agent.AgentEventTypeContentDelta:
		if ev.Delta == "" {
			return nil
		}
		out := t.closeReasoning()
		if !t.textOpen {
			t.textOpen = true
			out = append(out, NewTextMessageStart(t.messageID))
		}
		return append(out, NewTextMessageContent(t.messageID, ev.Delta))

	case agent.AgentEventTypeThinkingDelta:
		if ev.Delta == "" {
			return nil
		}
		var out []Event
		if !t.reasoningOpen {
			t.reasoningOpen = true
			out = append(out,
				NewReasoningStart(t.messageID),
				NewReasoningMessageStart(t.messageID),
			)
		}
		return append(out, NewReasoningMessageContent(t.messageID, ev.Delta))

	case agent.AgentEventTypeToolCall:
		return t.toolCall(ev)

	case agent.AgentEventTypeToolResult:
		if ev.ToolResult == nil || t.toolSuppressed[ev.ToolResult.ToolCallID] {
			return nil
		}
		var out []Event
		// A result implies the call is complete even if the finished flag never
		// arrived on the call snapshot — and, with a provider that does not stream
		// tool use, even if the call itself never reached the stream. Closing a
		// call the client never saw open is a protocol error on its side, so the
		// result is what opens it.
		if _, seen := t.toolArgsSent[ev.ToolResult.ToolCallID]; !seen && !t.toolEnded[ev.ToolResult.ToolCallID] {
			out = append(out, t.closeReasoning()...)
			out = append(out, NewToolCallStart(ev.ToolResult.ToolCallID, ev.ToolResult.Name, t.messageID))
			t.toolArgsSent[ev.ToolResult.ToolCallID] = 0
		}
		if !t.toolEnded[ev.ToolResult.ToolCallID] {
			t.toolEnded[ev.ToolResult.ToolCallID] = true
			out = append(out, NewToolCallEnd(ev.ToolResult.ToolCallID))
		}
		return append(out, NewToolCallResult(
			t.nextID("toolresult"),
			ev.ToolResult.ToolCallID,
			ev.ToolResult.Content,
		))

	case agent.AgentEventTypeError:
		msg := "unknown error"
		if ev.Error != nil {
			msg = ev.Error.Error()
		}
		out := t.closeOpenBlocks()
		return append(out, NewRunError(msg, ""))

	case agent.AgentEventTypeTodosUpdated:
		if t.state != nil {
			// The tracker emits the STATE_DELTA on /todos.
			return nil
		}
		return []Event{NewCustom("pando.todos", ev.Todos)}

	case agent.AgentEventTypeTokenUsage:
		if ev.TokenUsage == nil || t.state != nil {
			return nil
		}
		return []Event{NewCustom("pando.tokenUsage", ev.TokenUsage)}

	case agent.AgentEventTypeSummarize:
		return []Event{NewCustom("pando.summarize", map[string]any{
			"progress": ev.Progress,
			"done":     ev.Done,
		})}

	case agent.AgentEventTypeSystemMessage,
		agent.AgentEventTypeSteeringQueued,
		agent.AgentEventTypeSteeringInjected,
		agent.AgentEventTypeConclusionQueued,
		agent.AgentEventTypeConclusionInjected,
		agent.AgentEventTypeResurrected:
		if ev.SystemMessage == "" {
			return nil
		}
		return []Event{NewCustom("pando."+string(ev.Type), ev.SystemMessage)}

	case agent.AgentEventTypeResponse:
		// Handled by Finish: the response event is what ends the run, and the
		// caller decides the outcome.
		return nil
	}
	return nil
}

// toolCall converts Pando's repeated tool-call snapshots into the AG-UI
// START / ARGS* / END sequence.
func (t *translator) toolCall(ev agent.AgentEvent) []Event {
	tc := ev.ToolCall
	if tc == nil || tc.ID == "" {
		return nil
	}
	var out []Event
	sent, seen := t.toolArgsSent[tc.ID]
	if !seen {
		// A tool call interrupts the assistant's text block in AG-UI's model
		// only conceptually; the block stays open so following text continues
		// the same message. Reasoning, however, must be closed.
		out = append(out, t.closeReasoning()...)
		out = append(out, NewToolCallStart(tc.ID, tc.Name, t.messageID))
		t.toolArgsSent[tc.ID] = 0
		sent = 0
	}
	if len(tc.Input) > sent {
		out = append(out, NewToolCallArgs(tc.ID, tc.Input[sent:]))
		t.toolArgsSent[tc.ID] = len(tc.Input)
	}
	if tc.Finished && !t.toolEnded[tc.ID] {
		t.toolEnded[tc.ID] = true
		out = append(out, NewToolCallEnd(tc.ID))
	}
	return out
}

// closeReasoning ends an open reasoning block, if any.
func (t *translator) closeReasoning() []Event {
	if !t.reasoningOpen {
		return nil
	}
	t.reasoningOpen = false
	return []Event{
		NewReasoningMessageEnd(t.messageID),
		NewReasoningEnd(t.messageID),
	}
}

// closeOpenBlocks ends every block still open, in protocol order.
func (t *translator) closeOpenBlocks() []Event {
	out := t.closeReasoning()
	for id := range t.toolArgsSent {
		if !t.toolEnded[id] {
			t.toolEnded[id] = true
			out = append(out, NewToolCallEnd(id))
		}
	}
	if t.textOpen {
		t.textOpen = false
		out = append(out, NewTextMessageEnd(t.messageID))
	}
	return out
}

// Finish closes any open block and terminates the run. result is the final
// assistant text, when the run produced one.
func (t *translator) Finish(outcome string, result any) []Event {
	out := t.closeOpenBlocks()
	return append(out, NewRunFinished(t.threadID, t.runID, outcome, result))
}

// Fail closes any open block and terminates the run with an error.
func (t *translator) Fail(msg, code string) []Event {
	out := t.closeOpenBlocks()
	return append(out, NewRunError(msg, code))
}
