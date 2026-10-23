package provider

import (
	"encoding/json"
	"strings"

	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

// sanitizeToolCallArguments returns tool call arguments that are safe to replay
// to a provider as part of the conversation history.
//
// A tool call stored in history may carry arguments that are not a JSON object:
// a stream truncated by max_tokens in the middle of a large `write`, deltas lost
// by a provider adapter, or a model that emitted a bare string. Replaying such a
// call verbatim makes providers that validate history (Copilot, Anthropic,
// OpenAI) reject EVERY following request with a 400, even after switching
// model, because the broken call stays in the session. Arguments are repaired
// when possible; otherwise they are replaced by an empty object so the call and
// its matching tool result stay paired and the session remains usable.
func sanitizeToolCallArguments(toolName, input string) string {
	normalized, err := toolsPkg.NormalizeJSONInput(input)
	if err == nil {
		var obj map[string]any
		if err = json.Unmarshal([]byte(normalized), &obj); err == nil && obj != nil {
			return normalized
		}
	}
	logging.Warn("Replacing unparseable tool call arguments in history with {}", "tool", toolName, "error", err, "input_len", len(input))
	return "{}"
}

// sanitizeToolCallArgumentsMap is sanitizeToolCallArguments for providers that
// need the arguments decoded (Anthropic tool_use blocks).
func sanitizeToolCallArgumentsMap(toolName, input string) map[string]any {
	var obj map[string]any
	if err := json.Unmarshal([]byte(sanitizeToolCallArguments(toolName, input)), &obj); err != nil || obj == nil {
		return map[string]any{}
	}
	return obj
}

// dropTruncatedToolCalls removes tool calls whose arguments are not valid JSON
// from a response that stopped on max_tokens. Such a call was cut mid-stream
// (typically a large `write`); executing it after JSON repair would silently
// write a truncated file, so it is discarded and the turn ends as max_tokens.
func dropTruncatedToolCalls(calls []message.ToolCall) []message.ToolCall {
	kept := calls[:0]
	for _, call := range calls {
		if strings.TrimSpace(call.Input) != "" && !json.Valid([]byte(call.Input)) {
			logging.Warn("Dropping tool call truncated by max_tokens", "tool", call.Name, "input_len", len(call.Input))
			continue
		}
		kept = append(kept, call)
	}
	return kept
}

// isReplayableToolCall reports whether a tool call carries the ID and function
// name every provider requires. A stream cut by max_tokens can leave a call
// with arguments but no ID or name (Anthropic models served through Copilot
// interleave tool call deltas); Copilot rejects any history containing one
// with 400 "tool call must have a tool call ID and function name".
func isReplayableToolCall(call message.ToolCall) bool {
	return strings.TrimSpace(call.ID) != "" && strings.TrimSpace(call.Name) != ""
}

// dropIncompleteToolCalls removes tool calls without an ID or function name
// from a provider response, so they are never executed nor stored in history.
func dropIncompleteToolCalls(calls []message.ToolCall) []message.ToolCall {
	kept := calls[:0]
	for _, call := range calls {
		if !isReplayableToolCall(call) {
			logging.Warn("Dropping tool call without ID or function name", "id", call.ID, "tool", call.Name, "input_len", len(call.Input))
			continue
		}
		kept = append(kept, call)
	}
	return kept
}

// replayableToolCallIDs returns the IDs of the tool calls in history that can
// be replayed. Tool results whose call was skipped must be skipped too: a tool
// message that answers no assistant tool call is rejected just the same.
func replayableToolCallIDs(messages []message.Message) map[string]bool {
	ids := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != message.Assistant {
			continue
		}
		for _, call := range msg.ToolCalls() {
			if isReplayableToolCall(call) {
				ids[call.ID] = true
			}
		}
	}
	return ids
}
