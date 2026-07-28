package agui

import "time"

// EventType is the AG-UI event discriminator. Values are serialized uppercase
// with underscores, matching the protocol's EventType enum.
type EventType string

const (
	EventTextMessageStart   EventType = "TEXT_MESSAGE_START"
	EventTextMessageContent EventType = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd     EventType = "TEXT_MESSAGE_END"
	EventTextMessageChunk   EventType = "TEXT_MESSAGE_CHUNK"

	EventToolCallStart  EventType = "TOOL_CALL_START"
	EventToolCallArgs   EventType = "TOOL_CALL_ARGS"
	EventToolCallEnd    EventType = "TOOL_CALL_END"
	EventToolCallResult EventType = "TOOL_CALL_RESULT"

	EventStateSnapshot    EventType = "STATE_SNAPSHOT"
	EventStateDelta       EventType = "STATE_DELTA"
	EventMessagesSnapshot EventType = "MESSAGES_SNAPSHOT"

	EventActivitySnapshot EventType = "ACTIVITY_SNAPSHOT"
	EventActivityDelta    EventType = "ACTIVITY_DELTA"

	EventRaw    EventType = "RAW"
	EventCustom EventType = "CUSTOM"

	EventRunStarted  EventType = "RUN_STARTED"
	EventRunFinished EventType = "RUN_FINISHED"
	EventRunError    EventType = "RUN_ERROR"

	EventStepStarted  EventType = "STEP_STARTED"
	EventStepFinished EventType = "STEP_FINISHED"

	EventReasoningStart          EventType = "REASONING_START"
	EventReasoningMessageStart   EventType = "REASONING_MESSAGE_START"
	EventReasoningMessageContent EventType = "REASONING_MESSAGE_CONTENT"
	EventReasoningMessageEnd     EventType = "REASONING_MESSAGE_END"
	EventReasoningEnd            EventType = "REASONING_END"
)

// Run outcomes reported by RunFinishedEvent.
const (
	// OutcomeSuccess marks a run that completed on its own.
	OutcomeSuccess = "success"
	// OutcomeInterrupt marks a run that stopped waiting for the client (a
	// frontend tool result, a human decision). The agent-side run may still be
	// alive; the client resumes it with the next request on the same thread.
	OutcomeInterrupt = "interrupt"
)

// Event is any AG-UI event. Implementations embed BaseEvent.
type Event interface {
	EventType() EventType
}

// BaseEvent carries the fields every AG-UI event shares.
type BaseEvent struct {
	Type      EventType `json:"type"`
	Timestamp int64     `json:"timestamp,omitempty"`
	RawEvent  any       `json:"rawEvent,omitempty"`
}

// EventType implements Event.
func (b BaseEvent) EventType() EventType { return b.Type }

func base(t EventType) BaseEvent {
	return BaseEvent{Type: t, Timestamp: time.Now().UnixMilli()}
}

// ---------------------------------------------------------------- lifecycle

type RunStartedEvent struct {
	BaseEvent
	ThreadID    string `json:"threadId"`
	RunID       string `json:"runId"`
	ParentRunID string `json:"parentRunId,omitempty"`
}

func NewRunStarted(threadID, runID string) RunStartedEvent {
	return RunStartedEvent{BaseEvent: base(EventRunStarted), ThreadID: threadID, RunID: runID}
}

type RunFinishedEvent struct {
	BaseEvent
	ThreadID string `json:"threadId"`
	RunID    string `json:"runId"`
	Outcome  string `json:"outcome,omitempty"`
	Result   any    `json:"result,omitempty"`
}

func NewRunFinished(threadID, runID, outcome string, result any) RunFinishedEvent {
	return RunFinishedEvent{
		BaseEvent: base(EventRunFinished),
		ThreadID:  threadID,
		RunID:     runID,
		Outcome:   outcome,
		Result:    result,
	}
}

type RunErrorEvent struct {
	BaseEvent
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

func NewRunError(msg, code string) RunErrorEvent {
	return RunErrorEvent{BaseEvent: base(EventRunError), Message: msg, Code: code}
}

type StepStartedEvent struct {
	BaseEvent
	StepName string `json:"stepName"`
}

func NewStepStarted(name string) StepStartedEvent {
	return StepStartedEvent{BaseEvent: base(EventStepStarted), StepName: name}
}

type StepFinishedEvent struct {
	BaseEvent
	StepName string `json:"stepName"`
}

func NewStepFinished(name string) StepFinishedEvent {
	return StepFinishedEvent{BaseEvent: base(EventStepFinished), StepName: name}
}

// ------------------------------------------------------------- text message

type TextMessageStartEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Role      string `json:"role"`
}

func NewTextMessageStart(messageID string) TextMessageStartEvent {
	return TextMessageStartEvent{
		BaseEvent: base(EventTextMessageStart),
		MessageID: messageID,
		Role:      RoleAssistant,
	}
}

type TextMessageContentEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
}

func NewTextMessageContent(messageID, delta string) TextMessageContentEvent {
	return TextMessageContentEvent{
		BaseEvent: base(EventTextMessageContent),
		MessageID: messageID,
		Delta:     delta,
	}
}

type TextMessageEndEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
}

func NewTextMessageEnd(messageID string) TextMessageEndEvent {
	return TextMessageEndEvent{BaseEvent: base(EventTextMessageEnd), MessageID: messageID}
}

// ---------------------------------------------------------------- tool call

type ToolCallStartEvent struct {
	BaseEvent
	ToolCallID      string `json:"toolCallId"`
	ToolCallName    string `json:"toolCallName"`
	ParentMessageID string `json:"parentMessageId,omitempty"`
}

func NewToolCallStart(id, name, parentMessageID string) ToolCallStartEvent {
	return ToolCallStartEvent{
		BaseEvent:       base(EventToolCallStart),
		ToolCallID:      id,
		ToolCallName:    name,
		ParentMessageID: parentMessageID,
	}
}

type ToolCallArgsEvent struct {
	BaseEvent
	ToolCallID string `json:"toolCallId"`
	Delta      string `json:"delta"`
}

func NewToolCallArgs(id, delta string) ToolCallArgsEvent {
	return ToolCallArgsEvent{BaseEvent: base(EventToolCallArgs), ToolCallID: id, Delta: delta}
}

type ToolCallEndEvent struct {
	BaseEvent
	ToolCallID string `json:"toolCallId"`
}

func NewToolCallEnd(id string) ToolCallEndEvent {
	return ToolCallEndEvent{BaseEvent: base(EventToolCallEnd), ToolCallID: id}
}

type ToolCallResultEvent struct {
	BaseEvent
	MessageID  string `json:"messageId"`
	ToolCallID string `json:"toolCallId"`
	Content    string `json:"content"`
	Role       string `json:"role,omitempty"`
}

func NewToolCallResult(messageID, toolCallID, content string) ToolCallResultEvent {
	return ToolCallResultEvent{
		BaseEvent:  base(EventToolCallResult),
		MessageID:  messageID,
		ToolCallID: toolCallID,
		Content:    content,
		Role:       RoleTool,
	}
}

// -------------------------------------------------------------------- state

// JSONPatchOperation is a single RFC-6902 operation as carried by StateDelta.
type JSONPatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
	From  string `json:"from,omitempty"`
}

type StateSnapshotEvent struct {
	BaseEvent
	Snapshot any `json:"snapshot"`
}

func NewStateSnapshot(snapshot any) StateSnapshotEvent {
	return StateSnapshotEvent{BaseEvent: base(EventStateSnapshot), Snapshot: snapshot}
}

type StateDeltaEvent struct {
	BaseEvent
	Delta []JSONPatchOperation `json:"delta"`
}

func NewStateDelta(ops []JSONPatchOperation) StateDeltaEvent {
	return StateDeltaEvent{BaseEvent: base(EventStateDelta), Delta: ops}
}

type MessagesSnapshotEvent struct {
	BaseEvent
	Messages []Message `json:"messages"`
}

func NewMessagesSnapshot(msgs []Message) MessagesSnapshotEvent {
	return MessagesSnapshotEvent{BaseEvent: base(EventMessagesSnapshot), Messages: msgs}
}

// ---------------------------------------------------------------- reasoning

type ReasoningStartEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
}

func NewReasoningStart(messageID string) ReasoningStartEvent {
	return ReasoningStartEvent{BaseEvent: base(EventReasoningStart), MessageID: messageID}
}

type ReasoningMessageStartEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Role      string `json:"role"`
}

func NewReasoningMessageStart(messageID string) ReasoningMessageStartEvent {
	return ReasoningMessageStartEvent{
		BaseEvent: base(EventReasoningMessageStart),
		MessageID: messageID,
		Role:      RoleAssistant,
	}
}

type ReasoningMessageContentEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
}

func NewReasoningMessageContent(messageID, delta string) ReasoningMessageContentEvent {
	return ReasoningMessageContentEvent{
		BaseEvent: base(EventReasoningMessageContent),
		MessageID: messageID,
		Delta:     delta,
	}
}

type ReasoningMessageEndEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
}

func NewReasoningMessageEnd(messageID string) ReasoningMessageEndEvent {
	return ReasoningMessageEndEvent{BaseEvent: base(EventReasoningMessageEnd), MessageID: messageID}
}

type ReasoningEndEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
}

func NewReasoningEnd(messageID string) ReasoningEndEvent {
	return ReasoningEndEvent{BaseEvent: base(EventReasoningEnd), MessageID: messageID}
}

// ----------------------------------------------------------------- activity

type ActivitySnapshotEvent struct {
	BaseEvent
	MessageID    string `json:"messageId"`
	ActivityType string `json:"activityType"`
	Content      any    `json:"content"`
	Replace      bool   `json:"replace,omitempty"`
}

func NewActivitySnapshot(messageID, activityType string, content any) ActivitySnapshotEvent {
	return ActivitySnapshotEvent{
		BaseEvent:    base(EventActivitySnapshot),
		MessageID:    messageID,
		ActivityType: activityType,
		Content:      content,
	}
}

// ------------------------------------------------------------ escape hatches

type CustomEvent struct {
	BaseEvent
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}

// NewCustom builds a CUSTOM event. Pando-specific signals that have no AG-UI
// counterpart are emitted this way, namespaced as "pando.<something>", so a
// generic AG-UI client can ignore them safely.
func NewCustom(name string, value any) CustomEvent {
	return CustomEvent{BaseEvent: base(EventCustom), Name: name, Value: value}
}

type RawEvent struct {
	BaseEvent
	Event  any    `json:"event"`
	Source string `json:"source,omitempty"`
}

func NewRaw(event any, source string) RawEvent {
	return RawEvent{BaseEvent: base(EventRaw), Event: event, Source: source}
}
