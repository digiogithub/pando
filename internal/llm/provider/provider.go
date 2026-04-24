package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

type EventType string

const (
	maxRetries  = 10    // matches claude-code DEFAULT_MAX_RETRIES
	baseDelayMs = 500   // matches claude-code BASE_DELAY_MS (was 2000)
	maxDelayMs  = 32000 // matches claude-code default cap
)

const (
	EventContentStart  EventType = "content_start"
	EventToolUseStart  EventType = "tool_use_start"
	EventToolUseDelta  EventType = "tool_use_delta"
	EventToolUseStop   EventType = "tool_use_stop"
	EventContentDelta  EventType = "content_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventContentStop   EventType = "content_stop"
	EventComplete      EventType = "complete"
	EventError         EventType = "error"
	EventWarning       EventType = "warning"
)

type TokenUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
}

type ProviderResponse struct {
	Content      string
	ToolCalls    []message.ToolCall
	Usage        TokenUsage
	FinishReason message.FinishReason
}

type ProviderEvent struct {
	Type EventType

	Content  string
	Thinking string
	Response *ProviderResponse
	ToolCall *message.ToolCall
	Error    error
}
type Provider interface {
	SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)

	StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent

	Model() models.Model
}

type providerClientOptions struct {
	apiKey        string
	useOAuth      bool
	model         models.Model
	maxTokens     int64
	systemMessage string

	anthropicOptions []AnthropicOption
	openaiOptions    []OpenAIOption
	geminiOptions    []GeminiOption
	bedrockOptions   []BedrockOption
	copilotOptions   []CopilotOption
}

type ProviderClientOption func(*providerClientOptions)

type ProviderClient interface {
	send(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error)
	stream(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent
}

type baseProvider[C ProviderClient] struct {
	options providerClientOptions
	client  C
}

func wrapInstrumented(p Provider, err error) (Provider, error) {
	if err != nil || p == nil {
		return nil, err
	}
	return NewInstrumentedProvider(p), nil
}

func NewProvider(providerName models.ModelProvider, opts ...ProviderClientOption) (Provider, error) {
	clientOptions := providerClientOptions{}
	for _, o := range opts {
		o(&clientOptions)
	}
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Creating provider", "provider", providerName, "model", clientOptions.model.APIModel)
	}
	switch providerName {
	case models.ProviderCopilot:
		return wrapInstrumented(&baseProvider[CopilotClient]{
			options: clientOptions,
			client:  newCopilotClient(clientOptions),
		}, nil)
	case models.ProviderAnthropic:
		anthropicOpts := clientOptions.anthropicOptions
		// Use OAuth when explicitly requested via UseOAuth config flag, or as a
		// fallback when no API key is configured.
		// When UseOAuth is true, Claude Code credentials take priority over any
		// configured API key (the key is cleared so the OAuth path is used).
		useOAuth := clientOptions.useOAuth || clientOptions.apiKey == ""
		if useOAuth {
			if creds, source, err := auth.LoadClaudeCredentials(); err == nil && creds != nil {
				if token, updatedCreds, err := auth.GetClaudeToken(creds); err == nil && token != "" {
					// Save refreshed token back to the same source it came from.
					if updatedCreds != nil {
						if source == "claude-code" {
							_ = auth.SaveClaudeCodeCredentials(updatedCreds)
						} else {
							_ = auth.SaveClaudeCredentials(updatedCreds)
						}
					}
					anthropicOpts = append(anthropicOpts, WithAnthropicOAuthToken(token))
					clientOptions.apiKey = "" // OAuth takes over — clear API key
					logging.Debug("Using Claude OAuth token for authentication", "source", source, "explicit", clientOptions.useOAuth)
				}
			}
		}
		return wrapInstrumented(&baseProvider[AnthropicClient]{
			options: clientOptions,
			client: newAnthropicClient(providerClientOptions{
				apiKey:           clientOptions.apiKey,
				model:            clientOptions.model,
				maxTokens:        clientOptions.maxTokens,
				systemMessage:    clientOptions.systemMessage,
				anthropicOptions: anthropicOpts,
			}),
		}, nil)
	case models.ProviderOpenAI:
		return wrapInstrumented(&baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil)
	case models.ProviderOllama:
		// Ollama's current /v1/chat/completions and /v1/models endpoints are compatible
		// with the OpenAI client flow already used throughout Pando.
		clientOptions.openaiOptions = ensureOpenAIBaseURL(
			clientOptions.openaiOptions,
			models.ResolveOllamaBaseURL(""),
		)
		return wrapInstrumented(&baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil)
	case models.ProviderGemini:
		return wrapInstrumented(&baseProvider[GeminiClient]{
			options: clientOptions,
			client:  newGeminiClient(clientOptions),
		}, nil)
	case models.ProviderBedrock:
		return wrapInstrumented(&baseProvider[BedrockClient]{
			options: clientOptions,
			client:  newBedrockClient(clientOptions),
		}, nil)
	case models.ProviderGROQ:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://api.groq.com/openai/v1"),
		)
		return wrapInstrumented(&baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil)
	case models.ProviderAzure:
		return wrapInstrumented(&baseProvider[AzureClient]{
			options: clientOptions,
			client:  newAzureClient(clientOptions),
		}, nil)
	case models.ProviderVertexAI:
		return wrapInstrumented(&baseProvider[VertexAIClient]{
			options: clientOptions,
			client:  newVertexAIClient(clientOptions),
		}, nil)
	case models.ProviderOpenRouter:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://openrouter.ai/api/v1"),
			WithOpenAIExtraHeaders(map[string]string{
				"HTTP-Referer": "github.com/digiogithub/pando",
				"X-Title":      "Pando",
			}),
		)
		return wrapInstrumented(&baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil)
	case models.ProviderXAI:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL("https://api.x.ai/v1"),
		)
		return wrapInstrumented(&baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil)
	case models.ProviderLocal:
		clientOptions.openaiOptions = append(clientOptions.openaiOptions,
			WithOpenAIBaseURL(os.Getenv("LOCAL_ENDPOINT")),
		)
		return wrapInstrumented(&baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil)
	case models.ProviderOpenAICompatible:
		// openai-compatible endpoints use the standard OpenAI client with a custom base URL.
		// BaseURL and ExtraHeaders must be set via WithOpenAIOptions before calling NewProvider.
		return wrapInstrumented(&baseProvider[OpenAIClient]{
			options: clientOptions,
			client:  newOpenAIClient(clientOptions),
		}, nil)
	case models.ProviderMock:
		// TODO: implement mock client for test
		panic("not implemented")
	}
	return nil, fmt.Errorf("provider not supported: %s", providerName)
}

// NewProviderFromAccount creates a Provider from a named ProviderAccount configuration.
// This is the preferred way to create providers when using the multi-account system.
func NewProviderFromAccount(account config.ProviderAccount, model models.Model, maxTokens int64, systemMessage string) (Provider, error) {
	opts := []ProviderClientOption{
		WithAPIKey(account.APIKey),
		WithUseOAuth(account.UseOAuth),
		WithModel(model),
		WithMaxTokens(maxTokens),
		WithSystemMessage(systemMessage),
	}

	// Apply cache-disable options based on global config
	anthCacheOpts, oaiCacheOpts, gemCacheOpts := CacheDisabledOptions()
	if len(anthCacheOpts) > 0 {
		opts = append(opts, WithAnthropicOptions(anthCacheOpts...))
	}
	if len(oaiCacheOpts) > 0 {
		opts = append(opts, WithOpenAIOptions(oaiCacheOpts...))
	}
	if len(gemCacheOpts) > 0 {
		opts = append(opts, WithGeminiOptions(gemCacheOpts...))
	}

	providerType := account.Type

	// For openai-compatible, apply baseURL and extraHeaders via OpenAI options
	if providerType == models.ProviderOpenAICompatible {
		var openaiOpts []OpenAIOption
		if account.BaseURL != "" {
			openaiOpts = append(openaiOpts, WithOpenAIBaseURL(account.BaseURL))
		}
		if len(account.ExtraHeaders) > 0 {
			openaiOpts = append(openaiOpts, WithOpenAIExtraHeaders(account.ExtraHeaders))
		}
		if len(openaiOpts) > 0 {
			opts = append(opts, WithOpenAIOptions(openaiOpts...))
		}
		// openai-compatible uses the OpenAI client
		return NewProvider(models.ProviderOpenAICompatible, opts...)
	}

	// For providers that support a custom BaseURL (ollama, openai, etc.), pass it via their option
	if account.BaseURL != "" {
		switch providerType {
		case models.ProviderOllama:
			opts = append(opts, WithOpenAIOptions(WithOpenAIBaseURL(account.BaseURL)))
		case models.ProviderOpenAI:
			opts = append(opts, WithOpenAIOptions(WithOpenAIBaseURL(account.BaseURL)))
		}
	}

	return NewProvider(providerType, opts...)
}

func (p *baseProvider[C]) cleanMessages(messages []message.Message) (cleaned []message.Message) {
	for _, msg := range messages {
		// The message has no content
		if len(msg.Parts) == 0 {
			continue
		}
		cleaned = append(cleaned, msg)
	}
	return
}

func (p *baseProvider[C]) SendMessages(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error) {
	messages = p.cleanMessages(messages)
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Sending messages", "model", p.options.model.APIModel, "message_count", len(messages))
	}
	return p.client.send(ctx, messages, tools)
}

func (p *baseProvider[C]) Model() models.Model {
	return p.options.model
}

func (p *baseProvider[C]) StreamResponse(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent {
	messages = p.cleanMessages(messages)
	if cfg := config.Get(); cfg != nil && cfg.Debug {
		logging.Debug("Starting stream response", "model", p.options.model.APIModel, "message_count", len(messages), "tool_count", len(tools))
	}
	return p.client.stream(ctx, messages, tools)
}

func WithAPIKey(apiKey string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.apiKey = apiKey
	}
}

func WithUseOAuth(useOAuth bool) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.useOAuth = useOAuth
	}
}

func WithModel(model models.Model) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.model = model
	}
}

func WithMaxTokens(maxTokens int64) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.maxTokens = maxTokens
	}
}

func WithSystemMessage(systemMessage string) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.systemMessage = systemMessage
	}
}

func WithAnthropicOptions(anthropicOptions ...AnthropicOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.anthropicOptions = anthropicOptions
	}
}

func WithOpenAIOptions(openaiOptions ...OpenAIOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.openaiOptions = openaiOptions
	}
}

func WithGeminiOptions(geminiOptions ...GeminiOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.geminiOptions = geminiOptions
	}
}

func WithBedrockOptions(bedrockOptions ...BedrockOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.bedrockOptions = bedrockOptions
	}
}

func WithCopilotOptions(copilotOptions ...CopilotOption) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.copilotOptions = copilotOptions
	}
}

// CacheDisabledOptions returns provider options that disable LLM prompt caching,
// based on the global config. Returns nil slices if caching is enabled.
func CacheDisabledOptions() (anthropicOpts []AnthropicOption, openaiOpts []OpenAIOption, geminiOpts []GeminiOption) {
	cfg := config.Get()
	if cfg == nil || cfg.LLMCache.Enabled {
		return // caching enabled, no extra options needed
	}
	// Caching disabled — apply disable-cache options per provider
	anthropicOpts = append(anthropicOpts, WithAnthropicDisableCache())
	openaiOpts = append(openaiOpts, WithOpenAIDisableCache())
	geminiOpts = append(geminiOpts, WithGeminiDisableCache())
	return
}

func ensureOpenAIBaseURL(options []OpenAIOption, baseURL string) []OpenAIOption {
	if strings.TrimSpace(baseURL) == "" {
		return options
	}

	configured := openaiOptions{}
	for _, option := range options {
		option(&configured)
	}
	if strings.TrimSpace(configured.baseURL) != "" {
		return options
	}

	return append(options, WithOpenAIBaseURL(baseURL))
}
