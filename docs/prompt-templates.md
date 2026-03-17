# Pando Prompt Template System

## Overview

Pando uses a modular, composable template system to generate system prompts for AI models. Templates are composed from multiple sections based on the agent type, LLM provider, and available capabilities. The system is designed to be extensible via Lua hooks and external template overrides.

## Architecture

### Composition Pipeline

The system prompt is built through a layered pipeline (see `internal/llm/prompt/builder.go`):

1. **Identity** (`base/identity.md.tpl`) — Core identity statement ("You are Pando...")
2. **Provider** (`providers/{name}.md.tpl`) — Model-specific behavioral guidelines
3. **Agent** (`agents/{name}.md.tpl`) — Agent-specific instructions and workflow
4. **Base Sections** (`base/`) — Shared sections (environment, conventions, workflow, tone, tools)
5. **Capabilities** (`capabilities/`) — Conditional sections based on available tools
6. **Context** (`context/`) — Dynamic session context (git, project files, skills, MCP)

Each layer is rendered independently and can be modified via Lua hooks before final assembly.

### Key Files

| File | Purpose |
|------|---------|
| `internal/llm/prompt/builder.go` | Template composition engine |
| `internal/llm/prompt/registry.go` | Template loading, caching, and rendering |
| `internal/llm/prompt/data.go` | PromptData struct with all template variables |
| `internal/llm/prompt/capabilities.go` | Runtime capability detection |
| `internal/llm/prompt/luafuncs.go` | Lua helper functions bridge |
| `internal/llm/prompt/prompt.go` | Entry points (GetAgentPrompt, BuildPrompt) |

## Template Files

All templates use Go's `text/template` syntax and are embedded from `internal/llm/prompt/templates/`.

### Base Templates (shared across agents)

| File | Description |
|------|-------------|
| `base/identity.md.tpl` | Core identity ("You are Pando...") |
| `base/environment.md.tpl` | Working directory, platform, date, project listing |
| `base/conventions.md.tpl` | Code style and convention rules |
| `base/workflow.md.tpl` | Before/while/after acting guidelines |
| `base/tone.md.tpl` | Response style and brevity examples |
| `base/tools_policy.md.tpl` | Tool usage rules and best practices |

### Agent Templates

| File | Description |
|------|-------------|
| `agents/coder.md.tpl` | Main coding agent (default) — memory, proactiveness, code style |
| `agents/task.md.tpl` | Sub-agent for delegated tasks — concise, focused |
| `agents/planner.md.tpl` | Read-only planning mode — analysis without modifications |
| `agents/explorer.md.tpl` | Fast codebase exploration — search and read only |
| `agents/title.md.tpl` | Session title generation |
| `agents/summarizer.md.tpl` | Conversation summarization |

### Provider Templates

| File | Description |
|------|-------------|
| `providers/anthropic.md.tpl` | Optimized for Claude models — XML tags, thinking patterns, brevity |
| `providers/openai.md.tpl` | Optimized for GPT models — directive autonomy style |
| `providers/gemini.md.tpl` | Optimized for Gemini — step-by-step, markdown emphasis |
| `providers/ollama.md.tpl` | Optimized for local models — compact, simplified |

### Capability Templates (Conditional)

These are only included when the corresponding capability is detected at runtime.

| File | Condition | Description |
|------|-----------|-------------|
| `capabilities/remembrances.md.tpl` | remembrances MCP server | KB, code intelligence, events |
| `capabilities/orchestration.md.tpl` | mesnada MCP server | Sub-agent spawning and parallelization |
| `capabilities/web_search.md.tpl` | search tools available | Web search guidelines |
| `capabilities/code_indexing.md.tpl` | code search tools | Semantic code search |
| `capabilities/lsp.md.tpl` | LSP servers configured | Diagnostics integration |

### Context Templates

| File | Description |
|------|-------------|
| `context/git.md.tpl` | Git branch, status, recent commits |
| `context/project.md.tpl` | Project instruction files (PANDO.md, etc.) |
| `context/skills.md.tpl` | Available and active skills |
| `context/mcp_instructions.md.tpl` | MCP server instructions |

## Template Variables (PromptData)

All templates have access to the `PromptData` struct:

| Field | Type | Description |
|-------|------|-------------|
| `AgentName` | string | Current agent name |
| `AgentRole` | string | Agent role description |
| `Version` | string | Pando version |
| `WorkingDir` | string | Current working directory |
| `IsGitRepo` | bool | Whether in a git repository |
| `Platform` | string | Operating system |
| `Date` | string | Today's date |
| `GitBranch` | string | Current git branch |
| `GitStatus` | string | Git status output |
| `GitRecentCommits` | string | Recent commit log |
| `ProjectListing` | string | Project directory listing |
| `Provider` | string | LLM provider name |
| `Model` | string | Model name |
| `HasRemembrances` | bool | Remembrances tools available |
| `HasOrchestration` | bool | Mesnada orchestration available |
| `HasWebSearch` | bool | Web search tools available |
| `HasGoogleSearch` | bool | Google search specifically |
| `HasBraveSearch` | bool | Brave search specifically |
| `HasPerplexity` | bool | Perplexity search specifically |
| `HasCodeIndexing` | bool | Code indexing tools available |
| `HasLSP` | bool | LSP diagnostics available |
| `HasSkills` | bool | Skills system available |
| `ContextFiles` | []ContextFile | Loaded project context files |
| `SkillsMetadata` | string | Available skills listing |
| `ActiveSkills` | []string | Currently active skill instructions |
| `LSPInfo` | string | LSP configuration info |
| `MCPInstructions` | string | MCP server instructions |

## Template Functions

| Function | Description |
|----------|-------------|
| `trimSpace` | Trim whitespace from string |
| `join` | Join string slice with separator |
| `contains` | Check if string contains substring |
| `lower` | Lowercase string |
| `upper` | Uppercase string |
| `hasPrefix` | Check string prefix |
| `hasSuffix` | Check string suffix |
| `default` | Return default if value is empty |
| `notEmpty` | Check if string is not empty |

## Customization

### Override Templates

Place custom template files in one of these directories (checked in order):

1. `.pando/templates/` (project-level)
2. `~/.config/pando/templates/` (user-level)

Use the same directory structure as the embedded templates. Custom templates completely replace embedded ones for the same path.

**Example:** Override the workflow for your project:
```
.pando/templates/base/workflow.md.tpl
```

### Lua Hooks

Use Lua hooks for dynamic, programmatic template modification without replacing entire files. See [lua-hooks-prompts.md](lua-hooks-prompts.md) for details.

## Using BuildPrompt

The `BuildPrompt` function is the recommended entry point:

```go
prompt, err := prompt.BuildPrompt(ctx, config.AgentCoder, models.ProviderAnthropic, luaMgr,
    prompt.WithEnvironment(workDir, isGitRepo, platform, date),
    prompt.WithGitInfo(branch, status, commits),
    prompt.WithMCPServers(serverNames),
    prompt.WithTools(toolNames),
    prompt.WithSkills(metadata, activeSkills),
    prompt.WithVersion("1.0.0"),
    prompt.WithModel("claude-sonnet-4-6"),
)
```

Capability detection is automatic based on MCP server names and tool names.

## Backward Compatibility

The legacy `GetAgentPrompt` function remains available and uses hardcoded prompts. It will be gradually replaced by `BuildPrompt` across the codebase.
