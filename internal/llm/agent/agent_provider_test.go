package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	model := models.Model{Provider: models.ProviderOpenAI, CanReason: true}

	if got := effectiveReasoningEffort(model, agentConfig, SessionLLMOverrides{ReasoningEffort: "high"}); got != "high" {
		t.Fatalf("effectiveReasoningEffort() override = %q, want %q", got, "high")
	}
	if got := effectiveReasoningEffort(model, agentConfig, SessionLLMOverrides{}); got != "low" {
		t.Fatalf("effectiveReasoningEffort() config fallback = %q, want %q", got, "low")
	}
	if got := effectiveReasoningEffort(model, config.Agent{}, SessionLLMOverrides{}); got != "medium" {
		t.Fatalf("effectiveReasoningEffort() empty fallback = %q, want the model default %q", got, "medium")
	}

	// A model that does not accept "medium" must resolve to a valid default
	// instead of the historical hardcoded "medium".
	noMedium := models.Model{Provider: models.ProviderAnthropic, APIModel: "claude-sonnet-4-5", CanReason: true, ReasoningEfforts: []string{"low", "high"}}
	if got := effectiveReasoningEffort(noMedium, config.Agent{}, SessionLLMOverrides{}); got != "high" {
		t.Fatalf("effectiveReasoningEffort() no-medium default = %q, want %q", got, "high")
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

func TestBuildSystemMessageUsesTemplatePromptBuilder(t *testing.T) {
	// config.Load is a process-wide singleton loader (it short-circuits with
	// "if cfg != nil { return cfg, nil }" and otherwise assigns the package
	// global internal/config.cfg). Left alone, this test would leak that
	// loaded *Config — read from the real $HOME/XDG config, not a fixture —
	// into every later test in this binary via config.Get(), including the
	// caveman-mode and tool-discovery tests that assert on the zero-value
	// (no configured) default. Isolate HOME so Load() cannot pick up a real
	// host config, and reset the global before and after via the same
	// config.IsolateForTests() seam internal/config's own tests use for this,
	// instead of only restoring the handful of fields this test happens to
	// mutate.
	config.IsolateForTests(t)

	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("Use AGENTS instructions"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "CLAUDE.md"), []byte("Use CLAUDE instructions"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0644))

	cfg, err := config.Load(tmpDir, false)
	require.NoError(t, err)

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

// TestBuildSystemMessageSessionSystemPromptReplacesBase: a session
// SystemPrompt replaces the built-in prompt and project context; persona
// content is still appended, and no override leaves the built-in prompt.
func TestBuildSystemMessageSessionSystemPromptReplacesBase(t *testing.T) {
	config.IsolateForTests(t)
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "AGENTS.md"), []byte("Use AGENTS instructions"), 0644))
	cfg, err := config.Load(tmpDir, false)
	require.NoError(t, err)
	cfg.WorkingDir = tmpDir
	cfg.ContextPaths = []string{"AGENTS.md"}

	const sid = "system-prompt-override-session"
	ctx := context.WithValue(context.Background(), prompt.SessionIDKey, sid)
	t.Cleanup(func() { SetSessionLLMOverrides(sid, SessionLLMOverrides{}) })

	build := func(persona string) string {
		return buildSystemMessage(ctx, config.AgentCoder, models.ProviderAnthropic, nil, nil, nil, persona)
	}

	baseline := build("")
	assert.Contains(t, baseline, "Use AGENTS instructions")

	SetSessionLLMOverrides(sid, SessionLLMOverrides{SystemPrompt: "You are a backlog assistant."})
	msg := build("PERSONA TEXT\n\nPROFILE PROMPT")
	assert.True(t, strings.HasPrefix(msg, "You are a backlog assistant."), msg)
	assert.NotContains(t, msg, "You are Pando, an interactive CLI tool")
	assert.NotContains(t, msg, "Use AGENTS instructions")
	assert.Contains(t, msg, "PERSONA TEXT\n\nPROFILE PROMPT")

	SetSessionLLMOverrides(sid, SessionLLMOverrides{})
	assert.Equal(t, baseline, build(""), "empty SystemPrompt must keep the built-in prompt byte-for-byte")
}
