---
created_at: 2026-06-09T21:38:39.932177613Z
updated_at: 2026-06-09T21:38:39.932177613Z
tags:
    - pando
    - plan
    - tools
    - mcp
    - anthropic
    - openai
    - tool-search
---
# Tool Discovery, Semantic Tool Search, and Tool Aliasing Plan for Anthropic and GPT Providers

## Context

The Copilot analysis in `analysis/copilot-tools-optimization-and-renaming-analysis.md` describes three mechanisms worth adapting to Pando:

1. Deferred tools: keep only core tools visible initially and expose a `tool_search` capability for discovering deferred tools by natural-language query.
2. Semantic tool search: rank available tools using semantic/FTS matching instead of sending every schema to every model request.
3. Friendly aliases: allow stable, human-readable tool references while preserving canonical server/tool identifiers for execution.

Pando already has useful foundations:

- `internal/llm/tools/tools.go` defines the common `BaseTool` and `ToolInfo` contract.
- `internal/llm/agent/tools.go` assembles coder/task tool sets and already includes a `ContextTrimmer` hook for task-based tool filtering.
- `internal/llm/agent/mcp-tools.go` exposes direct MCP tools and has a gateway-aware path via `GetMcpToolsWithGateway`.
- `internal/mcpgateway` stores discovered MCP tools and already exposes `mcp_query_catalog` plus `mcp_call_tool`.
- `internal/llm/provider/anthropic.go`, `internal/llm/provider/openai.go`, and `internal/llm/provider/copilot.go` convert `BaseTool` to provider-specific tool schemas.
- The RAG stack already has FTS/vector search building blocks that can be reused for semantic tool lookup.

## Design Goals

- Reduce request token usage when MCP/internal tool count is high.
- Preserve existing behavior for providers/models without dynamic discovery support.
- Implement discovery once in the shared agent/tool layer and keep provider-specific code thin.
- Support Anthropic and OpenAI/GPT first; avoid coupling the feature to Copilot-only Responses API details.
- Keep tool execution canonical and permission-aware.
- Make aliasing deterministic and testable.

## Proposed Architecture

### 1. Tool metadata extension

Extend the shared tool model without breaking existing tools:

- Add optional metadata through a new interface, not by changing every `BaseTool` implementation immediately:
  - `ToolMetadataProvider` with `ToolMetadata() ToolMetadata`.
  - `ToolMetadata` fields: `CanonicalName`, `Aliases`, `ServerName`, `ToolName`, `Source`, `Category`, `NonDeferred`, `Dynamic`, `Priority`, `ProviderHints`.
- Keep `ToolInfo.Name` as the callable API name for backward compatibility.
- Implement wrappers for alias/canonical mapping rather than modifying every concrete tool.

### 2. Tool registry service

Introduce a shared runtime registry package, for example `internal/llm/tooldiscovery`:

- `Registry` stores all available `BaseTool` values, canonical names, aliases, and provider visibility state.
- It indexes names, descriptions, source/server labels, and schema summaries.
- It resolves aliases to canonical callable names before execution.
- It tracks per-session discovered/deferred tools so the agent can expand the visible set after `tool_search` results.

This registry should sit between agent tool assembly and provider request conversion.

### 3. Semantic search implementation

Start with local FTS/BM25 plus lightweight lexical scoring because it is deterministic and does not require network credentials. Then add embeddings behind a config flag:

- Phase 1 ranking inputs: name, alias, server, description, parameter names, required parameters.
- Phase 2 ranking inputs: optional embeddings using the existing RAG embedding providers.
- Reuse `internal/mcpgateway.Registry` data for MCP tools, but generalize the search across MCP, internal tools, Lua tools, Mesnada tools, and remembrance tools.

### 4. `tool_search` tool

Add a built-in `tool_search` tool in the shared tools layer:

- Input: `query`, optional `limit`, optional `include_schemas`, optional `source`/`server` filters.
- Output: compact JSON array with canonical name, aliases, description, server/source, and schema when needed.
- Side effect: mark returned tools as discovered for the current session so subsequent model turns can receive those tool schemas directly if the provider supports turn-by-turn tool updates.

For providers without native deferred loading, `tool_search` can still return canonical names and the agent loop can expose newly discovered direct tools on the next request.

### 5. Deferred tool selection

Add a `ToolSelectionPolicy` used by `CoderAgentToolsWithMesnada` and provider request preparation:

- Always visible: core file/edit/search/cache/shell/todo tools, `tool_search`, `mcp_query_catalog`, `mcp_call_tool`, and any configured favorites.
- Deferred by default: most MCP tools, low-frequency internal integrations, Lua tools unless marked non-deferred, and large schema tools.
- Threshold behavior: if total tools exceed `MaxDirectTools`, activate discovery mode automatically.
- Configurable policy under a new config section, likely `[ToolDiscovery]`.

### 6. Provider integration

Anthropic:

- Use the shared selected tool set in `anthropicClient.convertTools`.
- Keep cache control only on the final visible tool after filtering.
- Add provider prompt instructions using the prompt template system for Claude-family models.
- If Anthropic SDK support for server-side deferred loading is unavailable in current Go SDK, implement Pando-managed deferred loading at the agent loop level.

OpenAI/GPT:

- Use the same selected tool set in `openaiClient.convertTools`.
- For Chat Completions, implement Pando-managed deferred loading across turns.
- For future Responses API support in the OpenAI provider, map `tool_search` outputs to provider-native tool reference behavior where available.

Copilot:

- Leave the existing Responses path compatible.
- Later, converge Copilot-specific request shaping with the shared discovery registry so behavior is not duplicated.

### 7. Prompt/template updates

Add a new capability template:

- `internal/llm/prompt/templates/capabilities/tool_discovery.md.tpl`

Include provider-specific variants or conditional language for:

- Anthropic/Claude: XML-style instruction block matching existing Anthropic template style.
- OpenAI/GPT: concise directive style matching existing OpenAI templates.

Prompt rules should explain:

- Use `tool_search` before calling deferred tools.
- Use broad semantic queries.
- Do not repeatedly retry failed searches.
- Use canonical tool names returned by `tool_search`.
- Dynamic MCP tools may appear after calls that modify server capabilities.

### 8. Aliasing and friendly names

Implement aliasing in two layers:

- Registry-level alias resolution for all tools.
- MCP-specific friendly server mapping for `server/tool`, display labels, normalized names, and gateway names.

Rules:

- Canonical ID for MCP tools: `server_name/tool_name` in registry and gateway.
- Callable name for direct provider tools remains provider-safe, likely `server_tool` or a sanitized alias.
- User-facing references may use `server/tool`, display server name, or configured aliases.
- Alias collisions must be explicit errors or resolved by server qualification.

### 9. Configuration

Add `[ToolDiscovery]` with conservative defaults:

```toml
[ToolDiscovery]
Enabled = true
Mode = "auto" # auto, always, off
MaxDirectTools = 64
SearchLimit = 8
SemanticSearch = true
EmbeddingSearch = false
NonDeferredTools = ["bash", "edit", "view", "glob", "grep", "write", "patch", "ls", "cache_read", "cache_stats", "todo_write", "tool_search"]
DeferredSources = ["mcp", "lua", "internal_optional"]
AliasStrict = true
```

Update config structs, defaults, schema generator, and init template.

## Implementation Phases

### Phase 1: Shared registry and alias resolution

- Create `internal/llm/tooldiscovery` with registry, metadata, alias map, and selection policy.
- Add unit tests for canonicalization, alias collision handling, and provider-safe names.
- Wrap MCP tools with metadata from `internal/llm/agent/mcp-tools.go`.
- Do not change provider behavior yet except through no-op registry construction.

### Phase 2: `tool_search` and lexical semantic search

- Add `NewToolSearchTool(registry, sessionState)`.
- Implement FTS/lexical ranking over all registered tool descriptions.
- Add session-level discovered tool state keyed by session/message context.
- Add tests under `tests/` for CLI-facing behavior if applicable and Go tests for package behavior.

### Phase 3: Agent assembly and deferred selection

- Wire `ToolSelectionPolicy` into `CoderAgentToolsWithMesnada`.
- Preserve existing `ContextTrimmer` behavior, but make it operate before/with discovery policy.
- Ensure always-included tools remain visible.
- Add threshold mode for large tool lists.

### Phase 4: Anthropic and OpenAI/GPT provider support

- Update `anthropicClient.convertTools` and `openaiClient.convertTools` call sites to receive the already-selected visible tool set.
- Add prompt capability detection and provider-specific instructions.
- Add tests proving hidden deferred tools are not sent initially and become visible after `tool_search` discovery.

### Phase 5: Gateway semantic upgrade

- Replace `internal/mcpgateway.Registry.SearchTools` LIKE search with FTS5 ranking.
- Optionally add embeddings using existing RAG embedders behind config.
- Keep `mcp_query_catalog` as a backward-compatible gateway search tool, but make `tool_search` the general model-facing discovery tool.

### Phase 6: Configuration, docs, and schema

- Add `[ToolDiscovery]` to config structs, init template, schema generator, settings UI if needed.
- Document aliasing and discovery behavior.
- Add migration notes for users with many MCP tools.

## Testing Strategy

Run focused Go tests first:

- `go test ./internal/llm/agent ./internal/api`
- Add new package tests for `internal/llm/tooldiscovery`.
- Add provider conversion tests for Anthropic and OpenAI.
- Add gateway registry FTS tests once search changes.

Add Python tests under `tests/` only for CLI/config/schema behavior.

## Risks

- Provider-native deferred loading differs between Anthropic, OpenAI Chat Completions, OpenAI Responses, and Copilot Responses. The first implementation should be Pando-managed and provider-agnostic.
- Tool alias collisions can make execution ambiguous; strict mode should be the default.
- Persisted gateway registry and dynamic MCP list changes need invalidation/re-discovery logic.
- Search quality without embeddings may be acceptable initially but should expose enough schema/description context for reliable discovery.

## Acceptance Criteria

- When configured tool count exceeds the threshold, initial Anthropic/OpenAI requests include only core tools plus `tool_search` and selected favorites.
- `tool_search` returns relevant tool names and schemas using semantic/FTS search.
- Deferred MCP tools can be invoked after discovery without changing their permission checks.
- Friendly aliases resolve to canonical tools deterministically.
- Existing non-discovery configurations behave as before.
- Tests cover registry aliasing, search, selection policy, and Anthropic/OpenAI provider conversion behavior.
