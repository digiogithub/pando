package config

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/llm/models"
)

// TestPropagateCoderModelFillsOnlyEmptyAgents verifies that selecting the coder
// model seeds every agent that has none, and never overwrites an explicit one.
func TestPropagateCoderModelFillsOnlyEmptyAgents(t *testing.T) {
	isolateGlobalConfig(t)
	previous := cfg
	t.Cleanup(func() { cfg = previous })

	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAnthropic: {APIKey: "sk-ant-test"},
		},
		Agents: map[AgentName]Agent{
			AgentCoder: {Model: models.Claude4Sonnet},
			AgentTitle: {Model: models.Claude35Haiku},
		},
	}

	propagateCoderModel(models.Claude4Sonnet, false)

	if got := cfg.Agents[AgentSummarizer].Model; got != models.Claude4Sonnet {
		t.Fatalf("summarizer should inherit the coder model, got %q", got)
	}
	if got := cfg.Agents[AgentTask].Model; got != models.Claude4Sonnet {
		t.Fatalf("task should inherit the coder model, got %q", got)
	}
	if got := cfg.Agents[AgentTitle].Model; got != models.Claude35Haiku {
		t.Fatalf("an explicitly configured agent must be preserved, got %q", got)
	}
}

// TestSetDefaultModelForAgentInheritsCoder checks the non-coder branch: agents
// take the coder model instead of a hardcoded per-provider ID, and stay empty
// while the coder has no model.
func TestSetDefaultModelForAgentInheritsCoder(t *testing.T) {
	isolateGlobalConfig(t)
	previous := cfg
	t.Cleanup(func() { cfg = previous })

	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{},
		Agents:    map[AgentName]Agent{},
	}

	if setDefaultModelForAgent(AgentSummarizer) {
		t.Fatal("no coder model configured: summarizer must be left without a model")
	}
	if got := cfg.Agents[AgentSummarizer].Model; got != "" {
		t.Fatalf("expected empty model, got %q", got)
	}

	cfg.Agents[AgentCoder] = Agent{Model: models.Claude4Sonnet}
	if !setDefaultModelForAgent(AgentSummarizer) {
		t.Fatal("summarizer should inherit the coder model")
	}
	if got := cfg.Agents[AgentSummarizer].Model; got != models.Claude4Sonnet {
		t.Fatalf("expected the coder model, got %q", got)
	}
}

// TestMigrateProvidersToAccountsSkipsCredentialLessProviders makes sure a
// provider section without any credential (what older config templates wrote for
// Anthropic) does not become a provider account.
func TestMigrateProvidersToAccountsSkipsCredentialLessProviders(t *testing.T) {
	isolateGlobalConfig(t)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	c := &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAnthropic: {UseOAuth: true},
			models.ProviderOpenAI:    {APIKey: "sk-openai"},
		},
	}
	migrateProvidersToAccounts(c)

	for _, acc := range c.ProviderAccounts {
		if acc.Type == models.ProviderAnthropic {
			t.Fatal("credential-less Anthropic section must not become an account")
		}
	}
	if len(c.ProviderAccounts) != 1 || c.ProviderAccounts[0].Type != models.ProviderOpenAI {
		t.Fatalf("expected only the OpenAI account, got %+v", c.ProviderAccounts)
	}
	if _, ok := c.Providers[models.ProviderAnthropic]; ok {
		t.Fatal("credential-less provider entry should be dropped from the legacy map")
	}
}

// TestDefaultConfigTemplateEnablesNoProvider guards the template against
// re-introducing an always-present provider section.
func TestDefaultConfigTemplateEnablesNoProvider(t *testing.T) {
	if containsUncommentedLine(DefaultConfigTemplate, "[Providers.anthropic]") {
		t.Fatal("DefaultConfigTemplate must not enable the Anthropic provider by default")
	}
}

func containsUncommentedLine(template, needle string) bool {
	for _, line := range strings.Split(template, "\n") {
		if strings.TrimSpace(line) == needle {
			return true
		}
	}
	return false
}
