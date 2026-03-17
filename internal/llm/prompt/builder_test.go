package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptDataDefaults(t *testing.T) {
	t.Parallel()
	data := &PromptData{}

	assert.Empty(t, data.AgentName)
	assert.Empty(t, data.WorkingDir)
	assert.False(t, data.IsGitRepo)
	assert.False(t, data.HasRemembrances)
	assert.False(t, data.HasOrchestration)
	assert.False(t, data.HasWebSearch)
	assert.False(t, data.HasCodeIndexing)
	assert.False(t, data.HasLSP)
	assert.False(t, data.HasSkills)
	assert.Nil(t, data.ContextFiles)
	assert.Nil(t, data.ActiveSkills)
}

func TestTemplateRegistryEmbedded(t *testing.T) {
	t.Parallel()
	registry := NewTemplateRegistry()

	expectedTemplates := []string{
		"base/identity", "base/environment", "base/conventions",
		"base/workflow", "base/tone", "base/tools_policy",
		"agents/coder", "agents/task", "agents/planner",
		"agents/explorer", "agents/title", "agents/summarizer",
		"providers/anthropic", "providers/openai", "providers/gemini", "providers/ollama",
		"capabilities/remembrances", "capabilities/orchestration",
		"capabilities/web_search", "capabilities/code_indexing", "capabilities/lsp",
		"context/git", "context/project", "context/skills", "context/mcp_instructions",
	}

	for _, name := range expectedTemplates {
		assert.True(t, registry.Exists(name), "expected template %q to exist", name)
	}
}

func TestTemplateRegistryNotExists(t *testing.T) {
	t.Parallel()
	registry := NewTemplateRegistry()
	assert.False(t, registry.Exists("nonexistent/template"))
}

func TestTemplateRegistryRenderIdentity(t *testing.T) {
	t.Parallel()
	registry := NewTemplateRegistry()

	data := &PromptData{
		AgentName:  "coder",
		WorkingDir: "/test/project",
		Platform:   "linux",
		Date:       "3/17/2026",
		Version:    "1.0.0",
	}

	result, err := registry.Render("base/identity", data)
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.True(t, strings.Contains(result, "Pando") || strings.Contains(result, "pando"),
		"identity should mention Pando")
}

func TestTemplateRegistryRenderEnvironment(t *testing.T) {
	t.Parallel()
	registry := NewTemplateRegistry()

	data := &PromptData{
		WorkingDir: "/test/project",
		IsGitRepo:  true,
		Platform:   "linux",
		Date:       "3/17/2026",
	}

	result, err := registry.Render("base/environment", data)
	require.NoError(t, err)
	assert.Contains(t, result, "/test/project")
	assert.Contains(t, result, "linux")
}

func TestTemplateRegistryRenderMissing(t *testing.T) {
	t.Parallel()
	registry := NewTemplateRegistry()
	_, err := registry.Render("nonexistent/template", &PromptData{})
	assert.Error(t, err)
}

func TestPromptBuilderBuild(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:  "coder",
		WorkingDir: "/test/project",
		IsGitRepo:  true,
		Platform:   "linux",
		Date:       "3/17/2026",
		Provider:   "anthropic",
	}

	builder := NewPromptBuilder("coder", "anthropic", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, result)
	assert.True(t, strings.Contains(result, "Pando") || strings.Contains(result, "pando"),
		"should contain Pando identity")
	assert.Contains(t, result, "/test/project", "should contain working directory")
}

func TestPromptBuilderCapabilitiesIncluded(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:       "coder",
		Provider:        "anthropic",
		HasRemembrances: true,
		WorkingDir:      "/test",
		Platform:        "linux",
		Date:            "3/17/2026",
	}
	builder := NewPromptBuilder("coder", "anthropic", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)

	// Should include some remembrances-related content
	hasRemContent := strings.Contains(result, "remembrances") ||
		strings.Contains(result, "Remembrances") ||
		strings.Contains(result, "Knowledge") ||
		strings.Contains(result, "kb_")
	assert.True(t, hasRemContent, "should include remembrances content when enabled")
}

func TestPromptBuilderCapabilitiesExcluded(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:       "coder",
		Provider:        "anthropic",
		HasRemembrances: false,
		WorkingDir:      "/test",
		Platform:        "linux",
		Date:            "3/17/2026",
	}
	builder := NewPromptBuilder("coder", "anthropic", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)

	assert.NotContains(t, result, "kb_search_documents",
		"should NOT include remembrances tool references when disabled")
}

func TestPromptBuilderDifferentAgents(t *testing.T) {
	t.Parallel()
	agents := []string{"coder", "task", "planner", "explorer", "title", "summarizer"}
	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			data := &PromptData{
				AgentName:  agent,
				Provider:   "anthropic",
				WorkingDir: "/test",
				Platform:   "linux",
				Date:       "3/17/2026",
			}
			builder := NewPromptBuilder(agent, "anthropic", data, nil)
			result, err := builder.Build(context.Background())
			require.NoError(t, err)
			assert.NotEmpty(t, result, "empty prompt for agent %s", agent)
		})
	}
}

func TestPromptBuilderDifferentProviders(t *testing.T) {
	t.Parallel()
	providers := []string{"anthropic", "openai", "gemini", "ollama"}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			data := &PromptData{
				AgentName:  "coder",
				Provider:   provider,
				WorkingDir: "/test",
				Platform:   "linux",
				Date:       "3/17/2026",
			}
			builder := NewPromptBuilder("coder", provider, data, nil)
			result, err := builder.Build(context.Background())
			require.NoError(t, err)
			assert.NotEmpty(t, result, "empty prompt for provider %s", provider)
		})
	}
}

func TestPromptBuilderOrchestrationCapability(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:        "coder",
		Provider:         "anthropic",
		HasOrchestration: true,
		WorkingDir:       "/test",
		Platform:         "linux",
		Date:             "3/17/2026",
	}
	builder := NewPromptBuilder("coder", "anthropic", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)

	hasOrchContent := strings.Contains(result, "orchestration") ||
		strings.Contains(result, "Orchestration") ||
		strings.Contains(result, "spawn_agent") ||
		strings.Contains(result, "sub-agent") ||
		strings.Contains(result, "mesnada")
	assert.True(t, hasOrchContent, "should include orchestration content when enabled")
}

func TestPromptBuilderGitContext(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:        "coder",
		Provider:         "anthropic",
		WorkingDir:       "/test",
		Platform:         "linux",
		Date:             "3/17/2026",
		IsGitRepo:        true,
		GitBranch:        "feature/test",
		GitStatus:        "M  file.go",
		GitRecentCommits: "abc1234 fix: something",
	}
	builder := NewPromptBuilder("coder", "anthropic", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)

	assert.Contains(t, result, "feature/test")
}

func TestPromptBuilderSkillsContext(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:      "coder",
		Provider:       "anthropic",
		WorkingDir:     "/test",
		Platform:       "linux",
		Date:           "3/17/2026",
		HasSkills:      true,
		SkillsMetadata: "- **sql**: Tune SQL queries",
		ActiveSkills:   []string{"Use EXPLAIN ANALYZE before rewriting."},
	}
	builder := NewPromptBuilder("coder", "anthropic", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)

	assert.Contains(t, result, "sql")
}

func TestPromptBuilderMCPInstructions(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:       "coder",
		Provider:        "anthropic",
		WorkingDir:      "/test",
		Platform:        "linux",
		Date:            "3/17/2026",
		MCPInstructions: "Use the remembrances server for project context.",
	}
	builder := NewPromptBuilder("coder", "anthropic", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)

	assert.Contains(t, result, "remembrances server")
}

func TestPromptBuilderNoProvider(t *testing.T) {
	t.Parallel()
	data := &PromptData{
		AgentName:  "coder",
		Provider:   "",
		WorkingDir: "/test",
		Platform:   "linux",
		Date:       "3/17/2026",
	}
	builder := NewPromptBuilder("coder", "", data, nil)
	result, err := builder.Build(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, result, "should still produce a prompt without provider")
}
