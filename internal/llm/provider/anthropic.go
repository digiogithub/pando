package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	toolsPkg "github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

type anthropicOptions struct {
	useBedrock   bool
	disableCache bool
	thinkingMode config.ThinkingMode
	oauthToken   string // Bearer access token (if using OAuth instead of API key)
}

type AnthropicOption func(*anthropicOptions)

type anthropicClient struct {
	providerOptions providerClientOptions
	options         anthropicOptions
	client          anthropic.Client
}

type AnthropicClient ProviderClient

func newAnthropicClient(opts providerClientOptions) AnthropicClient {
	anthropicOpts := anthropicOptions{}
	for _, o := range opts.anthropicOptions {
		o(&anthropicOpts)
	}

	anthropicClientOptions := []option.RequestOption{}
	if opts.apiKey != "" {
		anthropicClientOptions = append(anthropicClientOptions, option.WithAPIKey(opts.apiKey))
		// Enable Anthropic beta features (matches claude-code-cli behaviour).
		anthropicClientOptions = append(anthropicClientOptions, option.WithHeader("anthropic-beta",
			"claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"))
	} else if anthropicOpts.oauthToken != "" {
		anthropicClientOptions = append(anthropicClientOptions, option.WithAuthToken(anthropicOpts.oauthToken))
		// Combine OAuth beta with Anthropic beta feature identifiers.
		anthropicClientOptions = append(anthropicClientOptions, option.WithHeader("anthropic-beta",
			auth.ClaudeOAuthBetaHeader+",claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"))
	}
	if anthropicOpts.useBedrock {
		anthropicClientOptions = append(anthropicClientOptions, bedrock.WithLoadDefaultConfig(context.Background()))
	}

	client := anthropic.NewClient(anthropicClientOptions...)
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Creating Anthropic client", "model", opts.model.APIModel)
	}
	return &anthropicClient{
		providerOptions: opts,
		options:         anthropicOpts,
		client:          client,
	}
}

func (a *anthropicClient) convertMessages(messages []message.Message) (anthropicMessages []anthropic.MessageParam) {
	for i, msg := range messages {
		cache := false
		if i > len(messages)-3 {
			cache = true
		}
		switch msg.Role {
		case message.User:
			content := anthropic.NewTextBlock(msg.Content().String())
			if cache && !a.options.disableCache {
				content.OfText.CacheControl = anthropic.CacheControlEphemeralParam{
					Type: "ephemeral",
				}
			}
			var contentBlocks []anthropic.ContentBlockParamUnion
			contentBlocks = append(contentBlocks, content)
			for _, binaryContent := range msg.BinaryContent() {
				base64Image := binaryContent.String(models.ProviderAnthropic)
				imageBlock := anthropic.NewImageBlockBase64(binaryContent.MIMEType, base64Image)
				contentBlocks = append(contentBlocks, imageBlock)
			}
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(contentBlocks...))

		case message.Assistant:
			blocks := []anthropic.ContentBlockParamUnion{}
			if msg.Content().String() != "" {
				content := anthropic.NewTextBlock(msg.Content().String())
				if cache && !a.options.disableCache {
					content.OfText.CacheControl = anthropic.CacheControlEphemeralParam{
						Type: "ephemeral",
					}
				}
				blocks = append(blocks, content)
			}

			for _, toolCall := range msg.ToolCalls() {
				var inputMap map[string]any
				err := json.Unmarshal([]byte(toolCall.Input), &inputMap)
				if err != nil {
					continue
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(toolCall.ID, inputMap, toolCall.Name))
			}

			if len(blocks) == 0 {
				logging.Warn("There is a message without content, investigate, this should not happen")
				continue
			}
			anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(blocks...))

		case message.Tool:
			results := make([]anthropic.ContentBlockParamUnion, len(msg.ToolResults()))
			for i, toolResult := range msg.ToolResults() {
				results[i] = anthropic.NewToolResultBlock(toolResult.ToolCallID, toolResult.Content, toolResult.IsError)
			}
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(results...))
		}
	}
	return
}

func (a *anthropicClient) convertTools(tools []toolsPkg.BaseTool) []anthropic.ToolUnionParam {
	anthropicTools := make([]anthropic.ToolUnionParam, len(tools))

	for i, tool := range tools {
		info := tool.Info()
		toolParam := anthropic.ToolParam{
			Name:        info.Name,
			Description: anthropic.String(info.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: info.Parameters,
				// TODO: figure out how we can tell claude the required fields?
			},
		}

		if i == len(tools)-1 && !a.options.disableCache {
			toolParam.CacheControl = anthropic.CacheControlEphemeralParam{
				Type: "ephemeral",
			}
		}

		anthropicTools[i] = anthropic.ToolUnionParam{OfTool: &toolParam}
	}

	return anthropicTools
}

func (a *anthropicClient) finishReason(reason string) message.FinishReason {
	switch reason {
	case "end_turn":
		return message.FinishReasonEndTurn
	case "max_tokens":
		return message.FinishReasonMaxTokens
	case "tool_use":
		return message.FinishReasonToolUse
	case "stop_sequence":
		return message.FinishReasonEndTurn
	default:
		return message.FinishReasonUnknown
	}
}

func (a *anthropicClient) preparedMessages(messages []anthropic.MessageParam, tools []anthropic.ToolUnionParam) anthropic.MessageNewParams {
	var thinkingParam anthropic.ThinkingConfigParamUnion
	temperature := anthropic.Float(0)
	if budgetTokens := thinkingBudgetTokens(a.options.thinkingMode, a.providerOptions.maxTokens); budgetTokens > 0 {
		if !isAdaptiveThinkingModel(a.providerOptions.model.APIModel) {
			thinkingParam = anthropic.ThinkingConfigParamOfEnabled(budgetTokens)
		}
		temperature = anthropic.Float(1)
	}

	return anthropic.MessageNewParams{
		Model:       anthropic.Model(a.providerOptions.model.APIModel),
		MaxTokens:   a.providerOptions.maxTokens,
		Temperature: temperature,
		Messages:    messages,
		Tools:       tools,
		Thinking:    thinkingParam,
		System: []anthropic.TextBlockParam{
			{
				Text: a.providerOptions.systemMessage,
				CacheControl: anthropic.CacheControlEphemeralParam{
					Type: "ephemeral",
				},
			},
		},
	}
}

func (a *anthropicClient) send(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) (resposne *ProviderResponse, err error) {
	preparedMessages := a.preparedMessages(a.convertMessages(messages), a.convertTools(tools))
	requestOptions := a.thinkingRequestOptions()
	cfg := config.Get()
	if cfg.Debug {
		jsonData, _ := json.Marshal(preparedMessages)
		logging.Debug("Prepared messages", "messages", string(jsonData))
	}

	attempts := 0
	for {
		attempts++
		anthropicResponse, err := a.client.Messages.New(
			ctx,
			preparedMessages,
			requestOptions...,
		)
		// If there is an error we are going to see if we can retry the call
		if err != nil {
			logging.Error("Error in Anthropic API call", "error", err)

			// Context-window overflow: adjust max_tokens and retry immediately.
			if inputTokens, contextLimit, ok := parseContextOverflowError(err); ok {
				const safetyBuffer = 1000
				const floorOutputTokens = 3000
				available := contextLimit - inputTokens - safetyBuffer
				if available >= floorOutputTokens {
					adjusted := max(int64(floorOutputTokens), available)
					logging.Warn("Context window overflow — reducing max_tokens for retry",
						"input_tokens", inputTokens, "context_limit", contextLimit, "new_max_tokens", adjusted)
					preparedMessages.MaxTokens = adjusted
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
		for _, block := range anthropicResponse.Content {
			if text, ok := block.AsAny().(anthropic.TextBlock); ok {
				content += text.Text
			}
		}

		if cfg := config.Get(); cfg != nil && cfg.Debug {
			logging.Debug("Anthropic send completed", "model", a.providerOptions.model.APIModel, "content_length", len(content))
		}
		return &ProviderResponse{
			Content:   content,
			ToolCalls: a.toolCalls(*anthropicResponse),
			Usage:     a.usage(*anthropicResponse),
		}, nil
	}
}

func (a *anthropicClient) stream(ctx context.Context, messages []message.Message, tools []toolsPkg.BaseTool) <-chan ProviderEvent {
	preparedMessages := a.preparedMessages(a.convertMessages(messages), a.convertTools(tools))
	requestOptions := a.thinkingRequestOptions()
	cfg := config.Get()

	var sessionId string
	requestSeqId := (len(messages) + 1) / 2
	if cfg.Debug {
		if sid, ok := ctx.Value(toolsPkg.SessionIDContextKey).(string); ok {
			sessionId = sid
		}
		jsonData, _ := json.Marshal(preparedMessages)
		if sessionId != "" {
			filepath := logging.WriteRequestMessageJson(sessionId, requestSeqId, preparedMessages)
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
			if cfg != nil && cfg.Debug {
				logging.Debug("Anthropic stream started", "model", a.providerOptions.model.APIModel, "attempt", attempts)
			}
			anthropicStream := a.client.Messages.NewStreaming(
				ctx,
				preparedMessages,
				requestOptions...,
			)
			accumulatedMessage := anthropic.Message{}

			currentToolCallID := ""
			for anthropicStream.Next() {
				event := anthropicStream.Current()
				err := accumulatedMessage.Accumulate(event)
				if err != nil {
					logging.Warn("Error accumulating message", "error", err)
					continue
				}

				switch event := event.AsAny().(type) {
				case anthropic.ContentBlockStartEvent:
					if event.ContentBlock.Type == "text" {
						eventChan <- ProviderEvent{Type: EventContentStart}
					} else if event.ContentBlock.Type == "tool_use" {
						currentToolCallID = event.ContentBlock.ID
						eventChan <- ProviderEvent{
							Type: EventToolUseStart,
							ToolCall: &message.ToolCall{
								ID:       event.ContentBlock.ID,
								Name:     event.ContentBlock.Name,
								Finished: false,
							},
						}
					}

				case anthropic.ContentBlockDeltaEvent:
					if event.Delta.Type == "thinking_delta" && event.Delta.Thinking != "" {
						eventChan <- ProviderEvent{
							Type:     EventThinkingDelta,
							Thinking: event.Delta.Thinking,
						}
					} else if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
						eventChan <- ProviderEvent{
							Type:    EventContentDelta,
							Content: event.Delta.Text,
						}
					} else if event.Delta.Type == "input_json_delta" {
						if currentToolCallID != "" {
							eventChan <- ProviderEvent{
								Type: EventToolUseDelta,
								ToolCall: &message.ToolCall{
									ID:       currentToolCallID,
									Finished: false,
									Input:    event.Delta.JSON.PartialJSON.Raw(),
								},
							}
						}
					}
				case anthropic.ContentBlockStopEvent:
					if currentToolCallID != "" {
						eventChan <- ProviderEvent{
							Type: EventToolUseStop,
							ToolCall: &message.ToolCall{
								ID: currentToolCallID,
							},
						}
						currentToolCallID = ""
					} else {
						eventChan <- ProviderEvent{Type: EventContentStop}
					}

				case anthropic.MessageStopEvent:
					content := ""
					for _, block := range accumulatedMessage.Content {
						if text, ok := block.AsAny().(anthropic.TextBlock); ok {
							content += text.Text
						}
					}

					eventChan <- ProviderEvent{
						Type: EventComplete,
						Response: &ProviderResponse{
							Content:      content,
							ToolCalls:    a.toolCalls(accumulatedMessage),
							Usage:        a.usage(accumulatedMessage),
							FinishReason: a.finishReason(string(accumulatedMessage.StopReason)),
						},
					}
				}
			}

			err := anthropicStream.Err()
			if err == nil || errors.Is(err, io.EOF) {
				if cfg != nil && cfg.Debug {
					logging.Debug("Anthropic stream completed successfully", "model", a.providerOptions.model.APIModel)
				}
				close(eventChan)
				return
			}
			// Context-window overflow: adjust max_tokens and restart stream immediately.
			if inputTokens, contextLimit, ok := parseContextOverflowError(err); ok {
				const safetyBuffer = 1000
				const floorOutputTokens = 3000
				available := contextLimit - inputTokens - safetyBuffer
				if available >= floorOutputTokens {
					adjusted := max(int64(floorOutputTokens), available)
					logging.Warn("Context window overflow — reducing max_tokens for retry",
						"input_tokens", inputTokens, "context_limit", contextLimit, "new_max_tokens", adjusted)
					preparedMessages.MaxTokens = adjusted
					accumulatedMessage = anthropic.Message{}
					currentToolCallID = ""
					continue
				}
			}

			// If there is an error we are going to see if we can retry the call
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
					// context cancelled
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

// rateLimitClaimNames maps anthropic-ratelimit-unified-representative-claim header values to human-readable descriptions.
var rateLimitClaimNames = map[string]string{
	"five_hour":        "5-hour session limit",
	"seven_day":        "weekly limit",
	"seven_day_sonnet": "Sonnet weekly limit",
	"seven_day_opus":   "Opus weekly limit",
	"overage":          "extra usage limit",
}

// contextOverflowRe matches the API error: "input length and `max_tokens` exceed context limit: X + Y > Z"
var contextOverflowRe = regexp.MustCompile(`input length and .max_tokens. exceed context limit: (\d+) \+ (\d+) > (\d+)`)

// buildRateLimitError constructs a user-friendly error message from a 429/529 response,
// reading unified rate limit headers to identify the limit type and reset time.
func (a *anthropicClient) buildRateLimitError(attempts int, apierr *anthropic.Error) error {
	claim := ""
	resetsMsg := ""
	if apierr.Response != nil {
		claim = apierr.Response.Header.Get("anthropic-ratelimit-unified-representative-claim")
		if resetStr := apierr.Response.Header.Get("anthropic-ratelimit-unified-reset"); resetStr != "" {
			if ts, parseErr := strconv.ParseInt(resetStr, 10, 64); parseErr == nil {
				resetTime := time.Unix(ts, 0).Local()
				resetsMsg = fmt.Sprintf(", resets at %s", resetTime.Format("15:04 on Jan 2"))
			}
		}
	}

	limitDesc := "rate limit"
	if name, ok := rateLimitClaimNames[claim]; ok {
		limitDesc = name
	}

	msg := fmt.Sprintf("%s reached after %d attempts%s", limitDesc, attempts, resetsMsg)
	if claim == "seven_day_sonnet" || claim == "seven_day_opus" {
		msg += ". Consider switching to a different model (e.g. Claude Haiku)"
	}
	return errors.New(msg)
}

// parseContextOverflowError checks if err is a 400 context-window overflow and returns
// (inputTokens, contextLimit, true) so the caller can compute a safe maxTokens.
func parseContextOverflowError(err error) (inputTokens, contextLimit int64, ok bool) {
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) || apierr.StatusCode != 400 {
		return 0, 0, false
	}
	m := contextOverflowRe.FindStringSubmatch(apierr.Error())
	if len(m) != 4 {
		return 0, 0, false
	}
	in, _ := strconv.ParseInt(m[1], 10, 64)
	lim, _ := strconv.ParseInt(m[3], 10, 64)
	if in == 0 || lim == 0 {
		return 0, 0, false
	}
	return in, lim, true
}

// retryDelay computes exponential backoff with ±25% jitter, capped at maxDelayMs.
// If the response contains a Retry-After header it is honoured (up to the cap).
func retryDelay(attempt int, apierr *anthropic.Error) int64 {
	// Honour Retry-After header when present.
	if apierr != nil && apierr.Response != nil {
		for _, v := range apierr.Response.Header.Values("Retry-After") {
			var secs int
			if _, err := fmt.Sscanf(v, "%d", &secs); err == nil && secs > 0 {
				ms := int64(secs) * 1000
				if ms > maxDelayMs {
					ms = maxDelayMs
				}
				return ms
			}
		}
	}
	base := math.Min(float64(baseDelayMs)*math.Pow(2, float64(attempt-1)), float64(maxDelayMs))
	jitter := base * 0.25 * rand.Float64()
	return int64(base + jitter)
}

// shouldRetry analyses err after an API call and returns (retry, delayMs, surfacedError).
//
// Design principles (aligned with claude-code's withRetry.ts):
//   - Honour x-should-retry header — the API knows best.
//   - Per-model weekly quota 429s (seven_day_sonnet/opus/seven_day) never resolve by retrying;
//     fail immediately with a clear message.
//   - Retry 408 (request timeout), 409 (lock timeout), 529 (overloaded), and 5xx.
//   - Use 500ms base delay × 2^attempt, capped at 32 s, with ±25% jitter.
func (a *anthropicClient) shouldRetry(attempts int, err error) (bool, int64, error) {
	var apierr *anthropic.Error
	if !errors.As(err, &apierr) {
		return false, 0, err
	}

	// The API signals explicitly whether a retry makes sense.
	if apierr.Response != nil {
		if apierr.Response.Header.Get("x-should-retry") == "false" {
			return false, 0, err
		}
	}

	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Anthropic retry evaluation", "attempts", attempts, "statusCode", apierr.StatusCode)
	}

	switch apierr.StatusCode {
	case 408, 409:
		// Request timeout / lock timeout — transient, always retry.
		if attempts <= maxRetries {
			return true, retryDelay(attempts, apierr), nil
		}
		return false, 0, err

	case 429:
		// Per-model weekly quota errors will not resolve by retrying.
		if apierr.Response != nil {
			claim := apierr.Response.Header.Get("anthropic-ratelimit-unified-representative-claim")
			if claim == "seven_day_sonnet" || claim == "seven_day_opus" || claim == "seven_day" {
				return false, 0, a.buildRateLimitError(attempts, apierr)
			}
		}
		// 5-hour session limit or overage — retry with backoff.
		if attempts <= maxRetries {
			return true, retryDelay(attempts, apierr), nil
		}
		return false, 0, a.buildRateLimitError(attempts, apierr)

	case 529:
		// API overloaded — retry.
		if attempts <= maxRetries {
			return true, retryDelay(attempts, apierr), nil
		}
		return false, 0, a.buildRateLimitError(attempts, apierr)
	}

	// Retry on 5xx server errors.
	if apierr.StatusCode >= 500 && attempts <= maxRetries {
		return true, retryDelay(attempts, apierr), nil
	}

	return false, 0, err
}

func (a *anthropicClient) toolCalls(msg anthropic.Message) []message.ToolCall {
	var toolCalls []message.ToolCall

	for _, block := range msg.Content {
		switch variant := block.AsAny().(type) {
		case anthropic.ToolUseBlock:
			toolCall := message.ToolCall{
				ID:       variant.ID,
				Name:     variant.Name,
				Input:    string(variant.Input),
				Type:     string(variant.Type),
				Finished: true,
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	return toolCalls
}

func (a *anthropicClient) usage(msg anthropic.Message) TokenUsage {
	return TokenUsage{
		InputTokens:         msg.Usage.InputTokens,
		OutputTokens:        msg.Usage.OutputTokens,
		CacheCreationTokens: msg.Usage.CacheCreationInputTokens,
		CacheReadTokens:     msg.Usage.CacheReadInputTokens,
	}
}

func WithAnthropicBedrock(useBedrock bool) AnthropicOption {
	return func(options *anthropicOptions) {
		options.useBedrock = useBedrock
	}
}

func WithAnthropicDisableCache() AnthropicOption {
	return func(options *anthropicOptions) {
		options.disableCache = true
	}
}

// thinkingBudgetTokens returns the number of budget tokens for the given thinking mode.
// Returns 0 when thinking is disabled or mode is empty.
func thinkingBudgetTokens(mode config.ThinkingMode, maxTokens int64) int64 {
	const minBudget = int64(1024)
	switch mode {
	case config.ThinkingLow:
		budget := int64(float64(maxTokens) * 0.2)
		if budget < minBudget {
			budget = minBudget
		}
		return budget
	case config.ThinkingMedium:
		budget := int64(float64(maxTokens) * 0.5)
		if budget < minBudget*2 {
			budget = minBudget * 2
		}
		return budget
	case config.ThinkingHigh:
		budget := int64(float64(maxTokens) * 0.8)
		if budget < minBudget*4 {
			budget = minBudget * 4
		}
		return budget
	default:
		return 0
	}
}

func isAdaptiveThinkingModel(apiModel string) bool {
	switch strings.ToLower(apiModel) {
	case "claude-sonnet-4-6", "claude-opus-4-6", "claude-sonnet-4.6", "claude-opus-4.6":
		return true
	default:
		return false
	}
}

func (a *anthropicClient) thinkingRequestOptions() []option.RequestOption {
	if budgetTokens := thinkingBudgetTokens(a.options.thinkingMode, a.providerOptions.maxTokens); budgetTokens > 0 &&
		isAdaptiveThinkingModel(a.providerOptions.model.APIModel) {
		// anthropic-sdk-go v1.4.0 has no ThinkingConfigParamOfAdaptive helper yet.
		return []option.RequestOption{
			option.WithJSONSet("thinking", map[string]any{
				"type": "adaptive",
			}),
		}
	}

	return nil
}

func WithAnthropicThinkingMode(mode config.ThinkingMode) AnthropicOption {
	return func(options *anthropicOptions) {
		options.thinkingMode = mode
	}
}

func WithAnthropicOAuthToken(token string) AnthropicOption {
	return func(options *anthropicOptions) {
		options.oauthToken = token
	}
}
