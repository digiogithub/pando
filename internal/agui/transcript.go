package agui

import (
	"encoding/json"

	"github.com/digiogithub/pando/internal/message"
)

// Converting Pando's persisted message history into AG-UI's Message[] shape
// (PANDO-US-0015/0016).
//
// Both the thread-messages read route (threads.go) and the MESSAGES_SNAPSHOT
// resync (server.go's runPrelude) need the exact same conversion, so it lives
// here once: toAGUIMessages is the only place internal message.Message values
// become agui.Message values.

// toAGUIMessages converts a session's persisted history into AG-UI Message[].
//
// A Pando "tool" message can carry more than one ToolResult (one per call
// resolved in the same turn -- see agent.go's cancelled/pending tool-call
// handling), while AG-UI expects one message per tool result. Such a message
// is therefore expanded into that many entries, each keeping the parent
// message's id for readability but disambiguated by its own tool call id, so
// assistant/tool pairing (matching toolCallId) survives the conversion
// exactly.
func toAGUIMessages(msgs []message.Message) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == message.Tool {
			for _, tr := range m.ToolResults() {
				out = append(out, Message{
					ID:         m.ID + "-" + tr.ToolCallID,
					Role:       RoleTool,
					Content:    MessageContent{Text: tr.Content},
					Name:       tr.Name,
					ToolCallID: tr.ToolCallID,
				})
			}
			continue
		}
		out = append(out, Message{
			ID:        m.ID,
			Role:      aguiRole(m.Role),
			Content:   MessageContent{Text: m.Content().Text},
			ToolCalls: toAGUIToolCalls(m.ToolCalls()),
		})
	}
	return out
}

// aguiRole maps Pando's message roles onto AG-UI's. Every Pando role has a
// direct AG-UI counterpart; the default case exists only as a defensive
// fallback should a new Pando role be introduced without updating this map.
func aguiRole(r message.MessageRole) string {
	switch r {
	case message.User:
		return RoleUser
	case message.Assistant:
		return RoleAssistant
	case message.System:
		return RoleSystem
	case message.Tool:
		return RoleTool
	default:
		return string(r)
	}
}

// toAGUIToolCalls converts an assistant message's tool calls into AG-UI's
// OpenAI-shaped ToolCall entries. Returns nil (not an empty slice) for no
// calls, so a plain text message serializes without an empty "toolCalls": [].
func toAGUIToolCalls(tcs []message.ToolCall) []ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		typ := tc.Type
		if typ == "" {
			typ = "function"
		}
		out = append(out, ToolCall{
			ID:   tc.ID,
			Type: typ,
			Function: ToolCallFunction{
				Name:      tc.Name,
				Arguments: tc.Input,
			},
		})
	}
	return out
}

// buildMessagesSnapshot converts a session's history into a MESSAGES_SNAPSHOT
// event, capped to maxMessages/maxBytes (see capMessages). It is a pure
// function of already-fetched messages so it can run before the agent run
// starts (PANDO-US-0016's "do not let a large history block the stream").
func buildMessagesSnapshot(msgs []message.Message, maxMessages, maxBytes int) MessagesSnapshotEvent {
	kept, truncated := capMessages(toAGUIMessages(msgs), maxMessages, maxBytes)
	return NewMessagesSnapshot(kept, truncated)
}

// capMessages keeps the most recent messages within maxMessages and
// maxBytes, dropping the oldest ones first. Either limit <= 0 means no cap on
// that dimension. At least one message (the newest) is always kept when msgs
// is non-empty, even if it alone exceeds maxBytes: a truncated-but-non-empty
// snapshot is more useful to the client than an empty one.
func capMessages(msgs []Message, maxMessages, maxBytes int) (kept []Message, truncated bool) {
	if maxMessages > 0 && len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
		truncated = true
	}
	if maxBytes <= 0 || len(msgs) == 0 {
		return msgs, truncated
	}

	total := 0
	keepFrom := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		size := messageByteSize(msgs[i])
		if total+size > maxBytes && keepFrom < len(msgs) {
			break
		}
		total += size
		keepFrom = i
		if total >= maxBytes {
			break
		}
	}
	if keepFrom > 0 {
		msgs = msgs[keepFrom:]
		truncated = true
	}
	return msgs, truncated
}

// messageByteSize is the JSON-encoded size of one AG-UI message, used to
// budget MESSAGES_SNAPSHOT against maxBytes. A marshal failure (never
// expected for this struct) is treated as zero cost rather than failing the
// whole snapshot.
func messageByteSize(m Message) int {
	b, err := json.Marshal(m)
	if err != nil {
		return 0
	}
	return len(b)
}
