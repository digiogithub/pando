package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAnthropicThinkingMode(t *testing.T) {
	tests := []struct {
		name           string
		model          models.Model
		configuredMode config.ThinkingMode
		want           config.ThinkingMode
	}{
		{
			name: "defaults to medium for anthropic reasoning model",
			model: models.Model{
				Provider:  models.ProviderAnthropic,
				CanReason: true,
			},
			want: config.ThinkingMedium,
		},
		{
			name: "keeps explicit disabled mode for anthropic reasoning model",
			model: models.Model{
				Provider:  models.ProviderAnthropic,
				CanReason: true,
			},
			configuredMode: config.ThinkingDisabled,
			want:           config.ThinkingDisabled,
		},
		{
			name: "keeps explicit low mode for anthropic reasoning model",
			model: models.Model{
				Provider:  models.ProviderAnthropic,
				CanReason: true,
			},
			configuredMode: config.ThinkingLow,
			want:           config.ThinkingLow,
		},
		{
			name: "does not default non-reasoning anthropic model",
			model: models.Model{
				Provider:  models.ProviderAnthropic,
				CanReason: false,
			},
			want: "",
		},
		{
			name: "does not default non-anthropic model",
			model: models.Model{
				Provider:  models.ProviderOpenAI,
				CanReason: true,
			},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultAnthropicThinkingMode(tc.model, tc.configuredMode)
			if got != tc.want {
				t.Fatalf("defaultAnthropicThinkingMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffectiveReasoningEffortPrecedence(t *testing.T) {
	agentConfig := config.Agent{ReasoningEffort: "low"}

	if got := effectiveReasoningEffort(agentConfig, SessionLLMOverrides{ReasoningEffort: "high"}); got != "high" {
		t.Fatalf("effectiveReasoningEffort() override = %q, want %q", got, "high")
	}
	if got := effectiveReasoningEffort(agentConfig, SessionLLMOverrides{}); got != "low" {
		t.Fatalf("effectiveReasoningEffort() config fallback = %q, want %q", got, "low")
	}
	if got := effectiveReasoningEffort(config.Agent{}, SessionLLMOverrides{}); got != "" {
		t.Fatalf("effectiveReasoningEffort() empty fallback = %q, want empty", got)
	}
}

func TestEffectiveAnthropicThinkingModePrecedence(t *testing.T) {
	model := models.Model{Provider: models.ProviderAnthropic, CanReason: true}

	if got := effectiveAnthropicThinkingMode(model, config.Agent{ThinkingMode: config.ThinkingLow}, SessionLLMOverrides{ThinkingMode: config.ThinkingHigh}); got != config.ThinkingHigh {
		t.Fatalf("effectiveAnthropicThinkingMode() override = %q, want %q", got, config.ThinkingHigh)
	}
	if got := effectiveAnthropicThinkingMode(model, config.Agent{ThinkingMode: config.ThinkingDisabled}, SessionLLMOverrides{}); got != config.ThinkingDisabled {
		t.Fatalf("effectiveAnthropicThinkingMode() config fallback = %q, want %q", got, config.ThinkingDisabled)
	}
	if got := effectiveAnthropicThinkingMode(model, config.Agent{}, SessionLLMOverrides{}); got != config.ThinkingMedium {
		t.Fatalf("effectiveAnthropicThinkingMode() default fallback = %q, want %q", got, config.ThinkingMedium)
	}
}

func TestSetSessionLLMOverridesNormalizesAndClears(t *testing.T) {
	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, "session-1")

	SetSessionLLMOverrides("session-1", SessionLLMOverrides{
		ReasoningEffort: " HIGH ",
		ThinkingMode:    config.ThinkingMode(" Disabled "),
	})
	t.Cleanup(func() {
		SetSessionLLMOverrides("session-1", SessionLLMOverrides{})
	})

	got := sessionLLMOverridesForContext(ctx)
	if got.ReasoningEffort != "high" || got.ThinkingMode != config.ThinkingDisabled {
		t.Fatalf("sessionLLMOverridesForContext() = %#v, want normalized high/disabled", got)
	}

	SetSessionLLMOverrides("session-1", SessionLLMOverrides{})
	if got := sessionLLMOverridesForContext(ctx); got != (SessionLLMOverrides{}) {
		t.Fatalf("sessionLLMOverridesForContext() after clear = %#v, want empty", got)
	}
}

func setTestConfig(c *config.Config) {
	config.SetForTests(c)
}

type fakePromptTool struct {
	name string
}

func (t fakePromptTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: t.name}
}

func (t fakePromptTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse(""), nil
}

func TestCreateAgentProviderAllowsLogicalAntigravityModelWithoutConcreteAccount(t *testing.T) {
	oldCfg := config.Get()
	t.Cleanup(func() {
		setTestConfig(oldCfg)
	})

	setTestConfig(&config.Config{
		WorkingDir: t.TempDir(),
		Agents: map[config.AgentName]config.Agent{
			config.AgentCoder: {Model: models.AntigravityGemini30Pro},
		},
		ProviderAccounts: []config.ProviderAccount{{
			ID:                "ag-1",
			Type:              models.ProviderAntigravity,
			Email:             "user@example.com",
			OAuthRefreshToken: "refresh-token",
			OAuthAccessToken:  "access-token",
			OAuthExpiry:       4102444800,
		}},
		Providers: map[models.ModelProvider]config.Provider{},
		LSP:       make(map[string]config.LSPConfig),
	})

	p, err := createAgentProvider(context.Background(), config.AgentCoder, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, models.AntigravityGemini30Pro, p.Model().ID)
	assert.Equal(t, models.ProviderAntigravity, p.Model().Provider)
}

func TestBuildSystemMessageUsesTemplatePromptBuilder(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("Use AGENTS instructions"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("Use CLAUDE instructions"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0644))

	cfg, err := config.Load(tmpDir, false)
	require.NoError(t, err)

	oldWorkingDir := cfg.WorkingDir
	oldContextPaths := append([]string(nil), cfg.ContextPaths...)
	oldMCPServers := make(map[string]config.MCPServer, len(cfg.MCPServers))
	for name, server := range cfg.MCPServers {
		oldMCPServers[name] = server
	}
	t.Cleanup(func() {
		cfg.WorkingDir = oldWorkingDir
		cfg.ContextPaths = oldContextPaths
		cfg.MCPServers = oldMCPServers
	})

	cfg.WorkingDir = tmpDir
	cfg.ContextPaths = []string{"AGENTS.md", "CLAUDE.md"}
	cfg.MCPServers = map[string]config.MCPServer{
		"mesnada":      {},
		"remembrances": {},
	}

	msg := buildSystemMessage(
		context.Background(),
		config.AgentCoder,
		models.ProviderAnthropic,
		[]tools.BaseTool{
			fakePromptTool{name: "kb_search_documents"},
			fakePromptTool{name: "mesnada_spawn_agent"},
		},
		nil,
		nil,
		"",
	)

	assert.Contains(t, msg, "Use AGENTS instructions")
	assert.NotContains(t, msg, "Use CLAUDE instructions")
	assert.Contains(t, msg, "# Knowledge Management")
	assert.Contains(t, msg, "# Agent Orchestration")
	assert.NotContains(t, msg, "There are more than 1000 files")
	assert.NotContains(t, msg, "<project>")
}

func TestBuildSystemMessageCleanModeReturnsEmptyPrompt(t *testing.T) {
	ctx := context.WithValue(context.Background(), cleanModeContextKey{}, true)
	msg := buildSystemMessage(
		ctx,
		config.AgentCoder,
		models.ProviderAnthropic,
		nil,
		nil,
		nil,
		"persona",
	)

	assert.Equal(t, "", msg)
}

func TestCleanModeCatalogToolHasExpectedContract(t *testing.T) {
	info := cleanModeCatalogTool{}.Info()
	assert.Equal(t, "list_mcp_catalog", info.Name)
	assert.Empty(t, info.Required)
	assert.Empty(t, info.Parameters)
}
