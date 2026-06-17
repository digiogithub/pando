package llmproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

// Anthropic Messages API request/response types.

type anthropicMessagesRequest struct {
	Model       string                  `json:"model"`
	Messages    []anthropicInputMessage `json:"messages"`
	System      json.RawMessage         `json:"system,omitempty"` // string or array of text blocks
	MaxTokens   *int64                  `json:"max_tokens,omitempty"`
	Temperature *float64                `json:"temperature,omitempty"`
	Stream      bool                    `json:"stream,omitempty"`
	Tools       []anthropicToolDef      `json:"tools,omitempty"`
}

type anthropicInputMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or array of content blocks
}

type anthropicToolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema,omitempty"`
}

// anthropicContentBlock is a flattened view of every content-block variant we
// accept on input and emit on output.
type anthropicContentBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	// image
	Source *anthropicImageSource `json:"source,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"`       // "base64" | "url"
	MediaType string `json:"media_type"` // e.g. "image/png"
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type anthropicMessagesResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Model        string                  `json:"model"`
	Content      []anthropicContentBlock `json:"content"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        anthropicUsage          `json:"usage"`
}

func writeAnthropicError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    "invalid_request_error",
			"message": msg,
		},
	})
}

// anthropicStopReason maps an internal finish reason to an Anthropic stop_reason.
func anthropicStopReason(reason message.FinishReason, hasToolCalls bool) string {
	switch reason {
	case message.FinishReasonToolUse:
		return "tool_use"
	case message.FinishReasonMaxTokens:
		return "max_tokens"
	case message.FinishReasonEndTurn:
		return "end_turn"
	}
	if hasToolCalls {
		return "tool_use"
	}
	return "end_turn"
}

// handleAnthropicMessages handles POST /v1/messages (Anthropic Messages API),
// routing the request through any configured Pando provider.
func (s *LLMProxyServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req anthropicMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %s", err.Error()))
		return
	}
	if req.Model == "" {
		writeAnthropicError(w, http.StatusBadRequest, "model is required")
		return
	}

	resolved, ok := resolveModel(req.Model)
	if !ok {
		writeAnthropicError(w, http.StatusNotFound, fmt.Sprintf("model '%s' not found or no provider configured for it", req.Model))
		return
	}
	model := resolved.model
	account := resolved.account

	maxTokens := model.DefaultMaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	systemMsg := anthropicSystemString(req.System)
	reqTools := anthropicToolsToBaseTools(req.Tools)
	internalMsgs := anthropicMessagesToInternal(req.Messages)

	prov, err := provider.NewProviderFromAccount(account, model, maxTokens, systemMsg)
	if err != nil {
		writeAnthropicError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create provider: %s", err.Error()))
		return
	}

	msgID := "msg_" + uuid.New().String()
	ctx := r.Context()

	if req.Stream {
		s.streamAnthropicMessages(ctx, w, prov, internalMsgs, reqTools, msgID, string(model.ID))
		return
	}

	resp, err := prov.SendMessages(ctx, internalMsgs, reqTools)
	if err != nil {
		writeAnthropicError(w, http.StatusInternalServerError, fmt.Sprintf("provider error: %s", err.Error()))
		return
	}

	content := make([]anthropicContentBlock, 0, 1+len(resp.ToolCalls))
	if resp.Content != "" {
		content = append(content, anthropicContentBlock{Type: "text", Text: resp.Content})
	}
	for _, tc := range resp.ToolCalls {
		content = append(content, anthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: toolInputJSON(tc.Input),
		})
	}

	writeJSON(w, http.StatusOK, anthropicMessagesResponse{
		ID:           msgID,
		Type:         "message",
		Role:         "assistant",
		Model:        string(model.ID),
		Content:      content,
		StopReason:   anthropicStopReason(resp.FinishReason, len(resp.ToolCalls) > 0),
		StopSequence: nil,
		Usage: anthropicUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	})
}

// streamAnthropicMessages emits a server-sent-event stream following the
// Anthropic Messages streaming protocol.
func (s *LLMProxyServer) streamAnthropicMessages(
	ctx context.Context,
	w http.ResponseWriter,
	prov provider.Provider,
	msgs []message.Message,
	reqTools []tools.BaseTool,
	msgID, modelID string,
) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	writeEvent := func(event string, payload any) bool {
		data, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(data)); err != nil {
			return false
		}
		if canFlush {
			flusher.Flush()
		}
		return true
	}

	// message_start
	if !writeEvent("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"model":         modelID,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	}) {
		return
	}

	textBlockOpen := false
	openTextBlock := func() bool {
		if textBlockOpen {
			return true
		}
		textBlockOpen = true
		return writeEvent("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "text", "text": ""},
		})
	}
	closeTextBlock := func() bool {
		if !textBlockOpen {
			return true
		}
		textBlockOpen = false
		return writeEvent("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
	}

	events := prov.StreamResponse(ctx, msgs, reqTools)

	stopReason := "end_turn"
	var outputTokens int64
	blockIndex := 0

	for event := range events {
		switch event.Type {
		case provider.EventContentDelta:
			if event.Content == "" {
				continue
			}
			if !openTextBlock() {
				return
			}
			if !writeEvent("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": event.Content},
			}) {
				return
			}
		case provider.EventComplete:
			if !closeTextBlock() {
				return
			}
			if event.Response != nil {
				stopReason = anthropicStopReason(event.Response.FinishReason, len(event.Response.ToolCalls) > 0)
				outputTokens = event.Response.Usage.OutputTokens
				// Emit each tool call as its own content block.
				for _, tc := range event.Response.ToolCalls {
					blockIndex++
					idx := blockIndex
					if !writeEvent("content_block_start", map[string]any{
						"type":          "content_block_start",
						"index":         idx,
						"content_block": map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": map[string]any{}},
					}) {
						return
					}
					if !writeEvent("content_block_delta", map[string]any{
						"type":  "content_block_delta",
						"index": idx,
						"delta": map[string]any{"type": "input_json_delta", "partial_json": tc.Input},
					}) {
						return
					}
					if !writeEvent("content_block_stop", map[string]any{
						"type":  "content_block_stop",
						"index": idx,
					}) {
						return
					}
				}
			}
			writeEvent("message_delta", map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
				"usage": map[string]any{"output_tokens": outputTokens},
			})
			writeEvent("message_stop", map[string]any{"type": "message_stop"})
			return
		case provider.EventError:
			_ = closeTextBlock()
			errMsg := "stream error"
			if event.Error != nil {
				errMsg = event.Error.Error()
			}
			writeEvent("error", map[string]any{
				"type":  "error",
				"error": map[string]string{"type": "api_error", "message": errMsg},
			})
			return
		}
	}
}
