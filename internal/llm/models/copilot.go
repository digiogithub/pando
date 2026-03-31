package models

import "strings"

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

// IsCopilotResponsesAPIModel returns true if the model must use the OpenAI Responses API
// (/v1/responses) instead of Chat Completions (/v1/chat/completions).
// GPT-5+ models (except gpt-5-mini variants) require the Responses API.
func IsCopilotResponsesAPIModel(apiModel string) bool {
	re := regexp.MustCompile(`^gpt-(\d+)`)
	m := re.FindStringSubmatch(strings.ToLower(apiModel))
	if m == nil {
		return false
	}
	major := 0
	fmt.Sscanf(m[1], "%d", &major)
	return major >= 5 && !strings.HasPrefix(strings.ToLower(apiModel), "gpt-5-mini")
}
