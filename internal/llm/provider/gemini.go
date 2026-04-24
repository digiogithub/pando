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

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
	"github.com/google/uuid"
	"google.golang.org/genai"
)

type geminiOptions struct {
	// disableCache is wired to the global LLMCache config but has no real effect
	// on implicit server-side caching: Gemini 2.5+ caches automatically and there
	// is no public API to opt out. Explicit Context Caching (a separate paid feature)
	// is not currently used in Pando. This field is kept for future compatibility.
	disableCache bool
}

type GeminiOption func(*geminiOptions)

type geminiClient struct {
	providerOptions providerClientOptions
	options         geminiOptions
	client          *genai.Client
}

type GeminiClient ProviderClient

func newGeminiClient(opts providerClientOptions) GeminiClient {
	geminiOpts := geminiOptions{}
	for _, o := range opts.geminiOptions {
		o(&geminiOpts)
	}

	genaiConfig := &genai.ClientConfig{APIKey: opts.apiKey, Backend: genai.BackendGeminiAPI}
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		// Inject the debug HTTP transport so every API call is logged to disk.
		genaiConfig.HTTPClient = &http.Client{Transport: newDebugRoundTripper(nil)}
	}

	client, err := genai.NewClient(context.Background(), genaiConfig)
	if err != nil {
		logging.Error("Failed to create Gemini client", "error", err)
		return nil
	}

	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Creating Gemini client", "model", opts.model.APIModel)
	}
	return &geminiClient{
		providerOptions: opts,
		options:         geminiOpts,
		client:          client,
	}
}

func (g *geminiClient) convertMessages(messages []message.Message) []*genai.Content {
	var history []*genai.Content
	for _, msg := range messages {
		switch msg.Role {
		case message.User:
			var parts []*genai.Part
			parts = append(parts, &genai.Part{Text: msg.Content().String()})
			for _, binaryContent := range msg.BinaryContent() {
				parts = append(parts, &genai.Part{InlineData: &genai.Blob{
					MIMEType: binaryContent.MIMEType,
					Data:     binaryContent.Data,
				}})
			}
			history = append(history, &genai.Content{
				Parts: parts,
				Role:  "user",
			})
		case message.Assistant:
			var assistantParts []*genai.Part

			if msg.Content().String() != "" {
				assistantParts = append(assistantParts, &genai.Part{Text: msg.Content().String()})
			}

			if len(msg.ToolCalls()) > 0 {
				for _, call := range msg.ToolCalls() {
					args, _ := parseJsonToMap(call.Input)
					assistantParts = append(assistantParts, &genai.Part{
						FunctionCall: &genai.FunctionCall{
							Name: call.Name,
							Args: args,
							ID:   call.ID,
						},
						ThoughtSignature: call.ThoughtSignature,
					})
				}
			}

			if len(assistantParts) > 0 {
				history = append(history, &genai.Content{
					Role:  "model",
					Parts: assistantParts,
				})
			}

		case message.Tool:
			for _, result := range msg.ToolResults() {
				response := map[string]interface{}{"result": result.Content}
				parsed, err := parseJsonToMap(result.Content)
				if err == nil {
					response = parsed
				}

				var toolCall message.ToolCall
				for _, m := range messages {
					if m.Role == message.Assistant {
						for _, call := range m.ToolCalls() {
							if call.ID == result.ToolCallID {
								toolCall = call
								break
							}
						}
					}
				}

				history = append(history, &genai.Content{
					Parts: []*genai.Part{
						{
							FunctionResponse: &genai.FunctionResponse{
								Name:     toolCall.Name,
								Response: response,
							},
						},
					},
					Role: "user",
				})
			}
		}
	}

	return history
}

func (g *geminiClient) convertTools(tools []tools.BaseTool) []*genai.Tool {
	geminiTool := &genai.Tool{}
	geminiTool.FunctionDeclarations = make([]*genai.FunctionDeclaration, 0, len(tools))

	for _, tool := range tools {
		info := tool.Info()
		properties := convertSchemaProperties(info.Parameters)
		required := sanitizeRequired(info.Required, properties)
		declaration := &genai.FunctionDeclaration{
			Name:        info.Name,
			Description: info.Description,
			Parameters: &genai.Schema{
				Type:       genai.TypeObject,
				Properties: properties,
				Required:   required,
			},
		}

		geminiTool.FunctionDeclarations = append(geminiTool.FunctionDeclarations, declaration)
	}

	return []*genai.Tool{geminiTool}
}

func (g *geminiClient) finishReason(reason genai.FinishReason) message.FinishReason {
	switch {
	case reason == genai.FinishReasonStop:
		return message.FinishReasonEndTurn
	case reason == genai.FinishReasonMaxTokens:
		return message.FinishReasonMaxTokens
	default:
		return message.FinishReasonUnknown
	}
}

func (g *geminiClient) buildThinkingConfig() *genai.ThinkingConfig {
	model := strings.ToLower(strings.TrimPrefix(g.providerOptions.model.APIModel, "models/"))

	switch {
	case strings.HasPrefix(model, "gemini-3") && !strings.Contains(model, "flash") && !strings.Contains(model, "lite"):
		// Gemini 3 Pro models support ThinkingLevel.
		return &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingLevel:   genai.ThinkingLevelHigh,
		}
	case strings.HasPrefix(model, "gemini-3"):
		// Gemini 3 Flash / Flash-Lite models use a thinking budget like 2.5.
		return &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  int32Ptr(8192),
		}
	case strings.HasPrefix(model, "gemini-2.5") && !strings.Contains(model, "flash-lite"):
		return &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  int32Ptr(2000),
		}
	default:
		return nil
	}
}

func (g *geminiClient) send(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error) {
	// Convert messages
	geminiMessages := g.convertMessages(messages)
	if len(geminiMessages) == 0 {
		return nil, errors.New("no messages to send to Gemini")
	}

	cfg := config.Get()
	if cfg.Debug {
		jsonData, _ := json.Marshal(geminiMessages)
		logging.Debug("Prepared messages", "messages", string(jsonData))
	}

	history := geminiMessages[:len(geminiMessages)-1] // All but last message
	lastMsg := geminiMessages[len(geminiMessages)-1]
	config := &genai.GenerateContentConfig{
		MaxOutputTokens: int32(g.providerOptions.maxTokens),
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: g.providerOptions.systemMessage}},
		},
		ThinkingConfig: g.buildThinkingConfig(),
	}
	if len(tools) > 0 {
		config.Tools = g.convertTools(tools)
	}
	chat, err := g.client.Chats.Create(ctx, g.providerOptions.model.APIModel, config, history)
	if err != nil {
		return nil, err
	}

	attempts := 0
	for {
		attempts++
		var toolCalls []message.ToolCall

		var lastMsgParts []genai.Part
		for _, part := range lastMsg.Parts {
			lastMsgParts = append(lastMsgParts, *part)
		}
		resp, err := chat.SendMessage(ctx, lastMsgParts...)
		// If there is an error we are going to see if we can retry the call
		if err != nil {
			retry, after, retryErr := g.shouldRetry(attempts, err)
			if retryErr != nil {
				return nil, retryErr
			}
			if retry {
				logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
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

		if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
			content = joinVisibleTextParts(resp.Candidates[0].Content.Parts)
			for _, part := range resp.Candidates[0].Content.Parts {
				switch {
				case part.FunctionCall != nil:
					id := "call_" + uuid.New().String()
					args, _ := json.Marshal(part.FunctionCall.Args)
					toolCalls = append(toolCalls, message.ToolCall{
						ID:               coalesceString(part.FunctionCall.ID, id),
						Name:             part.FunctionCall.Name,
						Input:            string(args),
						Type:             "function",
						Finished:         true,
						ThoughtSignature: cloneBytes(part.ThoughtSignature),
					})
				}
			}
		}
		finishReason := message.FinishReasonEndTurn
		if len(resp.Candidates) > 0 {
			finishReason = g.finishReason(resp.Candidates[0].FinishReason)
		}
		if len(toolCalls) > 0 {
			finishReason = message.FinishReasonToolUse
		}

		if cfg != nil && cfg.Debug {
			logging.Debug("Gemini send completed", "model", g.providerOptions.model.APIModel, "content_length", len(content))
		}
		return &ProviderResponse{
			Content:      content,
			ToolCalls:    toolCalls,
			Usage:        g.usage(resp),
			FinishReason: finishReason,
		}, nil
	}
}

func (g *geminiClient) stream(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent {
	eventChan := make(chan ProviderEvent)

	// Convert messages
	geminiMessages := g.convertMessages(messages)
	if len(geminiMessages) == 0 {
		go func() {
			defer close(eventChan)
			eventChan <- ProviderEvent{Type: EventError, Error: errors.New("no messages to send to Gemini")}
		}()
		return eventChan
	}

	cfg := config.Get()
	if cfg.Debug {
		jsonData, _ := json.Marshal(geminiMessages)
		logging.Debug("Prepared messages", "messages", string(jsonData))
	}

	history := geminiMessages[:len(geminiMessages)-1] // All but last message
	lastMsg := geminiMessages[len(geminiMessages)-1]
	config := &genai.GenerateContentConfig{
		MaxOutputTokens: int32(g.providerOptions.maxTokens),
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{{Text: g.providerOptions.systemMessage}},
		},
		ThinkingConfig: g.buildThinkingConfig(),
	}
	if len(tools) > 0 {
		config.Tools = g.convertTools(tools)
	}
	chat, err := g.client.Chats.Create(ctx, g.providerOptions.model.APIModel, config, history)
	if err != nil {
		go func() {
			defer close(eventChan)
			eventChan <- ProviderEvent{Type: EventError, Error: err}
		}()
		return eventChan
	}

	attempts := 0

	go func() {
		defer close(eventChan)

		for {
			attempts++
			if cfg != nil && cfg.Debug {
				logging.Debug("Gemini stream started", "model", g.providerOptions.model.APIModel, "attempt", attempts)
			}

			currentContent := ""
			toolCalls := []message.ToolCall{}
			var finalResp *genai.GenerateContentResponse

			eventChan <- ProviderEvent{Type: EventContentStart}

			var lastMsgParts []genai.Part

			for _, part := range lastMsg.Parts {
				lastMsgParts = append(lastMsgParts, *part)
			}
			retryStream := false
			for resp, err := range chat.SendMessageStream(ctx, lastMsgParts...) {
				if err != nil {
					retry, after, retryErr := g.shouldRetry(attempts, err)
					if retryErr != nil {
						eventChan <- ProviderEvent{Type: EventError, Error: retryErr}
						return
					}
					if retry {
						logging.WarnPersist(fmt.Sprintf("Retrying due to rate limit... attempt %d of %d", attempts, maxRetries), logging.PersistTimeArg, time.Millisecond*time.Duration(after+100))
						select {
						case <-ctx.Done():
							if ctx.Err() != nil {
								eventChan <- ProviderEvent{Type: EventError, Error: ctx.Err()}
							}

							return
						case <-time.After(time.Duration(after) * time.Millisecond):
							retryStream = true
						}
						if retryStream {
							break
						}
					} else {
						eventChan <- ProviderEvent{Type: EventError, Error: err}
						return
					}
				}

				if resp == nil {
					continue
				}

				finalResp = resp

				if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
					for _, delta := range visibleTextParts(resp.Candidates[0].Content.Parts) {
						eventChan <- ProviderEvent{
							Type:    EventContentDelta,
							Content: delta,
						}
						currentContent += delta
					}

					for _, part := range resp.Candidates[0].Content.Parts {
						switch {
						case part.FunctionCall != nil:
							id := "call_" + uuid.New().String()
							args, _ := json.Marshal(part.FunctionCall.Args)
							newCall := message.ToolCall{
								ID:               coalesceString(part.FunctionCall.ID, id),
								Name:             part.FunctionCall.Name,
								Input:            string(args),
								Type:             "function",
								Finished:         true,
								ThoughtSignature: cloneBytes(part.ThoughtSignature),
							}

							isNew := true
							for _, existing := range toolCalls {
								if existing.Name == newCall.Name && existing.Input == newCall.Input {
									isNew = false
									break
								}
							}

							if isNew {
								toolCalls = append(toolCalls, newCall)
							}
						}
					}
				}
			}

			if retryStream {
				continue
			}

			eventChan <- ProviderEvent{Type: EventContentStop}

			if finalResp != nil {

				finishReason := message.FinishReasonEndTurn
				if len(finalResp.Candidates) > 0 {
					finishReason = g.finishReason(finalResp.Candidates[0].FinishReason)
				}
				if len(toolCalls) > 0 {
					finishReason = message.FinishReasonToolUse
				}
				if cfg != nil && cfg.Debug {
					logging.Debug("Gemini stream completed", "model", g.providerOptions.model.APIModel)
				}
				eventChan <- ProviderEvent{
					Type: EventComplete,
					Response: &ProviderResponse{
						Content:      currentContent,
						ToolCalls:    toolCalls,
						Usage:        g.usage(finalResp),
						FinishReason: finishReason,
					},
				}
				return
			}

		}
	}()

	return eventChan
}

func (g *geminiClient) shouldRetry(attempts int, err error) (bool, int64, error) {
	// Check if error is a rate limit error
	if attempts > maxRetries {
		return false, 0, fmt.Errorf("maximum retry attempts reached for rate limit: %d retries", maxRetries)
	}

	// Gemini doesn't have a standard error type we can check against
	// So we'll check the error message for rate limit indicators
	if errors.Is(err, io.EOF) {
		return false, 0, err
	}

	errMsg := err.Error()
	isRateLimit := false

	// Check for common rate limit error messages
	if contains(errMsg, "rate limit", "quota exceeded", "too many requests") {
		isRateLimit = true
	}

	if !isRateLimit {
		return false, 0, err
	}

	// Calculate backoff with jitter
	backoffMs := 2000 * (1 << (attempts - 1))
	jitterMs := int(float64(backoffMs) * 0.2)
	retryMs := backoffMs + jitterMs

	return true, int64(retryMs), nil
}

func (g *geminiClient) toolCalls(resp *genai.GenerateContentResponse) []message.ToolCall {
	var toolCalls []message.ToolCall

	if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.FunctionCall != nil {
				id := "call_" + uuid.New().String()
				args, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, message.ToolCall{
					ID:               coalesceString(part.FunctionCall.ID, id),
					Name:             part.FunctionCall.Name,
					Input:            string(args),
					Type:             "function",
					ThoughtSignature: cloneBytes(part.ThoughtSignature),
				})
			}
		}
	}

	return toolCalls
}

func (g *geminiClient) usage(resp *genai.GenerateContentResponse) TokenUsage {
	if resp == nil || resp.UsageMetadata == nil {
		return TokenUsage{}
	}

	return TokenUsage{
		InputTokens:         int64(resp.UsageMetadata.PromptTokenCount),
		OutputTokens:        int64(resp.UsageMetadata.CandidatesTokenCount),
		CacheCreationTokens: 0, // Not directly provided by Gemini
		CacheReadTokens:     int64(resp.UsageMetadata.CachedContentTokenCount),
	}
}

// WithGeminiDisableCache sets the disableCache flag for Gemini providers.
// NOTE: Gemini uses implicit server-side caching with no public API to disable it.
// Explicit Context Caching (a separate feature) is not used by Pando.
// This option is kept for future compatibility when Gemini adds an opt-out mechanism.
func WithGeminiDisableCache() GeminiOption {
	return func(options *geminiOptions) {
		options.disableCache = true
	}
}

// Helper functions
func parseJsonToMap(jsonStr string) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	return result, err
}

func convertSchemaProperties(parameters map[string]interface{}) map[string]*genai.Schema {
	properties := make(map[string]*genai.Schema)

	for name, param := range parameters {
		sanitized := sanitizeGeminiSchema(param)
		properties[name] = convertToSchema(sanitized)
	}

	return properties
}

func convertToSchema(param interface{}) *genai.Schema {
	schema := &genai.Schema{Type: genai.TypeString}

	paramMap, ok := param.(map[string]interface{})
	if !ok {
		return schema
	}

	if desc, ok := paramMap["description"].(string); ok {
		schema.Description = desc
	}

	if format, ok := paramMap["format"].(string); ok {
		schema.Format = format
	}

	if enumVals, ok := paramMap["enum"].([]interface{}); ok {
		enum := make([]string, 0, len(enumVals))
		for _, v := range enumVals {
			enum = append(enum, fmt.Sprint(v))
		}
		schema.Enum = enum
	}

	if anyOfVals, ok := paramMap["anyOf"].([]interface{}); ok {
		anyOf := make([]*genai.Schema, 0, len(anyOfVals))
		for _, item := range anyOfVals {
			anyOf = append(anyOf, convertToSchema(item))
		}
		schema.AnyOf = anyOf
	}

	typeVal, hasType := paramMap["type"]
	if !hasType {
		return schema
	}

	typeStr, ok := typeVal.(string)
	if !ok {
		return schema
	}

	schema.Type = mapJSONTypeToGenAI(typeStr)

	switch typeStr {
	case "array":
		schema.Items = processArrayItems(paramMap)
	case "object":
		if props, ok := paramMap["properties"].(map[string]interface{}); ok {
			schema.Properties = convertSchemaProperties(props)
			schema.Required = sanitizeRequired(readStringSlice(paramMap["required"]), schema.Properties)
		}
	}

	return schema
}

func processArrayItems(paramMap map[string]interface{}) *genai.Schema {
	items, ok := paramMap["items"].(map[string]interface{})
	if !ok {
		return &genai.Schema{Type: genai.TypeString}
	}

	return convertToSchema(items)
}

func sanitizeGeminiSchema(param interface{}) interface{} {
	paramMap, ok := param.(map[string]interface{})
	if !ok {
		return param
	}

	sanitized := map[string]interface{}{}
	for key, value := range paramMap {
		switch typed := value.(type) {
		case map[string]interface{}:
			sanitized[key] = sanitizeGeminiSchema(typed)
		case []interface{}:
			arr := make([]interface{}, 0, len(typed))
			for _, item := range typed {
				arr = append(arr, sanitizeGeminiSchema(item))
			}
			sanitized[key] = arr
		default:
			sanitized[key] = value
		}
	}

	typeStr, _ := sanitized["type"].(string)
	if enumVals, ok := sanitized["enum"].([]interface{}); ok {
		strEnum := make([]interface{}, 0, len(enumVals))
		for _, v := range enumVals {
			strEnum = append(strEnum, fmt.Sprint(v))
		}
		sanitized["enum"] = strEnum
		if typeStr == "integer" || typeStr == "number" {
			sanitized["type"] = "string"
			typeStr = "string"
		}
	}

	if typeStr == "array" {
		items, hasItems := sanitized["items"]
		if !hasItems || items == nil {
			sanitized["items"] = map[string]interface{}{"type": "string"}
		} else if itemsMap, ok := items.(map[string]interface{}); ok {
			if len(itemsMap) == 0 {
				sanitized["items"] = map[string]interface{}{"type": "string"}
			}
		}
	}

	hasCombiner := sanitized["anyOf"] != nil || sanitized["oneOf"] != nil || sanitized["allOf"] != nil
	if typeStr != "" && typeStr != "object" && !hasCombiner {
		delete(sanitized, "properties")
		delete(sanitized, "required")
	}

	if typeStr == "object" {
		if props, ok := sanitized["properties"].(map[string]interface{}); ok {
			if requiredRaw, ok := sanitized["required"].([]interface{}); ok {
				required := make([]interface{}, 0, len(requiredRaw))
				for _, r := range requiredRaw {
					name, ok := r.(string)
					if ok {
						if _, exists := props[name]; exists {
							required = append(required, name)
						}
					}
				}
				sanitized["required"] = required
			}
		}
	}

	return sanitized
}

func readStringSlice(value interface{}) []string {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

func sanitizeRequired(required []string, properties map[string]*genai.Schema) []string {
	if len(required) == 0 || len(properties) == 0 {
		return nil
	}

	sanitized := make([]string, 0, len(required))
	for _, name := range required {
		if _, ok := properties[name]; ok {
			sanitized = append(sanitized, name)
		}
	}

	if len(sanitized) == 0 {
		return nil
	}

	return sanitized
}

func mapJSONTypeToGenAI(jsonType string) genai.Type {
	switch jsonType {
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	case "array":
		return genai.TypeArray
	case "object":
		return genai.TypeObject
	default:
		return genai.TypeString // Default to string for unknown types
	}
}

func contains(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if strings.Contains(strings.ToLower(s), strings.ToLower(substr)) {
			return true
		}
	}
	return false
}

func visibleTextParts(parts []*genai.Part) []string {
	visible := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != nil && part.Text != "" && !part.Thought {
			visible = append(visible, part.Text)
		}
	}
	return visible
}

func joinVisibleTextParts(parts []*genai.Part) string {
	return strings.Join(visibleTextParts(parts), "")
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	cloned := make([]byte, len(src))
	copy(cloned, src)
	return cloned
}

func coalesceString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func int32Ptr(v int32) *int32 {
	return &v
}
