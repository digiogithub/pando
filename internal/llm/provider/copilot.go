package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/version"
	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
)

type copilotOptions struct {
	reasoningEffort string
	extraHeaders    map[string]string
	bearerToken     string
	baseURL         string
}

type CopilotOption func(*copilotOptions)

type copilotClient struct {
	providerOptions providerClientOptions
	options         copilotOptions
	client          openai.Client
	baseURL         string
	// sourceToken is the GitHub OAuth token the bearer token was derived from.
	// Exchanged Copilot API tokens are short lived (~30 min), so it is kept to
	// re-run the exchange when the current one is about to expire.
	sourceToken   string
	enterpriseURL string
}

type CopilotClient ProviderClient

func (c *copilotClient) isAnthropicModel() bool {
	return models.IsCopilotAnthropicModel(c.providerOptions.model.APIModel)
}

func (c *copilotClient) isResponsesAPIModel() bool {
	return models.CopilotModelUsesResponsesAPI(c.providerOptions.model)
}

// copilotCredentials holds everything needed to talk to the Copilot API, plus
// the source OAuth token so the short-lived bearer can be renewed later.
type copilotCredentials struct {
	sourceToken   string
	bearerToken   string
	baseURL       string
	enterpriseURL string
}

func loadCopilotCredentials(savedToken, configuredToken, configuredBaseURL string) (copilotCredentials, error) {
	token := strings.TrimSpace(savedToken)
	if token == "" {
		token = strings.TrimSpace(configuredToken)
	}
	enterpriseURL := ""

	if token == "" {
		loaded, err := auth.LoadGitHubOAuthToken()
		if err != nil {
			return copilotCredentials{}, err
		}
		token = loaded
		if session, err := auth.LoadCopilotSession(); err == nil && session != nil {
			enterpriseURL = session.EnterpriseURL
		}
	}

	bearer, baseURL := resolveCopilotAccess(token, enterpriseURL, configuredBaseURL)
	return copilotCredentials{
		sourceToken:   token,
		bearerToken:   bearer,
		baseURL:       baseURL,
		enterpriseURL: enterpriseURL,
	}, nil
}

// resolveCopilotAccess swaps the GitHub OAuth token for a short-lived Copilot
// API token. That exchanged token is what unlocks the seat's organization BYOK
// custom models and the per-seat API host; when the exchange fails it degrades
// to the raw token and the default host, i.e. the previous behaviour.
func resolveCopilotAccess(token, enterpriseURL, configuredBaseURL string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return auth.ResolveCopilotAPIAccess(ctx, token, enterpriseURL, configuredBaseURL)
}

func newCopilotOpenAIClient(accessToken, baseURL string, headers map[string]string) openai.Client {
	options := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithAPIKey(accessToken),
		option.WithMiddleware(sseNormalizeMimeMiddleware),
	}
	for key, value := range headers {
		options = append(options, option.WithHeader(key, value))
	}
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		// Inject the debug HTTP transport so every API call is logged to disk.
		options = append(options,
			option.WithHTTPClient(&http.Client{Transport: newDebugRoundTripper(nil)}))
	}
	return openai.NewClient(options...)
}

func (c *copilotClient) requestHeaders(messages []message.Message) map[string]string {
	headers := map[string]string{
		"Editor-Version":         "Pando/" + version.Version,
		"Editor-Plugin-Version":  "Pando/" + version.Version,
		"Copilot-Integration-Id": "vscode-chat",
		"User-Agent":             "Pando/" + version.Version,
		"Openai-Intent":          "conversation-edits",
		"x-initiator":            c.initiator(messages),
	}
	if c.hasVisionInput(messages) {
		headers["Copilot-Vision-Request"] = "true"
	}
	if c.isAnthropicModel() {
		// GitHub Copilot's /v1/messages shim does not support the
		// fine-grained-tool-streaming beta feature. Using only the
		// interleaved-thinking beta matches what the opencode reference
		// implementation sends (see copilot.ts chat.headers hook).
		headers["anthropic-beta"] = "interleaved-thinking-2025-05-14"
	}
	for key, value := range c.options.extraHeaders {
		headers[key] = value
	}
	return headers
}

func (c *copilotClient) requestClient(messages []message.Message) openai.Client {
	c.refreshBearerToken()
	return newCopilotOpenAIClient(c.options.bearerToken, c.baseURL, c.requestHeaders(messages))
}

// refreshBearerToken renews the exchanged Copilot API token when it is close to
// expiring. auth caches the exchange, so this is a no-op lookup for the vast
// majority of calls and never touches the network while the token is valid.
func (c *copilotClient) refreshBearerToken() {
	if c.sourceToken == "" || !auth.IsCopilotAPIToken(c.options.bearerToken) {
		return
	}
	bearer, baseURL := resolveCopilotAccess(c.sourceToken, c.enterpriseURL, c.options.baseURL)
	if strings.TrimSpace(bearer) == "" || bearer == c.options.bearerToken {
		return
	}
	c.options.bearerToken = bearer
	c.baseURL = baseURL
	c.client = newCopilotOpenAIClient(bearer, baseURL, c.requestHeaders(nil))
}

func (c *copilotClient) initiator(messages []message.Message) string {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		switch messages[idx].Role {
		case message.User:
			return "user"
		case message.Assistant, message.Tool:
			return "agent"
		}
	}
	return "user"
}

func (c *copilotClient) hasVisionInput(messages []message.Message) bool {
	for _, msg := range messages {
		if msg.Role == message.User && len(msg.BinaryContent()) > 0 {
			return true
		}
	}
	return false
}

func (c *copilotClient) reloadCredentials() bool {
	creds, err := loadCopilotCredentials("", c.providerOptions.apiKey, c.options.baseURL)
	if err != nil || strings.TrimSpace(creds.bearerToken) == "" {
		return false
	}
	if creds.bearerToken == c.options.bearerToken && creds.baseURL == c.baseURL {
		return false
	}
	c.options.bearerToken = creds.bearerToken
	c.baseURL = creds.baseURL
	c.sourceToken = creds.sourceToken
	c.enterpriseURL = creds.enterpriseURL
	c.client = newCopilotOpenAIClient(creds.bearerToken, creds.baseURL, c.requestHeaders(nil))
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Copilot credentials reloaded", "baseURL", creds.baseURL)
	}
	return true
}

func newCopilotClient(opts providerClientOptions) CopilotClient {
	copilotOpts := copilotOptions{
		reasoningEffort: "medium",
	}
	// Apply copilot-specific options
	for _, o := range opts.copilotOptions {
		o(&copilotOpts)
	}

	creds, err := loadCopilotCredentials(copilotOpts.bearerToken, opts.apiKey, copilotOpts.baseURL)
	if err != nil {
		logging.Error("GitHub Copilot login is required. Run `pando auth copilot login` or provide a compatible GitHub token.", "error", err)
		return &copilotClient{providerOptions: opts, options: copilotOpts, baseURL: auth.CopilotAPIBaseURL("")}
	}

	bearerToken := creds.bearerToken
	baseURL := creds.baseURL
	copilotOpts.bearerToken = bearerToken

	// Verify that the Copilot models API is accessible before creating the client
	err = auth.CheckCopilotModelsAPI(bearerToken, baseURL)
	if err != nil && bearerToken != creds.sourceToken {
		// The exchanged Copilot API token (or the host it advertised) was
		// rejected: fall back to the legacy raw OAuth token against the default
		// host rather than disabling the provider.
		legacyBaseURL := strings.TrimSpace(copilotOpts.baseURL)
		if legacyBaseURL == "" {
			legacyBaseURL = auth.CopilotAPIBaseURL(creds.enterpriseURL)
		}
		if legacyErr := auth.CheckCopilotModelsAPI(creds.sourceToken, legacyBaseURL); legacyErr == nil {
			logging.Warn("Copilot API token exchange rejected; falling back to the raw GitHub token", "error", err)
			bearerToken = creds.sourceToken
			baseURL = legacyBaseURL
			copilotOpts.bearerToken = bearerToken
			err = nil
		}
	}
	if err != nil {
		logging.Error("Copilot models API is not accessible. Disabling Copilot provider.", "error", err)
		// Disable the Copilot provider in configuration
		if cfg := config.Get(); cfg != nil {
			if providerCfg, ok := cfg.Providers[models.ProviderCopilot]; ok {
				providerCfg.Disabled = true
				cfg.Providers[models.ProviderCopilot] = providerCfg
			}
		}
		return &copilotClient{providerOptions: opts, options: copilotOpts, baseURL: baseURL}
	}

	client := newCopilotOpenAIClient(bearerToken, baseURL, map[string]string{
		"Editor-Version":         "Pando/" + version.Version,
		"Editor-Plugin-Version":  "Pando/" + version.Version,
		"Copilot-Integration-Id": "vscode-chat",
		"User-Agent":             "Pando/" + version.Version,
		"Openai-Intent":          "conversation-edits",
		"x-initiator":            "user",
	})
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Copilot client created", "model", opts.model.APIModel, "baseURL", baseURL)
	}
	return &copilotClient{
		providerOptions: opts,
		options:         copilotOpts,
		client:          client,
		baseURL:         baseURL,
		sourceToken:     creds.sourceToken,
		enterpriseURL:   creds.enterpriseURL,
	}
}

func (c *copilotClient) convertMessages(messages []message.Message) (copilotMessages []openai.ChatCompletionMessageParamUnion) {
	// Add system message first
	copilotMessages = append(copilotMessages, openai.SystemMessage(c.providerOptions.systemMessage))

	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			var content []openai.ChatCompletionContentPartUnionParam
			textBlock := openai.ChatCompletionContentPartTextParam{Text: msg.Content().String()}
			content = append(content, openai.ChatCompletionContentPartUnionParam{OfText: &textBlock})

			for _, binaryContent := range msg.BinaryContent() {
				imageURL := openai.ChatCompletionContentPartImageImageURLParam{URL: binaryContent.String(models.ProviderCopilot)}
				imageBlock := openai.ChatCompletionContentPartImageParam{ImageURL: imageURL}
				content = append(content, openai.ChatCompletionContentPartUnionParam{OfImageURL: &imageBlock})
			}

			copilotMessages = append(copilotMessages, openai.UserMessage(content))

		case message.Assistant:
			assistantMsg := openai.ChatCompletionAssistantMessageParam{
				Role: "assistant",
			}

			if msg.Content().String() != "" {
				assistantMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(msg.Content().String()),
				}
			}

			// Pass reasoning_content back for providers that require it (e.g. SiliconFlow via OpenRouter).
			// Without this, multi-turn conversations with thinking models return a 400 error.
			if thinking := msg.ReasoningContent().Thinking; thinking != "" {
				assistantMsg.WithExtraFields(map[string]any{
					"reasoning_content": thinking,
				})
			}

			if len(msg.ToolCalls()) > 0 {
				assistantMsg.ToolCalls = make([]openai.ChatCompletionMessageToolCallParam, len(msg.ToolCalls()))
				for i, call := range msg.ToolCalls() {
					assistantMsg.ToolCalls[i] = openai.ChatCompletionMessageToolCallParam{
						ID:   call.ID,
						Type: "function",
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      call.Name,
							Arguments: call.Input,
						},
					}
				}
			}

			copilotMessages = append(copilotMessages, openai.ChatCompletionMessageParamUnion{
				OfAssistant: &assistantMsg,
			})

		case message.Tool:
			var toolImages []openai.ChatCompletionContentPartUnionParam
			for _, result := range msg.ToolResults() {
				copilotMessages = append(copilotMessages,
					openai.ToolMessage(result.Content, result.ToolCallID),
				)
				if c.providerOptions.model.SupportsAttachments {
					for _, img := range result.Images {
						imageURL := openai.ChatCompletionContentPartImageImageURLParam{URL: img.String(models.ProviderCopilot)}
						toolImages = append(toolImages, openai.ChatCompletionContentPartUnionParam{
							OfImageURL: &openai.ChatCompletionContentPartImageParam{ImageURL: imageURL},
						})
					}
				}
			}
			if len(toolImages) > 0 {
				content := []openai.ChatCompletionContentPartUnionParam{
					{OfText: &openai.ChatCompletionContentPartTextParam{Text: "Image(s) returned by the previous tool call:"}},
				}
				content = append(content, toolImages...)
				copilotMessages = append(copilotMessages, openai.UserMessage(content))
			}
		}
	}

	return
}

func (c *copilotClient) convertTools(tools []toolsPkg.BaseTool) []openai.ChatCompletionToolParam {
	copilotTools := make([]openai.ChatCompletionToolParam, len(tools))

	for i, tool := range tools {
		info := tool.Info()
		required := info.Required
		if required == nil {
			required = []string{}
		}
		copilotTools[i] = openai.ChatCompletionToolParam{
			Function: openai.FunctionDefinitionParam{
				Name:        info.Name,
				Description: openai.String(info.Description),
				Parameters: openai.FunctionParameters{
					"type":       "object",
					"properties": info.Parameters,
					"required":   required,
				},
			},
		}
	}

	return copilotTools
}

func (c *copilotClient) finishReason(reason string) message.FinishReason {
	switch reason {
	case "stop":
		return message.FinishReasonEndTurn
	case "length":
		return message.FinishReasonMaxTokens
	case "tool_calls":
		return message.FinishReasonToolUse
	default:
		return message.FinishReasonUnknown
	}
}

func (c *copilotClient) preparedParams(messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolParam) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(c.providerOptions.model.APIModel),
		Messages: messages,
		Tools:    tools,
	}

	if c.providerOptions.model.CanReason {
		params.MaxCompletionTokens = openai.Int(c.providerOptions.maxTokens)
		if c.providerOptions.model.SupportsReasoningEffort {
			params.ReasoningEffort = shared.ReasoningEffort(c.options.reasoningEffort)
		}
	} else {
		params.MaxTokens = openai.Int(c.providerOptions.maxTokens)
	}

	return params
}

func (c *copilotClient) send(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) (response *ProviderResponse, err error) {
	if c.isResponsesAPIModel() {
		return c.sendWithResponsesAPI(ctx, messages, tools)
	}
	params := c.preparedParams(c.convertMessages(messages), c.convertTools(tools))
	cfg := config.Get()
	var sessionId string
	requestSeqId := (len(messages) + 1) / 2
	if cfg.Debug {
		// jsonData, _ := json.Marshal(params)
		// logging.Debug("Prepared messages", "messages", string(jsonData))
		if sid, ok := ctx.Value(toolsPkg.SessionIDContextKey).(string); ok {
			sessionId = sid
		}
		jsonData, _ := json.Marshal(params)
		if sessionId != "" {
			filepath := logging.WriteRequestMessageJson(sessionId, requestSeqId, params)
			logging.Debug("Prepared messages", "filepath", filepath)
		} else {
			logging.Debug("Prepared messages", "messages", string(jsonData))
		}
	}

	attempts := 0
	for {
		attempts++
		client := c.requestClient(messages)
		copilotResponse, err := client.Chat.Completions.New(
			ctx,
			params,
		)

		// If there is an error we are going to see if we can retry the call
		if err != nil {
			retry, after, retryErr := c.shouldRetry(attempts, err)
			if retryErr != nil {
				return nil, retryErr
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				notify.Warn(notify.SourceLLMProvider, fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), 10*time.Second)
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
		if copilotResponse.Choices[0].Message.Content != "" {
			content = copilotResponse.Choices[0].Message.Content
		}

		toolCalls := c.toolCalls(*copilotResponse)
		finishReason := c.finishReason(string(copilotResponse.Choices[0].FinishReason))

		if len(toolCalls) > 0 {
			finishReason = message.FinishReasonToolUse
		}

		if cfg.Debug {
			logging.Debug("Copilot send completed", "model", c.providerOptions.model.APIModel, "content_length", len(content))
		}
		return &ProviderResponse{
			Content:      content,
			ToolCalls:    toolCalls,
			Usage:        c.usage(*copilotResponse),
			FinishReason: finishReason,
		}, nil
	}
}

func (c *copilotClient) stream(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	if c.isResponsesAPIModel() {
		return c.streamWithResponsesAPI(ctx, messages, tools)
	}
	params := c.preparedParams(c.convertMessages(messages), c.convertTools(tools))
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}

	cfg := config.Get()
	var sessionId string
	requestSeqId := (len(messages) + 1) / 2
	if cfg.Debug {
		if sid, ok := ctx.Value(toolsPkg.SessionIDContextKey).(string); ok {
			sessionId = sid
		}
		jsonData, _ := json.Marshal(params)
		if sessionId != "" {
			filepath := logging.WriteRequestMessageJson(sessionId, requestSeqId, params)
			logging.Debug("Prepared messages", "filepath", filepath)
		} else {
			logging.Debug("Prepared messages", "messages", string(jsonData))
		}

	}

	attempts := 0
	eventChan := make(chan ProviderEvent)

	go func() {
		for {
			attempts++
			if cfg.Debug {
				logging.Debug("Copilot stream started", "model", c.providerOptions.model.APIModel, "attempt", attempts, "isAnthropicModel", c.isAnthropicModel())
			}
			client := c.requestClient(messages)
			copilotStream := client.Chat.Completions.NewStreaming(
				ctx,
				params,
			)

			acc := openai.ChatCompletionAccumulator{}
			currentContent := ""
			toolCalls := make([]message.ToolCall, 0)

			var currentToolCallId string
			var currentToolCall openai.ChatCompletionMessageToolCall
			var msgToolCalls []openai.ChatCompletionMessageToolCall
			for copilotStream.Next() {
				chunk := copilotStream.Current()
				acc.AddChunk(chunk)

				if cfg.Debug {
					logging.AppendToStreamSessionLogJson(sessionId, requestSeqId, chunk)
				}

				for _, choice := range chunk.Choices {
					if choice.Delta.Content != "" {
						eventChan <- ProviderEvent{
							Type:    EventContentDelta,
							Content: choice.Delta.Content,
						}
						currentContent += choice.Delta.Content
					}
					// Capture reasoning_content from providers like SiliconFlow via OpenRouter.
					if rc, ok := choice.Delta.JSON.ExtraFields["reasoning_content"]; ok {
						if raw := rc.Raw(); raw != "" && raw != "null" {
							var rcStr string
							if err := json.Unmarshal([]byte(raw), &rcStr); err == nil && rcStr != "" {
								eventChan <- ProviderEvent{
									Type:     EventThinkingDelta,
									Thinking: rcStr,
								}
							}
						}
					}
				}

				if c.isAnthropicModel() {
					// Monkeypatch adapter for Sonnet-4 multi-tool use
					for _, choice := range chunk.Choices {
						if choice.Delta.ToolCalls != nil && len(choice.Delta.ToolCalls) > 0 {
							toolCall := choice.Delta.ToolCalls[0]
							// Detect tool use start
							if currentToolCallId == "" {
								if toolCall.ID != "" {
									currentToolCallId = toolCall.ID
									currentToolCall = openai.ChatCompletionMessageToolCall{
										ID:   toolCall.ID,
										Type: "function",
										Function: openai.ChatCompletionMessageToolCallFunction{
											Name:      toolCall.Function.Name,
											Arguments: toolCall.Function.Arguments,
										},
									}
								}
							} else {
								// Delta tool use
								if toolCall.ID == "" {
									currentToolCall.Function.Arguments += toolCall.Function.Arguments
								} else {
									// Detect new tool use
									if toolCall.ID != currentToolCallId {
										msgToolCalls = append(msgToolCalls, currentToolCall)
										currentToolCallId = toolCall.ID
										currentToolCall = openai.ChatCompletionMessageToolCall{
											ID:   toolCall.ID,
											Type: "function",
											Function: openai.ChatCompletionMessageToolCallFunction{
												Name:      toolCall.Function.Name,
												Arguments: toolCall.Function.Arguments,
											},
										}
									}
								}
							}
						}
						if choice.FinishReason == "tool_calls" {
							msgToolCalls = append(msgToolCalls, currentToolCall)
							acc.ChatCompletion.Choices[0].Message.ToolCalls = msgToolCalls
						}
					}
				}
			}

			err := copilotStream.Err()
			if err == nil || errors.Is(err, io.EOF) {
				if cfg.Debug {
					respFilepath := logging.WriteChatResponseJson(sessionId, requestSeqId, acc.ChatCompletion)
					logging.Debug("Chat completion response", "filepath", respFilepath)
				}
				// Stream completed successfully
				finishReason := c.finishReason(string(acc.ChatCompletion.Choices[0].FinishReason))
				if len(acc.ChatCompletion.Choices[0].Message.ToolCalls) > 0 {
					toolCalls = append(toolCalls, c.toolCalls(acc.ChatCompletion)...)
				}
				if len(toolCalls) > 0 {
					finishReason = message.FinishReasonToolUse
				}

				if cfg.Debug {
					logging.Debug("Copilot stream completed", "model", c.providerOptions.model.APIModel, "finishReason", finishReason, "toolCallCount", len(toolCalls))
				}
				eventChan <- ProviderEvent{
					Type: EventComplete,
					Response: &ProviderResponse{
						Content:      currentContent,
						ToolCalls:    toolCalls,
						Usage:        c.usage(acc.ChatCompletion),
						FinishReason: finishReason,
					},
				}
				close(eventChan)
				return
			}

			// If there is an error we are going to see if we can retry the call
			retry, after, retryErr := c.shouldRetry(attempts, err)
			if retryErr != nil {
				eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
				close(eventChan)
				return
			}
			if attempts > maxRetries {
				logging.Warn("Maximum retry attempts reached for rate limit", "attempts", attempts, "max_retries", maxRetries)
				retry = false
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d (paused for %d ms)", attempts, maxRetries, after), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				notify.Warn(notify.SourceLLMProvider, fmt.Sprintf("Retrying due to rate limit... attempt %d of %d (paused for %d ms)", attempts, maxRetries, after), 10*time.Second)
				select {
				case <-ctx.Done():
					// context cancelled
					if ctx.Err() == nil {
						eventChan <- ProviderEvent{Type: EventError, Error: ctx.Err()}
					}
					close(eventChan)
					return
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			// Max retries exceeded — retryErr may be nil here when the error is a
			// rate-limit (429/500) that exhausted attempts; wrap it to avoid sending
			// a nil error downstream which callers cannot distinguish from success.
			if retryErr == nil {
				retryErr = fmt.Errorf("maximum retry attempts (%d) exceeded for rate limit", maxRetries)
			}
			notify.Error(notify.SourceLLMProvider, retryErr.Error())
			eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
			close(eventChan)
			return
		}
	}()

	return eventChan
}

// convertMessagesToResponsesInput converts the internal message format to the
// OpenAI Responses API input format (used by GPT-5+ models).
func (c *copilotClient) convertMessagesToResponsesInput(msgs []message.Message) responses.ResponseInputParam {
	var input responses.ResponseInputParam

	for _, msg := range msgs {
		switch msg.Role {
		case message.User:
			var contentList responses.ResponseInputMessageContentListParam
			textPart := responses.ResponseInputContentParamOfInputText(msg.Content().String())
			contentList = append(contentList, textPart)
			for _, bin := range msg.BinaryContent() {
				imgPart := responses.ResponseInputContentUnionParam{
					OfInputImage: &responses.ResponseInputImageParam{
						ImageURL: openai.String(bin.String(models.ProviderCopilot)),
						Detail:   responses.ResponseInputImageDetailAuto,
					},
				}
				contentList = append(contentList, imgPart)
			}
			item := responses.ResponseInputItemParamOfMessage(contentList, responses.EasyInputMessageRoleUser)
			input = append(input, item)

		case message.Assistant:
			if len(msg.ToolCalls()) > 0 {
				// Each tool call becomes a separate function_call input item
				for _, tc := range msg.ToolCalls() {
					callID := tc.ID
					if callID == "" {
						callID = "call_" + uuid.New().String()
					}
					item := responses.ResponseInputItemParamOfFunctionCall(tc.Input, callID, tc.Name)
					input = append(input, item)
				}
			} else if msg.Content().String() != "" {
				textContent := responses.ResponseOutputMessageContentUnionParam{
					OfOutputText: &responses.ResponseOutputTextParam{
						Text: msg.Content().String(),
					},
				}
				item := responses.ResponseInputItemParamOfOutputMessage(
					[]responses.ResponseOutputMessageContentUnionParam{textContent},
					"msg_"+uuid.New().String(),
					responses.ResponseOutputMessageStatusCompleted,
				)
				input = append(input, item)
			}

		case message.Tool:
			for _, result := range msg.ToolResults() {
				callID := result.ToolCallID
				if callID == "" {
					callID = "call_" + uuid.New().String()
				}
				item := responses.ResponseInputItemParamOfFunctionCallOutput(callID, result.Content)
				input = append(input, item)

				// Function call outputs cannot carry images; inject them as a
				// separate user message so vision models can see them.
				if c.providerOptions.model.SupportsAttachments && len(result.Images) > 0 {
					var contentList responses.ResponseInputMessageContentListParam
					contentList = append(contentList, responses.ResponseInputContentParamOfInputText("Image(s) returned by the previous tool call:"))
					for _, img := range result.Images {
						contentList = append(contentList, responses.ResponseInputContentUnionParam{
							OfInputImage: &responses.ResponseInputImageParam{
								ImageURL: openai.String(img.String(models.ProviderCopilot)),
								Detail:   responses.ResponseInputImageDetailAuto,
							},
						})
					}
					input = append(input, responses.ResponseInputItemParamOfMessage(contentList, responses.EasyInputMessageRoleUser))
				}
			}
		}
	}

	return input
}

func (c *copilotClient) convertToolsToResponses(tools []toolsPkg.BaseTool) []responses.ToolUnionParam {
	result := make([]responses.ToolUnionParam, len(tools))
	for i, tool := range tools {
		info := tool.Info()
		required := info.Required
		if required == nil {
			required = []string{}
		}
		params := map[string]interface{}{
			"type":       "object",
			"properties": info.Parameters,
			"required":   required,
		}
		result[i] = responses.ToolParamOfFunction(info.Name, params, false)
		if info.Description != "" {
			result[i].OfFunction.Description = openai.String(info.Description)
		}
	}
	return result
}

func (c *copilotClient) responsesFinishReason(status string) message.FinishReason {
	switch status {
	case "completed":
		return message.FinishReasonEndTurn
	case "max_output_tokens":
		return message.FinishReasonMaxTokens
	default:
		return message.FinishReasonUnknown
	}
}

func (c *copilotClient) sendWithResponsesAPI(ctx context.Context, msgs []message.Message, tools []toolsPkg.BaseTool) (*ProviderResponse, error) {
	input := c.convertMessagesToResponsesInput(msgs)
	respTools := c.convertToolsToResponses(tools)

	params := responses.ResponseNewParams{
		Model:           shared.ResponsesModel(c.providerOptions.model.APIModel),
		Input:           responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Instructions:    openai.String(c.providerOptions.systemMessage),
		MaxOutputTokens: openai.Int(c.providerOptions.maxTokens),
	}
	if len(respTools) > 0 {
		params.Tools = respTools
	}

	cfg := config.Get()
	attempts := 0
	for {
		attempts++
		client := c.requestClient(msgs)
		resp, err := client.Responses.New(ctx, params)
		if err != nil {
			retry, after, retryErr := c.shouldRetry(attempts, err)
			if retryErr != nil {
				return nil, retryErr
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				notify.Warn(notify.SourceLLMProvider, fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), 10*time.Second)
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
		var toolCalls []message.ToolCall

		for _, item := range resp.Output {
			switch item.Type {
			case "message":
				msg := item.AsMessage()
				for _, part := range msg.Content {
					if part.Type == "output_text" {
						content += part.AsOutputText().Text
					}
				}
			case "function_call":
				fc := item.AsFunctionCall()
				toolCalls = append(toolCalls, message.ToolCall{
					ID:       fc.CallID,
					Name:     fc.Name,
					Input:    fc.Arguments,
					Type:     "function",
					Finished: true,
				})
			}
		}

		finishReason := c.responsesFinishReason(string(resp.Status))
		if len(toolCalls) > 0 {
			finishReason = message.FinishReasonToolUse
		}

		if cfg.Debug {
			logging.Debug("Copilot Responses API send completed", "model", c.providerOptions.model.APIModel, "content_length", len(content))
		}

		return &ProviderResponse{
			Content:   content,
			ToolCalls: toolCalls,
			Usage: TokenUsage{
				InputTokens:     resp.Usage.InputTokens - resp.Usage.InputTokensDetails.CachedTokens,
				OutputTokens:    resp.Usage.OutputTokens,
				CacheReadTokens: resp.Usage.InputTokensDetails.CachedTokens,
				ReasoningTokens: resp.Usage.OutputTokensDetails.ReasoningTokens,
			},
			FinishReason: finishReason,
		}, nil
	}
}

func (c *copilotClient) streamWithResponsesAPI(ctx context.Context, msgs []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	input := c.convertMessagesToResponsesInput(msgs)
	respTools := c.convertToolsToResponses(tools)

	params := responses.ResponseNewParams{
		Model:           shared.ResponsesModel(c.providerOptions.model.APIModel),
		Input:           responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Instructions:    openai.String(c.providerOptions.systemMessage),
		MaxOutputTokens: openai.Int(c.providerOptions.maxTokens),
	}
	if len(respTools) > 0 {
		params.Tools = respTools
	}

	cfg := config.Get()
	attempts := 0
	eventChan := make(chan ProviderEvent)

	go func() {
		for {
			attempts++
			if cfg.Debug {
				logging.Debug("Copilot Responses API stream started", "model", c.providerOptions.model.APIModel, "attempt", attempts)
			}
			client := c.requestClient(msgs)
			stream := client.Responses.NewStreaming(ctx, params)

			currentContent := ""
			var completedResp responses.Response

			for stream.Next() {
				event := stream.Current()

				switch event.Type {
				case "response.output_text.delta":
					delta := event.AsResponseOutputTextDelta()
					eventChan <- ProviderEvent{Type: EventContentDelta, Content: delta.Delta}
					currentContent += delta.Delta

				case "response.completed":
					completedResp = event.AsResponseCompleted().Response
				}
			}

			err := stream.Err()
			if err == nil || errors.Is(err, io.EOF) {
				// Extract tool calls from the complete response output
				var toolCalls []message.ToolCall
				for _, item := range completedResp.Output {
					if item.Type == "function_call" {
						toolCalls = append(toolCalls, message.ToolCall{
							ID:       item.CallID,
							Name:     item.Name,
							Input:    item.Arguments,
							Type:     "function",
							Finished: true,
						})
					}
				}

				finishReason := c.responsesFinishReason(string(completedResp.Status))
				if len(toolCalls) > 0 {
					finishReason = message.FinishReasonToolUse
				}
				usage := TokenUsage{
					InputTokens:     completedResp.Usage.InputTokens - completedResp.Usage.InputTokensDetails.CachedTokens,
					OutputTokens:    completedResp.Usage.OutputTokens,
					CacheReadTokens: completedResp.Usage.InputTokensDetails.CachedTokens,
					ReasoningTokens: completedResp.Usage.OutputTokensDetails.ReasoningTokens,
				}
				if cfg.Debug {
					logging.Debug("Copilot Responses API stream completed", "model", c.providerOptions.model.APIModel, "finishReason", finishReason, "toolCallCount", len(toolCalls))
				}
				eventChan <- ProviderEvent{
					Type: EventComplete,
					Response: &ProviderResponse{
						Content:      currentContent,
						ToolCalls:    toolCalls,
						Usage:        usage,
						FinishReason: finishReason,
					},
				}
				close(eventChan)
				return
			}

			retry, after, retryErr := c.shouldRetry(attempts, err)
			if retryErr != nil {
				eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
				close(eventChan)
				return
			}
			if attempts > maxRetries {
				retry = false
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d (paused for %d ms)", attempts, maxRetries, after), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
				notify.Warn(notify.SourceLLMProvider, fmt.Sprintf("Retrying due to rate limit... attempt %d of %d (paused for %d ms)", attempts, maxRetries, after), 10*time.Second)
				select {
				case <-ctx.Done():
					if ctx.Err() == nil {
						eventChan <- ProviderEvent{Type: EventError, Error: ctx.Err()}
					}
					close(eventChan)
					return
				case <-time.After(time.Duration(after) * time.Millisecond):
					continue
				}
			}
			// Max retries exceeded — retryErr may be nil here when the error is a
			// rate-limit (429/500) that exhausted attempts; wrap it to avoid sending
			// a nil error downstream which callers cannot distinguish from success.
			if retryErr == nil {
				retryErr = fmt.Errorf("maximum retry attempts (%d) exceeded for rate limit", maxRetries)
			}
			notify.Error(notify.SourceLLMProvider, retryErr.Error())
			eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
			close(eventChan)
			return
		}
	}()

	return eventChan
}

func (c *copilotClient) shouldRetry(attempts int, err error) (bool, int64, error) {
	var apierr *openai.Error
	if !errors.As(err, &apierr) {
		return false, 0, err
	}

	// Check for token expiration (401 Unauthorized)
	if apierr.StatusCode == 401 {
		if c.reloadCredentials() {
			logging.Info("Reloaded GitHub Copilot credentials from the latest login state")
			return true, 1000, nil
		}
		return false, 0, fmt.Errorf("authentication failed: %w. Run `pando auth copilot login`", err)
	}
	logging.Debug("Copilot API Error", "status", apierr.StatusCode, "headers", apierr.Response.Header, "body", apierr.RawJSON())

	if apierr.StatusCode != 429 && apierr.StatusCode != 500 {
		return false, 0, err
	}

	if apierr.StatusCode == 500 {
		logging.Warn("Copilot API returned 500 error, retrying", "error", err)
	}

	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Copilot retry evaluation", "attempts", attempts, "statusCode", apierr.StatusCode)
	}

	if attempts > maxRetries {
		return false, 0, fmt.Errorf("maximum retry attempts reached for rate limit: %d retries", maxRetries)
	}

	retryMs := 0
	retryAfterValues := apierr.Response.Header.Values("Retry-After")

	backoffMs := 2000 * (1 << (attempts - 1))
	jitterMs := int(float64(backoffMs) * 0.2)
	retryMs = backoffMs + jitterMs
	if len(retryAfterValues) > 0 {
		if _, err := fmt.Sscanf(retryAfterValues[0], "%d", &retryMs); err == nil {
			retryMs = retryMs * 1000
		}
	}
	return true, int64(retryMs), nil
}

func (c *copilotClient) toolCalls(completion openai.ChatCompletion) []message.ToolCall {
	var toolCalls []message.ToolCall

	if len(completion.Choices) > 0 && len(completion.Choices[0].Message.ToolCalls) > 0 {
		for _, call := range completion.Choices[0].Message.ToolCalls {
			toolCall := message.ToolCall{
				ID:       call.ID,
				Name:     call.Function.Name,
				Input:    call.Function.Arguments,
				Type:     "function",
				Finished: true,
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	return toolCalls
}

func (c *copilotClient) usage(completion openai.ChatCompletion) TokenUsage {
	cachedTokens := completion.Usage.PromptTokensDetails.CachedTokens
	inputTokens := completion.Usage.PromptTokens - cachedTokens

	return TokenUsage{
		InputTokens:         inputTokens,
		OutputTokens:        completion.Usage.CompletionTokens,
		CacheCreationTokens: 0, // GitHub Copilot doesn't provide this directly
		CacheReadTokens:     cachedTokens,
		ReasoningTokens:     completion.Usage.CompletionTokensDetails.ReasoningTokens,
	}
}

func WithCopilotReasoningEffort(effort string) CopilotOption {
	return func(options *copilotOptions) {
		options.reasoningEffort = clampReasoningEffort(effort)
	}
}

func WithCopilotExtraHeaders(headers map[string]string) CopilotOption {
	return func(options *copilotOptions) {
		options.extraHeaders = headers
	}
}

func WithCopilotBearerToken(bearerToken string) CopilotOption {
	return func(options *copilotOptions) {
		options.bearerToken = bearerToken
	}
}

func WithCopilotBaseURL(baseURL string) CopilotOption {
	return func(options *copilotOptions) {
		options.baseURL = strings.TrimSpace(baseURL)
	}
}
