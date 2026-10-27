package client

import "encoding/json"

// Stream event types written by POST /api/v1/chat/stream and
// GET /api/v1/sessions/{id}/stream (internal/api/handlers_chat.go).
const (
	EventSession            = "session"
	EventRun                = "run"
	EventContentDelta       = "content_delta"
	EventThinkingDelta      = "thinking_delta"
	EventToolCall           = "tool_call"
	EventToolCallUpdate     = "tool_call_update"
	EventToolResult         = "tool_result"
	EventPlanUpdate         = "plan_update"
	EventTodosUpdate        = "todos_update"
	EventGoalStatus         = "goal_status"
	EventTokenUsage         = "token_usage"
	EventPermissionRequest  = "permission_request"
	EventQuestionRequest    = "question_request"
	EventSteeringQueued     = "steering_queued"
	EventSteeringInjected   = "steering_injected"
	EventConclusionQueued   = "conclusion_queued"
	EventConclusionInjected = "conclusion_injected"
	EventResurrected        = "resurrected"
	EventSystemMessage      = "system_message"
	EventError              = "error"
	EventDone               = "done"

	// EventReattach is synthetic: the client emits it right before it
	// reattaches to a run whose stream closed without `done`. The server
	// replays the run from its start, so consumers must discard the partial
	// assistant turn they built from this run.
	EventReattach = "reattach"
)

// StreamEvent is one decoded chat stream event. Only the fields of its Type
// are set; Raw always holds the JSON payload, so views can read fields this
// struct does not model yet.
type StreamEvent struct {
	Type      string
	SessionID string
	// Running is set by `session`.
	Running bool
	// RunSeq is set by `run`.
	RunSeq int64
	// Text is the delta of content_delta / thinking_delta and the text of
	// system_message.
	Text string
	// Message is the note of steering / conclusion / resurrection events.
	Message string
	// Error is set by `error`.
	Error string

	ToolCall       *ToolCallEvent
	ToolCallUpdate *ToolCallUpdateEvent
	ToolResult     *ToolResultEvent
	Plan           []PlanEntry
	Todos          []Todo
	// Goal is set by goal_status; nil means "no goal".
	Goal              *GoalStatus
	TokenUsage        *TokenUsage
	PermissionRequest *PermissionRequest
	QuestionRequest   *QuestionRequest
	// Routing and ContextFilter are the raw objects of system_message.
	Routing       json.RawMessage
	ContextFilter json.RawMessage

	Raw json.RawMessage
}

// HasContent reports whether the event adds visible content to a turn.
func (e StreamEvent) HasContent() bool {
	switch e.Type {
	case EventContentDelta, EventThinkingDelta, EventToolCall, EventToolResult, EventPlanUpdate:
		return true
	}
	return false
}

// Location is a file a tool call touches.
type Location struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// ToolCallEvent is the `tool_call` payload. Kind is one of execute, edit,
// read, search, fetch, think, switch_mode, other; Status one of pending,
// in_progress, completed, failed.
type ToolCallEvent struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Input     string     `json:"input"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Locations []Location `json:"locations"`
}

// ToolCallUpdateEvent is the `tool_call_update` payload.
type ToolCallUpdateEvent struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Input     string     `json:"input"`
	Locations []Location `json:"locations"`
}

// ToolResultEvent is the `tool_result` payload.
type ToolResultEvent struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Content    string          `json:"content"`
	IsError    bool            `json:"is_error"`
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	Status     string          `json:"status"`
	Locations  []Location      `json:"locations"`
	Images     []string        `json:"images"`
	RawOutput  json.RawMessage `json:"raw_output"`
	Terminal   *ToolTerminal   `json:"terminal"`
	Diff       *ToolDiff       `json:"diff"`
}

// ToolTerminal is the terminal metadata of a command result.
type ToolTerminal struct {
	TerminalID string `json:"terminal_id"`
	ExitCode   int    `json:"exit_code"`
	Signal     string `json:"signal,omitempty"`
}

// ToolDiff is the diff metadata of an edit result.
type ToolDiff struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// PlanEntry is an entry of `plan_update`.
type PlanEntry struct {
	Title      string `json:"title"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form,omitempty"`
	Priority   string `json:"priority,omitempty"`
}

// Todo is an entry of `todos_update`.
type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	Priority   string `json:"priority,omitempty"`
	ActiveForm string `json:"activeForm,omitempty"`
}

// GoalStatus is the `goal_status` goal.
type GoalStatus struct {
	Objective     string `json:"objective"`
	Status        string `json:"status"`
	Iteration     int64  `json:"iteration"`
	MaxIterations int64  `json:"maxIterations"`
	Progress      string `json:"progress,omitempty"`
	NextStep      string `json:"nextStep,omitempty"`
	StartedAt     int64  `json:"startedAt"`
	CompletedAt   *int64 `json:"completedAt,omitempty"`
}

// TokenUsage is the `token_usage` payload.
type TokenUsage struct {
	PromptTokens        int64   `json:"prompt_tokens"`
	CompletionTokens    int64   `json:"completion_tokens"`
	ContextWindow       int64   `json:"context_window"`
	Estimated           bool    `json:"estimated"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	ReasoningTokens     int64   `json:"reasoning_tokens"`
	Cost                float64 `json:"cost"`
}

// PermissionRequest is a tool permission prompt (`permission_request`, and
// the entries of /pending).
type PermissionRequest struct {
	ID                      string          `json:"id"`
	SessionID               string          `json:"session_id"`
	ToolName                string          `json:"tool_name"`
	Description             string          `json:"description,omitempty"`
	Action                  string          `json:"action,omitempty"`
	Path                    string          `json:"path,omitempty"`
	Params                  json.RawMessage `json:"params,omitempty"`
	Justification           string          `json:"justification,omitempty"`
	GrantKey                string          `json:"grant_key,omitempty"`
	RequireExplicitApproval bool            `json:"require_explicit_approval,omitempty"`
	NeverAutoApprove        bool            `json:"never_auto_approve,omitempty"`
}

// QuestionRequest is an AskUserQuestion prompt (`question_request`).
type QuestionRequest struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	Questions []Question `json:"questions"`
}

// Question is one question of a QuestionRequest.
type Question struct {
	ID          string           `json:"id"`
	Header      string           `json:"header"`
	Question    string           `json:"question"`
	MultiSelect bool             `json:"multi_select"`
	Options     []QuestionOption `json:"options"`
}

// QuestionOption is a choice of a Question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ParseStreamEvent decodes an SSE frame of a chat stream. Unknown event
// types and malformed payloads still produce an event (with Raw), so callers
// can log or ignore them.
func ParseStreamEvent(f SSEFrame) StreamEvent {
	ev := StreamEvent{Type: f.Event, Raw: json.RawMessage(f.Data)}
	if ev.Type == "" {
		ev.Type = "content"
	}
	var base struct {
		SessionIDCamel string `json:"sessionId"`
		SessionIDSnake string `json:"session_id"`
		Text           string `json:"text"`
		Error          string `json:"error"`
		Message        string `json:"message"`
		Running        bool   `json:"running"`
		RunSeq         int64  `json:"runSeq"`
	}
	if json.Unmarshal([]byte(f.Data), &base) != nil {
		return ev
	}
	ev.SessionID = base.SessionIDCamel
	if ev.SessionID == "" {
		ev.SessionID = base.SessionIDSnake
	}
	ev.Text, ev.Error, ev.Message = base.Text, base.Error, base.Message
	data := []byte(f.Data)
	switch ev.Type {
	case EventSession:
		ev.Running = base.Running
	case EventRun:
		ev.RunSeq = base.RunSeq
	case EventToolCall:
		var tc ToolCallEvent
		if json.Unmarshal(data, &tc) == nil && tc.ID != "" {
			ev.ToolCall = &tc
		}
	case EventToolCallUpdate:
		var u ToolCallUpdateEvent
		if json.Unmarshal(data, &u) == nil && u.ID != "" {
			ev.ToolCallUpdate = &u
		}
	case EventToolResult:
		var r ToolResultEvent
		if json.Unmarshal(data, &r) == nil && r.ToolCallID != "" {
			ev.ToolResult = &r
		}
	case EventPlanUpdate:
		var p struct {
			Entries []PlanEntry `json:"entries"`
		}
		if json.Unmarshal(data, &p) == nil {
			ev.Plan = p.Entries
		}
	case EventTodosUpdate:
		var t struct {
			Todos []Todo `json:"todos"`
		}
		if json.Unmarshal(data, &t) == nil {
			ev.Todos = t.Todos
		}
	case EventGoalStatus:
		var g struct {
			Goal *GoalStatus `json:"goal"`
		}
		if json.Unmarshal(data, &g) == nil {
			ev.Goal = g.Goal
		}
	case EventTokenUsage:
		var u TokenUsage
		if json.Unmarshal(data, &u) == nil {
			ev.TokenUsage = &u
		}
	case EventPermissionRequest:
		var p PermissionRequest
		if json.Unmarshal(data, &p) == nil && p.ID != "" {
			ev.PermissionRequest = &p
		}
	case EventQuestionRequest:
		var q QuestionRequest
		if json.Unmarshal(data, &q) == nil && q.ID != "" {
			ev.QuestionRequest = &q
		}
	case EventSystemMessage:
		var s struct {
			Routing       json.RawMessage `json:"routing"`
			ContextFilter json.RawMessage `json:"context_filter"`
		}
		if json.Unmarshal(data, &s) == nil {
			ev.Routing, ev.ContextFilter = nullToNil(s.Routing), nullToNil(s.ContextFilter)
		}
	}
	return ev
}

func nullToNil(r json.RawMessage) json.RawMessage {
	if len(r) == 0 || string(r) == "null" {
		return nil
	}
	return r
}
