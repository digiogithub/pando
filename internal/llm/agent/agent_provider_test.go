package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
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
