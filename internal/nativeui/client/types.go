package client

import (
	"encoding/json"
	"time"
)

// The DTOs below mirror the wire format of internal/api (and the normalized
// types in web-ui/packages/pando-client/src/types). They are tolerant: unknown
// fields are ignored and missing ones default, so a newer server does not
// break an older desktop client.

// ServerInfo is GET /health.
type ServerInfo struct {
	Status           string `json:"status"`
	Version          string `json:"version"`
	StartupMode      string `json:"startup_mode"`
	ParentInstanceID string `json:"parent_instance_id"`
	ProjectID        string `json:"project_id"`
	ProjectName      string `json:"project_name"`
	PublicBasePath   string `json:"public_base_path"`
}

// Session is a chat session (PascalCase on the wire; timestamps are Unix
// seconds).
type Session struct {
	ID                  string  `json:"ID"`
	ParentSessionID     string  `json:"ParentSessionID"`
	Title               string  `json:"Title"`
	MessageCount        int64   `json:"MessageCount"`
	PromptTokens        int64   `json:"PromptTokens"`
	CompletionTokens    int64   `json:"CompletionTokens"`
	CacheReadTokens     int64   `json:"CacheReadTokens"`
	CacheCreationTokens int64   `json:"CacheCreationTokens"`
	ReasoningTokens     int64   `json:"ReasoningTokens"`
	SummaryMessageID    string  `json:"SummaryMessageID"`
	Cost                float64 `json:"Cost"`
	CreatedAt           int64   `json:"CreatedAt"`
	UpdatedAt           int64   `json:"UpdatedAt"`
}

// Updated returns UpdatedAt as a time.
func (s Session) Updated() time.Time { return time.Unix(s.UpdatedAt, 0) }

// SessionSummary is a session as listed by GET /api/v1/sessions and pushed by
// /api/v1/sessions/events, with its run state.
type SessionSummary struct {
	Session
	IsRunning     bool   `json:"is_running"`
	RunSeq        int64  `json:"run_seq"`
	PromptPreview string `json:"prompt_preview,omitempty"`
	BusyElsewhere bool   `json:"busy_elsewhere"`
}

// SessionList is a page of GET /api/v1/sessions (newest first).
type SessionList struct {
	Sessions []SessionSummary `json:"sessions"`
	Total    int              `json:"total"`
	Limit    int              `json:"limit"`
	Offset   int              `json:"offset"`
	HasMore  bool             `json:"has_more"`
}

// SessionDetail is GET /api/v1/sessions/{id}.
type SessionDetail struct {
	Session       Session   `json:"session"`
	Messages      []Message `json:"messages"`
	IsRunning     bool      `json:"is_running"`
	RunSeq        int64     `json:"run_seq"`
	BusyElsewhere bool      `json:"busy_elsewhere"`
}

// Message roles.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
	RoleTool      = "tool"
)

// Message is a history message.
type Message struct {
	ID        string `json:"ID"`
	Role      string `json:"Role"`
	SessionID string `json:"SessionID"`
	Parts     []Part `json:"Parts"`
	Model     string `json:"Model"`
	CreatedAt int64  `json:"CreatedAt"`
	UpdatedAt int64  `json:"UpdatedAt"`
}

// Text returns the concatenated text parts of the message.
func (m Message) Text() string {
	var out string
	for _, p := range m.Parts {
		if p.Kind == PartText {
			out += p.Text
		}
	}
	return out
}

// PartKind classifies a message part. Pando writes parts untagged, so the
// kind is inferred from the keys present (as the Rust and web clients do).
type PartKind int

// Part kinds.
const (
	PartOther PartKind = iota
	PartText
	PartThinking
	PartToolCall
	PartToolResult
	PartFinish
	PartImageURL
	PartBinary
)

// Part is one content part of a message.
type Part struct {
	Kind PartKind
	// Text holds the text of PartText and PartThinking, and the URL of
	// PartImageURL.
	Text string
	// ToolCall is set for PartToolCall.
	ToolCall *ToolCallPart
	// ToolResult is set for PartToolResult.
	ToolResult *ToolResultPart
	// FinishReason and FinishTime are set for PartFinish:
	// end_turn|max_tokens|tool_use|canceled|error|permission_denied|...
	FinishReason string
	FinishTime   int64
	// Path and MIMEType describe a PartBinary attachment.
	Path     string
	MIMEType string
	// Raw is the part as received.
	Raw json.RawMessage
}

// ToolCallPart is a tool call stored in an assistant message.
type ToolCallPart struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Finished bool   `json:"finished"`
}

// ToolResultPart is a tool result stored in a tool message.
type ToolResultPart struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	Metadata   string `json:"metadata"`
	IsError    bool   `json:"is_error"`
}

// UnmarshalJSON classifies a raw part object.
func (p *Part) UnmarshalJSON(b []byte) error {
	*p = Part{Raw: append(json.RawMessage(nil), b...)}
	var o map[string]json.RawMessage
	if err := json.Unmarshal(b, &o); err != nil {
		return nil // not an object: keep as PartOther
	}
	str := func(k string) (string, bool) {
		v, ok := o[k]
		if !ok {
			return "", false
		}
		var s string
		if json.Unmarshal(v, &s) != nil {
			return "", false
		}
		return s, true
	}
	_, hasID := o["id"]
	_, hasName := o["name"]
	_, hasInput := o["input"]
	switch {
	case has(o, "tool_call_id"):
		var r ToolResultPart
		if json.Unmarshal(b, &r) == nil {
			p.Kind, p.ToolResult = PartToolResult, &r
		}
	case hasID && hasName && hasInput:
		var c ToolCallPart
		if json.Unmarshal(b, &c) == nil {
			p.Kind, p.ToolCall = PartToolCall, &c
		}
	default:
		if t, ok := str("text"); ok {
			p.Kind, p.Text = PartText, t
		} else if t, ok := str("thinking"); ok {
			p.Kind, p.Text = PartThinking, t
		} else if r, ok := str("reason"); ok {
			p.Kind, p.FinishReason = PartFinish, r
			if v, ok := o["time"]; ok {
				_ = json.Unmarshal(v, &p.FinishTime)
			}
		} else if u, ok := str("url"); ok {
			p.Kind, p.Text = PartImageURL, u
		} else if has(o, "Path") && has(o, "Data") {
			p.Kind = PartBinary
			p.Path, _ = str("Path")
			p.MIMEType, _ = str("MIMEType")
		}
	}
	return nil
}

func has(o map[string]json.RawMessage, k string) bool {
	_, ok := o[k]
	return ok
}

// Pending is GET /api/v1/sessions/{id}/pending: the run state plus the
// permission and question requests waiting for an answer.
type Pending struct {
	Running     bool                `json:"running"`
	RunSeq      int64               `json:"run_seq"`
	Permissions []PermissionRequest `json:"permissions"`
	Questions   []QuestionRequest   `json:"questions"`
}

// Model is an entry of GET /api/v1/models.
type Model struct {
	ID                      string   `json:"id"`
	Name                    string   `json:"name"`
	Provider                string   `json:"provider"`
	AccountID               string   `json:"accountId,omitempty"`
	Description             string   `json:"description"`
	Badges                  []string `json:"badges"`
	CanReason               bool     `json:"canReason"`
	SupportsReasoningEffort bool     `json:"supportsReasoningEffort"`
	SupportsAttachments     bool     `json:"supportsAttachments"`
	ContextWindow           int64    `json:"contextWindow,omitempty"`
	MaxOutputTokens         int64    `json:"maxOutputTokens,omitempty"`
	CostPer1MIn             float64  `json:"costPer1MIn,omitempty"`
	CostPer1MOut            float64  `json:"costPer1MOut,omitempty"`
	RouterProvider          string   `json:"routerProvider,omitempty"`
	RouterModel             string   `json:"routerModel,omitempty"`
	RouterHealthy           bool     `json:"routerHealthy,omitempty"`
}

// ModelList is GET /api/v1/models.
type ModelList struct {
	Models       []Model           `json:"models"`
	Errors       map[string]string `json:"errors"`
	AutoSelected bool              `json:"autoSelected"`
}

// SessionModel is GET /api/v1/sessions/{id}/model.
type SessionModel struct {
	SessionID string `json:"sessionId"`
	// Model is the model the session runs on: its override, or the coder model.
	Model string `json:"model"`
	// Override reports whether Model comes from a session-scoped selection.
	Override     bool `json:"override"`
	AutoSelected bool `json:"autoSelected"`
}
