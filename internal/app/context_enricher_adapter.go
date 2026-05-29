package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
)

// plannerProviderAdapter wraps a provider.Provider to satisfy rag.PlannerProvider.
// It avoids the rag→llm/provider import cycle by living in the app package,
// which can safely import both sides.
type plannerProviderAdapter struct {
	p provider.Provider
}

// SendRequest sends a single system+user message and returns the text response.
func (a *plannerProviderAdapter) SendRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	msgs := []message.Message{}
	if strings.TrimSpace(systemPrompt) != "" {
		msgs = append(msgs, message.Message{
			Role:  message.System,
			Parts: []message.ContentPart{message.TextContent{Text: systemPrompt}},
		})
	}
	msgs = append(msgs, message.Message{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: userPrompt}},
	})

	resp, err := a.p.SendMessages(ctx, msgs, []tools.BaseTool{})
	if err != nil {
		return "", fmt.Errorf("planner provider send failed: %w", err)
	}
	return resp.Content, nil
}

// ProviderName returns the underlying model provider name string.
func (a *plannerProviderAdapter) ProviderName() string {
	return string(a.p.Model().Provider)
}
