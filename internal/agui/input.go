package agui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Message roles defined by AG-UI.
const (
	RoleDeveloper = "developer"
	RoleSystem    = "system"
	RoleAssistant = "assistant"
	RoleUser      = "user"
	RoleTool      = "tool"
	RoleActivity  = "activity"
	RoleReasoning = "reasoning"
)

// Multimodal input content kinds.
const (
	ContentText     = "text"
	ContentImage    = "image"
	ContentAudio    = "audio"
	ContentVideo    = "video"
	ContentDocument = "document"
)

// ErrInvalidInput is returned by DecodeRunAgentInput when the payload does not
// satisfy the protocol's minimum requirements.
var ErrInvalidInput = errors.New("agui: invalid RunAgentInput")

// RunAgentInput is the request body of an AG-UI run. It is sent in full on every
// turn: the client owns the visible transcript, so `Messages` carries the whole
// conversation even though Pando keeps its own history server-side.
type RunAgentInput struct {
	ThreadID       string    `json:"threadId"`
	RunID          string    `json:"runId"`
	ParentRunID    string    `json:"parentRunId,omitempty"`
	State          any       `json:"state,omitempty"`
	Messages       []Message `json:"messages,omitempty"`
	Tools          []Tool    `json:"tools,omitempty"`
	Context        []Context `json:"context,omitempty"`
	ForwardedProps any       `json:"forwardedProps,omitempty"`
}

// Tool is a frontend-declared tool. The agent may call it; the browser executes
// it and returns the outcome as a tool message on the next run.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// Context is ambient information the page attaches to the run.
type Context struct {
	Description string `json:"description"`
	Value       string `json:"value"`
}

// InputContent is one part of a multimodal user message.
type InputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// URL/Data carry non-text parts. Only text is consumed today; other kinds
	// are preserved so P2+ can map them onto message.Attachment.
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// MessageContent holds either a plain string or a list of multimodal parts.
// AG-UI allows both shapes on user messages.
type MessageContent struct {
	Text  string
	Parts []InputContent
}

// UnmarshalJSON accepts either a JSON string or an array of InputContent.
func (m *MessageContent) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*m = MessageContent{}
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*m = MessageContent{Text: s}
		return nil
	}
	var parts []InputContent
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	*m = MessageContent{Parts: parts}
	return nil
}

// MarshalJSON writes back the shape the content was received in.
func (m MessageContent) MarshalJSON() ([]byte, error) {
	if len(m.Parts) > 0 {
		return json.Marshal(m.Parts)
	}
	return json.Marshal(m.Text)
}

// String flattens the content to plain text, joining the text parts of a
// multimodal message. Non-text parts are skipped (see doc.go, P2).
func (m MessageContent) String() string {
	if len(m.Parts) == 0 {
		return m.Text
	}
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type != ContentText || p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// HasNonTextParts reports whether the message carries content this adapter
// cannot forward to the agent yet.
func (m MessageContent) HasNonTextParts() bool {
	for _, p := range m.Parts {
		if p.Type != ContentText {
			return true
		}
	}
	return false
}

// ToolCall is an assistant-issued call as echoed back by the client.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction is the OpenAI-shaped function payload of a tool call.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Message is one entry of the AG-UI conversation.
type Message struct {
	ID           string         `json:"id"`
	Role         string         `json:"role"`
	Content      MessageContent `json:"content,omitempty"`
	Name         string         `json:"name,omitempty"`
	ToolCalls    []ToolCall     `json:"toolCalls,omitempty"`
	ToolCallID   string         `json:"toolCallId,omitempty"`
	Error        string         `json:"error,omitempty"`
	ActivityType string         `json:"activityType,omitempty"`
}

// DecodeRunAgentInput reads and validates a RunAgentInput from an HTTP body.
// maxBytes caps the payload; pass 0 for the default.
func DecodeRunAgentInput(r io.Reader, maxBytes int64) (*RunAgentInput, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxRequestBytes
	}
	var in RunAgentInput
	dec := json.NewDecoder(io.LimitReader(r, maxBytes))
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	return &in, nil
}

// Validate enforces the protocol's required fields.
func (in *RunAgentInput) Validate() error {
	if strings.TrimSpace(in.ThreadID) == "" {
		return fmt.Errorf("%w: threadId is required", ErrInvalidInput)
	}
	if strings.TrimSpace(in.RunID) == "" {
		return fmt.Errorf("%w: runId is required", ErrInvalidInput)
	}
	for i, m := range in.Messages {
		if strings.TrimSpace(m.Role) == "" {
			return fmt.Errorf("%w: messages[%d] has no role", ErrInvalidInput, i)
		}
	}
	return nil
}

// LastUserMessage returns the trailing user message, which is the only part of
// the transcript this adapter forwards to Pando: the agent keeps its own
// history, so replaying the whole array would duplicate context and defeat
// compaction and prompt caching.
func (in *RunAgentInput) LastUserMessage() (Message, bool) {
	for i := len(in.Messages) - 1; i >= 0; i-- {
		if in.Messages[i].Role == RoleUser {
			return in.Messages[i], true
		}
	}
	return Message{}, false
}

// TrailingToolMessages returns the tool messages that appear after the last user
// message. They are frontend tool results resolving a previously interrupted
// run rather than a new prompt (P3 consumes them).
func (in *RunAgentInput) TrailingToolMessages() []Message {
	lastUser := -1
	for i := len(in.Messages) - 1; i >= 0; i-- {
		if in.Messages[i].Role == RoleUser {
			lastUser = i
			break
		}
	}
	var out []Message
	for i := lastUser + 1; i < len(in.Messages); i++ {
		if in.Messages[i].Role == RoleTool {
			out = append(out, in.Messages[i])
		}
	}
	return out
}

// ContextBlock renders the client-supplied context entries as a text block that
// can be prefixed to the prompt. Returns "" when there is no context.
func (in *RunAgentInput) ContextBlock() string {
	if len(in.Context) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<context>\n")
	for _, c := range in.Context {
		if c.Value == "" {
			continue
		}
		if c.Description != "" {
			fmt.Fprintf(&b, "%s: %s\n", c.Description, c.Value)
			continue
		}
		fmt.Fprintf(&b, "%s\n", c.Value)
	}
	b.WriteString("</context>")
	return b.String()
}
