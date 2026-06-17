package llmproxy

import (
	"encoding/json"
	"strings"

	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

// anthropicSystemString flattens the Anthropic "system" field, which may be a
// plain string or an array of text content blocks, into a single string.
func anthropicSystemString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" {
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}

// anthropicToolsToBaseTools converts Anthropic tool definitions into proxy tools.
func anthropicToolsToBaseTools(defs []anthropicToolDef) []tools.BaseTool {
	if len(defs) == 0 {
		return nil
	}
	result := make([]tools.BaseTool, 0, len(defs))
	for _, d := range defs {
		if d.Name == "" {
			continue
		}
		result = append(result, schemaToProxyTool(d.Name, d.Description, d.InputSchema))
	}
	return result
}

// anthropicMessagesToInternal converts Anthropic input messages into internal
// message.Message values. Anthropic carries tool results inside user turns; we
// split those out into Tool-role messages as the providers expect.
func anthropicMessagesToInternal(msgs []anthropicInputMessage) []message.Message {
	var result []message.Message

	for _, m := range msgs {
		blocks := parseAnthropicContent(m.Content)

		switch m.Role {
		case "assistant":
			var parts []message.ContentPart
			for _, blk := range blocks {
				switch blk.Type {
				case "text":
					if blk.Text != "" {
						parts = append(parts, message.TextContent{Text: blk.Text})
					}
				case "tool_use":
					parts = append(parts, message.ToolCall{
						ID:    blk.ID,
						Name:  blk.Name,
						Input: string(blk.Input),
						Type:  "function",
					})
				}
			}
			if len(parts) > 0 {
				result = append(result, message.Message{Role: message.Assistant, Parts: parts})
			}
		case "user":
			var userParts []message.ContentPart
			var toolParts []message.ContentPart
			for _, blk := range blocks {
				switch blk.Type {
				case "text":
					if blk.Text != "" {
						userParts = append(userParts, message.TextContent{Text: blk.Text})
					}
				case "image":
					if blk.Source != nil {
						if blk.Source.Type == "url" && blk.Source.URL != "" {
							userParts = append(userParts, message.ImageURLContent{URL: blk.Source.URL})
						} else if blk.Source.Data != "" {
							url := "data:" + blk.Source.MediaType + ";base64," + blk.Source.Data
							userParts = append(userParts, message.ImageURLContent{URL: url})
						}
					}
				case "tool_result":
					toolParts = append(toolParts, message.ToolResult{
						ToolCallID: blk.ToolUseID,
						Content:    anthropicToolResultContent(blk.Content),
						IsError:    blk.IsError,
					})
				}
			}
			// Tool results form their own Tool-role message and must precede the
			// user's follow-up text in the conversation.
			if len(toolParts) > 0 {
				result = append(result, message.Message{Role: message.Tool, Parts: toolParts})
			}
			if len(userParts) > 0 {
				result = append(result, message.Message{Role: message.User, Parts: userParts})
			}
		}
	}

	return result
}

// parseAnthropicContent decodes a message content field that may be a plain
// string or an array of content blocks.
func parseAnthropicContent(raw json.RawMessage) []anthropicContentBlock {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil
		}
		return []anthropicContentBlock{{Type: "text", Text: s}}
	}
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		return blocks
	}
	return nil
}

// anthropicToolResultContent flattens a tool_result "content" value (string or
// array of text blocks) into a plain string.
func anthropicToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []anthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" {
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}

// toolInputJSON normalizes a tool-call argument string into a JSON object value
// suitable for the Anthropic "input" field. Invalid/empty input becomes "{}".
func toolInputJSON(input string) json.RawMessage {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return json.RawMessage("{}")
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	return json.RawMessage("{}")
}
