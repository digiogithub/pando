package agent

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/message"
)

func maxTokensReply(text string) []provider.ProviderEvent {
	return []provider.ProviderEvent{
		{Type: provider.EventContentDelta, Content: text},
		{Type: provider.EventComplete, Response: &provider.ProviderResponse{
			Content: text, FinishReason: message.FinishReasonMaxTokens,
		}},
	}
}

func lastUserText(history []message.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == message.User {
			return history[i].Content().String()
		}
	}
	return ""
}

func TestOutputLimitContinuesTurn(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(c *config.ModelAutoModeConfig) { c.Enabled = false }))
	prov := env.provs[autoCoder]
	prov.respond = func(call int, _ []message.Message) []provider.ProviderEvent {
		if call == 0 {
			return maxTokensReply("writing the plan")
		}
		return textReply("plan saved in parts")
	}

	events := env.run(env.session("s-limit", autoCoder), "write the plan")

	final := finalEvent(events)
	if final.Type != AgentEventTypeResponse || final.Message.FinishReason() != message.FinishReasonEndTurn {
		t.Fatalf("turn should end normally after continuing, got %v / %v", final.Type, final.Message.FinishReason())
	}
	if got := prov.callCount(); got != 2 {
		t.Fatalf("expected one continuation request, got %d calls", got)
	}
	if prompt := lastUserText(prov.history(1)); !strings.Contains(prompt, "output limit") || !strings.Contains(prompt, "NOT executed") {
		t.Fatalf("continuation prompt missing, last user message: %q", prompt)
	}
	found := false
	for _, n := range notices(events) {
		if strings.Contains(n, "output limit") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a visible output-limit notice, got %v", notices(events))
	}
}

func TestOutputLimitContinuationIsBounded(t *testing.T) {
	env := newAutoEnv(t, withAutoConfig(func(c *config.ModelAutoModeConfig) { c.Enabled = false }))
	prov := env.provs[autoCoder]
	prov.respond = func(int, []message.Message) []provider.ProviderEvent {
		return maxTokensReply("still too long")
	}

	events := env.run(env.session("s-limit-loop", autoCoder), "write the plan")

	if got := prov.callCount(); got != maxOutputLimitContinuations+1 {
		t.Fatalf("expected %d requests, got %d", maxOutputLimitContinuations+1, got)
	}
	final := finalEvent(events)
	if final.Type != AgentEventTypeResponse || final.Message.FinishReason() != message.FinishReasonMaxTokens {
		t.Fatalf("turn should stop on max_tokens after the bound, got %v / %v", final.Type, final.Message.FinishReason())
	}
	ns := notices(events)
	if len(ns) == 0 || !strings.Contains(ns[len(ns)-1], "stopped") {
		t.Fatalf("expected a final stop notice, got %v", ns)
	}
}
