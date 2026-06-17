package llmproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/llm/provider"
)

// OpenAI response types

type openAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	Index        int                `json:"index"`
	Message      *openAIChatMessage `json:"message,omitempty"`
	Delta        *openAIChatMessage `json:"delta,omitempty"`
	FinishReason *string            `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type openAIStreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
}

type openAIError struct {
	Error openAIErrorDetail `json:"error"`
}

type openAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

func writeOpenAIError(w http.ResponseWriter, status int, detail openAIErrorDetail) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(openAIError{Error: detail})
}

// handleChatCompletions handles POST /v1/chat/completions.
func (s *LLMProxyServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, openAIErrorDetail{
			Message: "method not allowed",
			Type:    "invalid_request_error",
		})
		return
	}

	var req openAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, openAIErrorDetail{
			Message: fmt.Sprintf("invalid request body: %s", err.Error()),
			Type:    "invalid_request_error",
		})
		return
	}

	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, openAIErrorDetail{
			Message: "model is required",
			Type:    "invalid_request_error",
			Code:    "model_required",
		})
		return
	}

	// Resolve the requested model to a concrete model + provider account. This
	// accepts registry IDs, raw upstream model IDs and shorthand aliases, so any
	// model returned by GET /v1/models can actually be used here.
	resolved, ok := resolveModel(req.Model)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, openAIErrorDetail{
			Message: fmt.Sprintf("model '%s' not found or no provider configured for it", req.Model),
			Type:    "invalid_request_error",
			Code:    "model_not_found",
		})
		return
	}
	model := resolved.model
	account := resolved.account

	// Determine max tokens
	maxTokens := model.DefaultMaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	// Extract system message
	systemMsg := extractSystemMessage(req.Messages)

	// Convert inbound tool definitions so the upstream model can call them.
	reqTools := openAIToolsToBaseTools(req.Tools)

	// Create provider
	prov, err := provider.NewProviderFromAccount(account, model, maxTokens, systemMsg)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, openAIErrorDetail{
			Message: fmt.Sprintf("failed to create provider: %s", err.Error()),
			Type:    "server_error",
		})
		return
	}

	// Convert messages
	internalMsgs := openAIMessagesToInternal(req.Messages)

	chatID := "chatcmpl-" + uuid.New().String()
	created := time.Now().Unix()

	ctx := r.Context()

	if req.Stream {
		// Streaming response
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		flusher, canFlush := w.(http.Flusher)

		writeSSEChunk := func(chunk openAIStreamChunk) error {
			data, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", string(data))
			if err != nil {
				return err
			}
			if canFlush {
				flusher.Flush()
			}
			return nil
		}

		events := prov.StreamResponse(ctx, internalMsgs, reqTools)
		for event := range events {
			switch event.Type {
			case provider.EventContentDelta:
				chunk := openAIStreamChunk{
					ID:      chatID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   string(model.ID),
					Choices: []openAIChoice{
						{
							Index: 0,
							Delta: &openAIChatMessage{
								Role:    "assistant",
								Content: event.Content,
							},
							FinishReason: nil,
						},
					},
				}
				if err := writeSSEChunk(chunk); err != nil {
					return
				}
			case provider.EventComplete:
				stopReason := "stop"
				if event.Response != nil {
					stopReason = openAIFinishReason(event.Response.FinishReason, len(event.Response.ToolCalls) > 0)
					// Emit any tool calls (with their full arguments) before finishing.
					if len(event.Response.ToolCalls) > 0 {
						toolChunk := openAIStreamChunk{
							ID:      chatID,
							Object:  "chat.completion.chunk",
							Created: created,
							Model:   string(model.ID),
							Choices: []openAIChoice{
								{
									Index: 0,
									Delta: &openAIChatMessage{
										Role:      "assistant",
										ToolCalls: toolCallsToOpenAI(event.Response.ToolCalls),
									},
								},
							},
						}
						if err := writeSSEChunk(toolChunk); err != nil {
							return
						}
					}
				}
				chunk := openAIStreamChunk{
					ID:      chatID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   string(model.ID),
					Choices: []openAIChoice{
						{
							Index:        0,
							Delta:        &openAIChatMessage{},
							FinishReason: &stopReason,
						},
					},
				}
				if err := writeSSEChunk(chunk); err != nil {
					return
				}
				// Write the [DONE] sentinel
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				if canFlush {
					flusher.Flush()
				}
				return
			case provider.EventError:
				errMsg := "stream error"
				if event.Error != nil {
					errMsg = event.Error.Error()
				}
				errDetail := openAIErrorDetail{
					Message: errMsg,
					Type:    "server_error",
				}
				errData, _ := json.Marshal(openAIError{Error: errDetail})
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(errData))
				if canFlush {
					flusher.Flush()
				}
				return
			}
		}
	} else {
		// Non-streaming response
		resp, err := prov.SendMessages(ctx, internalMsgs, reqTools)
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, openAIErrorDetail{
				Message: fmt.Sprintf("provider error: %s", err.Error()),
				Type:    "server_error",
			})
			return
		}

		stopReason := openAIFinishReason(resp.FinishReason, len(resp.ToolCalls) > 0)
		respMsg := &openAIChatMessage{
			Role:    "assistant",
			Content: resp.Content,
		}
		if len(resp.ToolCalls) > 0 {
			respMsg.ToolCalls = toolCallsToOpenAI(resp.ToolCalls)
		}
		oaiResp := openAIChatResponse{
			ID:      chatID,
			Object:  "chat.completion",
			Created: created,
			Model:   string(model.ID),
			Choices: []openAIChoice{
				{
					Index:        0,
					Message:      respMsg,
					FinishReason: &stopReason,
				},
			},
			Usage: openAIUsage{
				PromptTokens:     resp.Usage.InputTokens,
				CompletionTokens: resp.Usage.OutputTokens,
				TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
			},
		}

		writeJSON(w, http.StatusOK, oaiResp)
	}
}
