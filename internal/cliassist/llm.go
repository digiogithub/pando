package cliassist

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/message"
)

// FetchCommand sends a system+user prompt to the cli-assist model and returns the cleaned command string.
func FetchCommand(ctx context.Context, cfg *config.Config, systemPrompt, userPrompt string) (string, error) {
	// Determine model ID to use
	modelID := cfg.CLIAssist.Model
	if modelID == "" {
		if coderAgent, ok := cfg.Agents[config.AgentCoder]; ok {
			modelID = coderAgent.Model
		}
	}
	if modelID == "" {
		return "", fmt.Errorf("no model configured for cli-assist; set CLIAssist.Model in config or configure a coder agent")
	}

	// Resolve model from supported models registry
	model, ok := models.SupportedModels[modelID]
	if !ok {
		return "", fmt.Errorf("model %s not supported", modelID)
	}

	// Look up provider config
	providerCfg, ok := cfg.Providers[model.Provider]
	if !ok {
		return "", fmt.Errorf("provider %s not configured", model.Provider)
	}
	if providerCfg.Disabled {
		return "", fmt.Errorf("provider %s is disabled", model.Provider)
	}

	// Apply timeout
	timeout := cfg.CLIAssist.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Show simple indicator on stderr when it is a terminal
	if fi, err := os.Stderr.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		fmt.Fprintf(os.Stderr, "Thinking...\n")
	}

	// Create provider
	p, err := provider.NewProvider(
		model.Provider,
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithUseOAuth(providerCfg.UseOAuth),
		provider.WithModel(model),
		provider.WithSystemMessage(systemPrompt),
		provider.WithMaxTokens(model.DefaultMaxTokens),
	)
	if err != nil {
		return "", fmt.Errorf("initializing provider: %w", err)
	}

	// Build user message
	msgs := []message.Message{
		{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: userPrompt}},
		},
	}

	resp, err := p.SendMessages(ctx, msgs, nil)
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	return CleanCommand(resp.Content), nil
}
