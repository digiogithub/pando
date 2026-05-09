package llmproxy

import (
	"encoding/json"
	"fmt"

	"github.com/digiogithub/pando/internal/message"
)

// OpenAI request types

type openAIChatMessage struct {
	Role       string           `json:"role"`
	Content    interface{}      `json:"content"` // string or array of parts
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Stream      bool                `json:"stream"`
	MaxTokens   *int64              `json:"max_tokens,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	Tools       []openAIToolDef     `json:"tools,omitempty"`
}

type openAIToolDef struct {
	Type     string            `json:"type"`
	Function openAIFunctionDef `json:"function"`
}

type openAIFunctionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// extractContentString extracts the text content from an openAIChatMessage's Content field.
// Content can be a string or an array of content parts.
func extractContentString(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var result string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if typ, ok := m["type"].(string); ok && typ == "text" {
					if text, ok := m["text"].(string); ok {
						result += text
					}
				}
			}
		}
		return result
	}
	// Try JSON marshal/unmarshal for edge cases
	if b, err := json.Marshal(content); err == nil {
		var s string
		if err := json.Unmarshal(b, &s); err == nil {
			return s
		}
	}
	return fmt.Sprintf("%v", content)
}

// extractSystemMessage finds the first "system" role message and returns its content string.
func extractSystemMessage(oaiMsgs []openAIChatMessage) string {
	for _, msg := range oaiMsgs {
		if msg.Role == "system" {
			return extractContentString(msg.Content)
		}
	}
	return ""
}

// openAIMessagesToInternal converts a slice of OpenAI messages to internal message.Message types.
// System messages are skipped (they are handled separately via extractSystemMessage).
func openAIMessagesToInternal(oaiMsgs []openAIChatMessage) []message.Message {
	var result []message.Message

	for _, oaiMsg := range oaiMsgs {
		switch oaiMsg.Role {
		case "system":
			// Skip - handled separately as system message
			continue
		case "user":
			msg := message.Message{
				Role:  message.User,
				Parts: extractParts(oaiMsg.Content),
			}
			result = append(result, msg)
		case "assistant":
			var parts []message.ContentPart

			// Add text content from content field
			contentParts := extractParts(oaiMsg.Content)
			parts = append(parts, contentParts...)

			// Add tool calls if present
			for _, tc := range oaiMsg.ToolCalls {
				parts = append(parts, message.ToolCall{
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: tc.Function.Arguments,
					Type:  tc.Type,
				})
			}

			msg := message.Message{
				Role:  message.Assistant,
				Parts: parts,
			}
			result = append(result, msg)
		case "tool":
			content := extractContentString(oaiMsg.Content)
			msg := message.Message{
				Role: message.Tool,
				Parts: []message.ContentPart{
					message.ToolResult{
						ToolCallID: oaiMsg.ToolCallID,
						Content:    content,
					},
				},
			}
			result = append(result, msg)
		}
	}

	return result
}

// extractParts converts an OpenAI content value (string or array) into internal ContentPart slice.
func extractParts(content interface{}) []message.ContentPart {
	if content == nil {
		return nil
	}

	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []message.ContentPart{message.TextContent{Text: v}}
	case []interface{}:
		var parts []message.ContentPart
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			switch typ {
			case "text":
				text, _ := m["text"].(string)
				parts = append(parts, message.TextContent{Text: text})
			case "image_url":
				if urlObj, ok := m["image_url"].(map[string]interface{}); ok {
					url, _ := urlObj["url"].(string)
					detail, _ := urlObj["detail"].(string)
					if url != "" {
						parts = append(parts, message.ImageURLContent{URL: url, Detail: detail})
					}
				}
			}
		}
		return parts
	}

	// Fallback: try to treat as string
	return []message.ContentPart{message.TextContent{Text: fmt.Sprintf("%v", content)}}
}
