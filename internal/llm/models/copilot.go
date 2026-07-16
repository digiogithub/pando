package models

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	ProviderCopilot ModelProvider = "copilot"

	// Model IDs match the dynamic format: "copilot.{api-id}"
	CopilotGTP35Turbo      ModelID = "copilot.gpt-3.5-turbo"
	CopilotGPT4o           ModelID = "copilot.gpt-4o"
	CopilotGPT4oMini       ModelID = "copilot.gpt-4o-mini"
	CopilotGPT41           ModelID = "copilot.gpt-4.1"
	CopilotClaude35        ModelID = "copilot.claude-3.5-sonnet"
	CopilotClaude37        ModelID = "copilot.claude-3.7-sonnet"
	CopilotClaude4         ModelID = "copilot.claude-sonnet-4"
	CopilotO1              ModelID = "copilot.o1"
	CopilotO3Mini          ModelID = "copilot.o3-mini"
	CopilotO4Mini          ModelID = "copilot.o4-mini"
	CopilotGemini20        ModelID = "copilot.gemini-2.0-flash"
	CopilotGemini25        ModelID = "copilot.gemini-2.5-pro"
	CopilotGPT4            ModelID = "copilot.gpt-4"
	CopilotClaude37Thought ModelID = "copilot.claude-3.7-sonnet-thought"
	CopilotGPT54           ModelID = "copilot.gpt-5.4"
	CopilotClaudeOpus4     ModelID = "copilot.claude-opus-4"
	CopilotGemini25Flash   ModelID = "copilot.gemini-2.5-flash"
)

// IsCopilotAnthropicModel returns true if the given API model name is an Anthropic model
// served through the GitHub Copilot API (detected by name containing "claude").
func IsCopilotAnthropicModel(apiModel string) bool {
	return strings.Contains(strings.ToLower(apiModel), "claude")
}

// Endpoint identifiers as reported by the Copilot /models API in
// "supported_endpoints".
const (
	CopilotEndpointChatCompletions = "/chat/completions"
	CopilotEndpointResponses       = "/responses"
	CopilotEndpointMessages        = "/v1/messages"
)

// CopilotModelUsesResponsesAPI reports whether a model must be driven through the
// OpenAI Responses API (/responses) instead of Chat Completions.
//
// The Copilot /models API advertises the routes each model accepts, and that
// metadata is authoritative: models such as "mai-code-1-flash-picker" or
// "gpt-5.6-luna" only accept /responses while carrying names that no
// gpt-version heuristic can classify. This mirrors how the official VS Code
// extension routes requests (see chatEndpoint.ts, useResponsesApi).
//
// When a model advertises both routes we keep the historical name-based
// decision, so models like gpt-5-mini keep working over Chat Completions.
// Models with no advertised endpoints (static catalogue entries, older API
// responses) fall back to the name heuristic entirely.
func CopilotModelUsesResponsesAPI(m Model) bool {
	if len(m.SupportedEndpoints) == 0 {
		return IsCopilotResponsesAPIModel(m.APIModel)
	}
	if !slices.Contains(m.SupportedEndpoints, CopilotEndpointResponses) {
		return false
	}
	if !slices.Contains(m.SupportedEndpoints, CopilotEndpointChatCompletions) {
		return true
	}
	return IsCopilotResponsesAPIModel(m.APIModel)
}

// responsesAPIModelRE matches models like "gpt-5", "gpt-5.4"; compiled once at package level.
var responsesAPIModelRE = regexp.MustCompile(`(?i)^gpt-(\d+)`)

// IsCopilotResponsesAPIModel reports whether a model requires the Responses API
// based on its name alone. It is the fallback used by CopilotModelUsesResponsesAPI
// when the provider does not advertise the model's supported endpoints; prefer
// that function whenever a full Model is available.
// GPT-5+ models (except gpt-5-mini variants) require the Responses API.
func IsCopilotResponsesAPIModel(apiModel string) bool {
	m := responsesAPIModelRE.FindStringSubmatch(apiModel)
	if m == nil {
		return false
	}
	major := 0
	fmt.Sscanf(m[1], "%d", &major)
	return major >= 5 && !strings.HasPrefix(strings.ToLower(apiModel), "gpt-5-mini")
}
