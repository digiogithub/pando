package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/digiogithub/pando/internal/config"
	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

// anthropicFilesBeta is the beta header enabling the Files API and file_id image
// sources on the Messages endpoint.
const anthropicFilesBeta = "files-api-2025-04-14"

// useBetaAPI reports whether requests should go through the beta Messages API
// (which supports referencing images by Files-API file_id). Opt-in via
// [Image] UseFilesAPI. Not available on Bedrock (no Files API there).
func (a *anthropicClient) useBetaAPI() bool {
	if a.options.useBedrock {
		return false
	}
	cfg := config.Get()
	return cfg != nil && cfg.Image.UseFilesAPI
}

// betaRequestOptions returns the per-request options for a beta call: the
// thinking options plus the Files beta header.
func (a *anthropicClient) betaRequestOptions() []option.RequestOption {
	opts := a.thinkingRequestOptions()
	return append(opts, option.WithHeader("anthropic-beta", anthropicFilesBeta))
}

// uploadImage uploads image bytes to the Anthropic Files API and returns the
// file_id, memoized by content hash so identical images upload only once. On any
// failure it returns "" so the caller falls back to an inline base64 block.
func (a *anthropicClient) uploadImage(ctx context.Context, data []byte, mime string) string {
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:])

	a.fileIDMu.Lock()
	if a.fileIDCache == nil {
		a.fileIDCache = make(map[string]string)
	}
	if id, ok := a.fileIDCache[key]; ok {
		a.fileIDMu.Unlock()
		return id
	}
	a.fileIDMu.Unlock()

	filename := "image." + mimeExtension(mime)
	meta, err := a.client.Beta.Files.Upload(ctx, anthropic.BetaFileUploadParams{
		File:  anthropic.File(bytes.NewReader(data), filename, mime),
		Betas: []anthropic.AnthropicBeta{anthropicFilesBeta},
	})
	if err != nil || meta == nil || meta.ID == "" {
		if err != nil {
			logging.Warn("Files API upload failed, falling back to base64", "error", err)
		}
		return ""
	}

	a.fileIDMu.Lock()
	a.fileIDCache[key] = meta.ID
	a.fileIDMu.Unlock()
	return meta.ID
}

func mimeExtension(mime string) string {
	switch mime {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "img"
	}
}

// betaImageBlock builds a beta image block for the given bytes, preferring a
// Files-API file_id reference (uploaded once, small payload every turn) and
// falling back to an inline base64 source.
func (a *anthropicClient) betaImageBlock(ctx context.Context, mime string, data []byte) anthropic.BetaImageBlockParam {
	if fileID := a.uploadImage(ctx, data, mime); fileID != "" {
		return anthropic.BetaImageBlockParam{
			Source: anthropic.BetaImageBlockParamSourceUnion{
				OfFile: &anthropic.BetaFileImageSourceParam{FileID: fileID},
			},
		}
	}
	return anthropic.BetaImageBlockParam{
		Source: anthropic.BetaImageBlockParamSourceUnion{
			OfBase64: &anthropic.BetaBase64ImageSourceParam{
				Data:      base64.StdEncoding.EncodeToString(data),
				MediaType: anthropic.BetaBase64ImageSourceMediaType(mime),
			},
		},
	}
}

// convertMessagesBeta mirrors convertMessages using beta content blocks, routing
// every image through the Files API (file_id) when possible.
func (a *anthropicClient) convertMessagesBeta(ctx context.Context, messages []message.Message) []anthropic.BetaMessageParam {
	var out []anthropic.BetaMessageParam
	for i, msg := range messages {
		cache := i > len(messages)-3

		switch msg.Role {
		case message.User:
			text := anthropic.BetaTextBlockParam{Text: msg.Content().String()}
			if cache && !a.options.disableCache {
				text.CacheControl = anthropic.BetaCacheControlEphemeralParam{Type: "ephemeral"}
			}
			blocks := []anthropic.BetaContentBlockParamUnion{{OfText: &text}}
			for _, bin := range msg.BinaryContent() {
				img := a.betaImageBlock(ctx, bin.MIMEType, bin.Data)
				blocks = append(blocks, anthropic.BetaContentBlockParamUnion{OfImage: &img})
			}
			out = append(out, anthropic.NewBetaUserMessage(blocks...))

		case message.Assistant:
			var blocks []anthropic.BetaContentBlockParamUnion
			if msg.Content().String() != "" {
				text := anthropic.BetaTextBlockParam{Text: msg.Content().String()}
				if cache && !a.options.disableCache {
					text.CacheControl = anthropic.BetaCacheControlEphemeralParam{Type: "ephemeral"}
				}
				blocks = append(blocks, anthropic.BetaContentBlockParamUnion{OfText: &text})
			}
			for _, toolCall := range msg.ToolCalls() {
				input := toolCall.Input
				if normalized, err := toolsPkg.NormalizeJSONInput(input); err == nil {
					input = normalized
				}
				var inputMap map[string]any
				if err := json.Unmarshal([]byte(input), &inputMap); err != nil {
					logging.Warn("Skipping tool call with unparseable arguments", "tool", toolCall.Name, "error", err)
					continue
				}
				blocks = append(blocks, anthropic.NewBetaToolUseBlock(toolCall.ID, inputMap, toolCall.Name))
			}
			if len(blocks) == 0 {
				continue
			}
			out = append(out, anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: blocks})

		case message.Tool:
			var results []anthropic.BetaContentBlockParamUnion
			for _, toolResult := range msg.ToolResults() {
				if a.providerOptions.model.SupportsAttachments && len(toolResult.Images) > 0 {
					content := []anthropic.BetaToolResultBlockParamContentUnion{
						{OfText: &anthropic.BetaTextBlockParam{Text: toolResult.Content}},
					}
					for _, bin := range toolResult.Images {
						img := a.betaImageBlock(ctx, bin.MIMEType, bin.Data)
						content = append(content, anthropic.BetaToolResultBlockParamContentUnion{OfImage: &img})
					}
					results = append(results, anthropic.BetaContentBlockParamUnion{OfToolResult: &anthropic.BetaToolResultBlockParam{
						ToolUseID: toolResult.ToolCallID,
						IsError:   anthropic.Bool(toolResult.IsError),
						Content:   content,
					}})
					continue
				}
				results = append(results, anthropic.NewBetaToolResultBlock(toolResult.ToolCallID, toolResult.Content, toolResult.IsError))
			}
			out = append(out, anthropic.NewBetaUserMessage(results...))
		}
	}
	return out
}

func (a *anthropicClient) convertToolsBeta(tools []toolsPkg.BaseTool) []anthropic.BetaToolUnionParam {
	betaTools := make([]anthropic.BetaToolUnionParam, len(tools))
	for i, tool := range tools {
		info := tool.Info()
		toolParam := anthropic.BetaToolParam{
			Name:        info.Name,
			Description: anthropic.String(info.Description),
			InputSchema: anthropic.BetaToolInputSchemaParam{
				Properties: info.Parameters,
				Required:   info.Required,
			},
		}
		if i == len(tools)-1 && !a.options.disableCache {
			toolParam.CacheControl = anthropic.BetaCacheControlEphemeralParam{Type: "ephemeral"}
		}
		betaTools[i] = anthropic.BetaToolUnionParam{OfTool: &toolParam}
	}
	return betaTools
}

func (a *anthropicClient) preparedMessagesBeta(messages []anthropic.BetaMessageParam, tools []anthropic.BetaToolUnionParam) anthropic.BetaMessageNewParams {
	var thinkingParam anthropic.BetaThinkingConfigParamUnion
	temperature := anthropic.Float(0)
	if budgetTokens := thinkingBudgetTokens(a.options.thinkingMode, a.options.reasoningEffort, a.providerOptions.maxTokens); budgetTokens > 0 {
		if !isAdaptiveThinkingModel(a.providerOptions.model.APIModel) {
			thinkingParam = anthropic.BetaThinkingConfigParamOfEnabled(budgetTokens)
		}
		temperature = anthropic.Float(1)
	}

	return anthropic.BetaMessageNewParams{
		Model:       anthropic.Model(a.providerOptions.model.APIModel),
		MaxTokens:   a.providerOptions.maxTokens,
		Temperature: temperature,
		Messages:    messages,
		Tools:       tools,
		Thinking:    thinkingParam,
		System: []anthropic.BetaTextBlockParam{
			{
				Text:         a.providerOptions.systemMessage,
				CacheControl: anthropic.BetaCacheControlEphemeralParam{Type: "ephemeral"},
			},
		},
	}
}

func (a *anthropicClient) toolCallsBeta(msg anthropic.BetaMessage) []message.ToolCall {
	var toolCalls []message.ToolCall
	for _, block := range msg.Content {
		if variant, ok := block.AsAny().(anthropic.BetaToolUseBlock); ok {
			input := variant.JSON.Input.Raw()
			if input == "" {
				input = "{}"
				if variant.Input != nil {
					if raw, err := json.Marshal(variant.Input); err == nil {
						input = string(raw)
					}
				}
			}
			toolCalls = append(toolCalls, message.ToolCall{
				ID:       variant.ID,
				Name:     variant.Name,
				Input:    input,
				Type:     string(variant.Type),
				Finished: true,
			})
		}
	}
	return toolCalls
}

func (a *anthropicClient) usageBeta(msg anthropic.BetaMessage) TokenUsage {
	return TokenUsage{
		InputTokens:         msg.Usage.InputTokens,
		OutputTokens:        msg.Usage.OutputTokens,
		CacheCreationTokens: msg.Usage.CacheCreationInputTokens,
		CacheReadTokens:     msg.Usage.CacheReadInputTokens,
	}
}

func (a *anthropicClient) sendBeta(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) (*ProviderResponse, error) {
	prepared := a.preparedMessagesBeta(a.convertMessagesBeta(ctx, messages), a.convertToolsBeta(tools))
	requestOptions := a.betaRequestOptions()
	cfg := config.Get()
	if cfg != nil && cfg.Debug {
		jsonData, _ := json.Marshal(prepared)
		logging.Debug("Prepared beta messages", "messages", string(jsonData))
	}

	attempts := 0
	for {
		attempts++
		resp, err := a.client.Beta.Messages.New(ctx, prepared, requestOptions...)
		if err != nil {
			logging.Error("Error in Anthropic beta API call", "error", err)
			if inputTokens, contextLimit, ok := parseContextOverflowError(err); ok {
				const safetyBuffer = 1000
				const floorOutputTokens = 3000
				available := contextLimit - inputTokens - safetyBuffer
				if available >= floorOutputTokens {
					prepared.MaxTokens = max(int64(floorOutputTokens), available)
					continue
				}
			}
			retry, after, retryErr := a.shouldRetry(attempts, err)
			if retryErr != nil {
				return nil, retryErr
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying (attempt %d/%d)...", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			return nil, retryErr
		}

		content := ""
		for _, block := range resp.Content {
			if text, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
				content += text.Text
			}
		}
		return &ProviderResponse{
			Content:   content,
			ToolCalls: a.toolCallsBeta(*resp),
			Usage:     a.usageBeta(*resp),
		}, nil
	}
}

func (a *anthropicClient) streamBeta(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	prepared := a.preparedMessagesBeta(a.convertMessagesBeta(ctx, messages), a.convertToolsBeta(tools))
	requestOptions := a.betaRequestOptions()
	cfg := config.Get()
	if cfg != nil && cfg.Debug {
		jsonData, _ := json.Marshal(prepared)
		logging.Debug("Prepared beta messages", "messages", string(jsonData))
	}

	attempts := 0
	eventChan := make(chan ProviderEvent)
	go func() {
		for {
			attempts++
			stream := a.client.Beta.Messages.NewStreaming(ctx, prepared, requestOptions...)
			acc := anthropic.BetaMessage{}
			currentToolCallID := ""

			for stream.Next() {
				event := stream.Current()
				if err := acc.Accumulate(event); err != nil {
					logging.Warn("Error accumulating beta message", "error", err)
					continue
				}

				switch event := event.AsAny().(type) {
				case anthropic.BetaRawContentBlockStartEvent:
					if event.ContentBlock.Type == "text" {
						eventChan <- ProviderEvent{Type: EventContentStart}
					} else if event.ContentBlock.Type == "tool_use" {
						currentToolCallID = event.ContentBlock.ID
						eventChan <- ProviderEvent{
							Type:     EventToolUseStart,
							ToolCall: &message.ToolCall{ID: event.ContentBlock.ID, Name: event.ContentBlock.Name, Finished: false},
						}
					}
				case anthropic.BetaRawContentBlockDeltaEvent:
					if event.Delta.Type == "thinking_delta" && event.Delta.Thinking != "" {
						eventChan <- ProviderEvent{Type: EventThinkingDelta, Thinking: event.Delta.Thinking}
					} else if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
						eventChan <- ProviderEvent{Type: EventContentDelta, Content: event.Delta.Text}
					} else if event.Delta.Type == "input_json_delta" && currentToolCallID != "" {
						eventChan <- ProviderEvent{
							Type:     EventToolUseDelta,
							ToolCall: &message.ToolCall{ID: currentToolCallID, Finished: false, Input: event.Delta.PartialJSON},
						}
					}
				case anthropic.BetaRawContentBlockStopEvent:
					if currentToolCallID != "" {
						eventChan <- ProviderEvent{Type: EventToolUseStop, ToolCall: &message.ToolCall{ID: currentToolCallID}}
						currentToolCallID = ""
					} else {
						eventChan <- ProviderEvent{Type: EventContentStop}
					}
				case anthropic.BetaRawMessageStopEvent:
					content := ""
					for _, block := range acc.Content {
						if text, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
							content += text.Text
						}
					}
					eventChan <- ProviderEvent{
						Type: EventComplete,
						Response: &ProviderResponse{
							Content:      content,
							ToolCalls:    a.toolCallsBeta(acc),
							Usage:        a.usageBeta(acc),
							FinishReason: a.finishReason(string(acc.StopReason)),
						},
					}
				}
			}

			err := stream.Err()
			if err == nil || errors.Is(err, io.EOF) {
				close(eventChan)
				return
			}
			if inputTokens, contextLimit, ok := parseContextOverflowError(err); ok {
				const safetyBuffer = 1000
				const floorOutputTokens = 3000
				available := contextLimit - inputTokens - safetyBuffer
				if available >= floorOutputTokens {
					prepared.MaxTokens = max(int64(floorOutputTokens), available)
					acc = anthropic.BetaMessage{}
					currentToolCallID = ""
					continue
				}
			}
			logging.Error("Anthropic beta stream error", "error", err, "attempt", attempts, "model", a.providerOptions.model.APIModel)
			retry, after, retryErr := a.shouldRetry(attempts, err)
			if retryErr != nil {
				eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
				close(eventChan)
				return
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying (attempt %d/%d)...", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				select {
				case <-ctx.Done():
					if ctx.Err() != nil {
						eventChan <- ProviderEvent{Type: EventError, Error: ctx.Err()}
					}
					close(eventChan)
					return
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			if ctx.Err() != nil {
				eventChan <- ProviderEvent{Type: EventError, Error: ctx.Err()}
			}
			close(eventChan)
			return
		}
	}()
	return eventChan
}
