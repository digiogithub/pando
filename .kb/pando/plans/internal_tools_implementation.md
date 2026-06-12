# Plan: Internal Tools Implementation for Pando

## Objective
Implement as Pando's native internal tools the following functionalities:
1. **md-fetch** (Go): fetch URLs with HTML→Markdown conversion and JSON detection
2. **Google Search** (hyper-mcp plugin Rust): search via Google Custom Search API
3. **Brave Search** (hyper-mcp plugin Rust): search via Brave Search API
4. **Context7** (hyper-mcp plugin Rust): library ID resolution and docs retrieval
5. **Perplexity Search** (hyper-mcp plugin Rust): AI-powered search with citations

## Source Analysis

### md-fetch (/www/MCP/Pando/md-fetch)
- **Go Architecture**: Fetcher → Browser (Chrome/Firefox/Lynx/w3m/Curl) → Converter (HTML→Markdown)
- **Type detection**: HTML → Markdown, JSON → pretty-printed ```json block, Plaintext → text
- **Pando already has** `fetch.go` with HTML→Markdown but without JSON detection
- **Enhancement to incorporate**: JSON detection by Content-Type and by content (starts with { or [)

### Google Search (/www/MCP/hyper-mcp/examples/plugins/google-search/src/lib.rs)
- **API**: GET https://www.googleapis.com/customsearch/v1
- **Required config**: GOOGLE_API_KEY, GOOGLE_SEARCH_ENGINE_ID
- **Params**: query, num(1-10), start(1-91), safe, lr, gl, cr, date_restrict, site_search, search_type
- **Raw response**: JSON with searchInformation, items (title, link, displayLink, snippet), spelling

### Brave Search (/www/MCP/hyper-mcp/examples/plugins/brave-search/src/lib.rs)
- **API**: GET https://api.search.brave.com/res/v1/web/search
- **Auth**: Header X-Subscription-Token: API_KEY
- **Required config**: BRAVE_API_KEY
- **Params**: query, count(1-20), offset, country, search_lang, ui_lang, safesearch, freshness, result_filter
- **Raw response**: JSON with query, web.results (title, url, description, page_age), discussions

### Context7 (/www/MCP/hyper-mcp/examples/plugins/context7/src/lib.rs)
- **API**: https://context7.com/api (no API key required)
- **Header**: X-Context7-Source: mcp-server
- **Tool 1** `c7_resolve_library_id`: GET /v1/search?query={name} → results[]{title, id, description, totalSnippets, stars}
- **Tool 2** `c7_get_library_docs`: GET /v1/{lib_id}/?context7CompatibleLibraryID={id}&topic={t}&tokens={n}
- **Docs response**: Direct Markdown

### Perplexity Search (/www/MCP/hyper-mcp/examples/plugins/perplexity-search/src/lib.rs)
- **API**: POST https://api.perplexity.ai/chat/completions
- **Auth**: Bearer token
- **Required config**: PERPLEXITY_API_KEY
- **Params**: query, model(sonar-pro|sonar-reasoning|sonar-deep-research), system_message, max_tokens, temperature, search_recency_filter, return_citations, return_images, return_related_questions
- **Raw response**: OpenAI-compatible chat response with citations[] and search_results[]

## Architecture in Pando

### Existing tool pattern
- `BaseTool` interface with `Info() ToolInfo` and `Run(ctx, ToolCall) (ToolResponse, error)`
- Registration in `internal/llm/agent/tools.go` → `CoderAgentTools()` and `CoderAgentToolsWithMesnada()`
- Config in `internal/config/config.go` using Viper (JSON + TOML + env vars)
- TUI settings in `internal/tui/page/settings.go` → `buildSections()` → new section

## Implementation Phases

### Phase 1: Config Infrastructure (fact key: internal_tools_plan_phase1)
- Add `InternalToolsConfig` struct to config.go
- Add `InternalTools InternalToolsConfig` to `Config` struct
- Bind env vars in `setProviderDefaults()`: PANDO_GOOGLE_API_KEY, GOOGLE_API_KEY, BRAVE_API_KEY, PERPLEXITY_API_KEY, PANDO_GOOGLE_SEARCH_ENGINE_ID
- Add defaults in `setDefaults()`
- Add `buildInternalToolsSection()` in settings.go with: toggles per tool, API keys (masked), Search Engine ID

### Phase 2: Enhanced Fetch Tool (fact key: internal_tools_plan_phase2)
- Modify `internal/llm/tools/fetch.go`
- Add "auto" and "json" formats
- JSON detection: by Content-Type (application/json) and by content (starts { or [)
- Pretty-print JSON → ```json ... ``` code block
- Use InternalTools.FetchMaxSizeMB for size limit

### Phase 3: Search Tools (fact key: internal_tools_plan_phase3)
- Create `internal/llm/tools/search_google.go`
- Create `internal/llm/tools/search_brave.go`
- Create `internal/llm/tools/search_perplexity.go`
- Structured Markdown responses (improvement vs original plugins that return plain text)
- HTTP/JSON errors → return in readable code block
- Use permission.Service for approval (action: "web_search")
- Only register if API key is configured

### Phase 4: Context7 Tool (fact key: internal_tools_plan_phase4)
- Create `internal/llm/tools/context7.go`
- Two structs: `context7ResolveTool` and `context7DocsTool`
- Constructor `NewContext7Tools() []BaseTool`
- Only register if Context7Enabled=true
- No API key required

### Phase 5: Registration & Integration (fact key: internal_tools_plan_phase5)
- Modify `internal/llm/agent/tools.go`
- Conditional registration based on config
- JSON and TOML example configs ready to copy
- Document supported env vars

### Phase 6: Tests (fact key: internal_tools_plan_phase6)
- Tests in `tests/` (Python)
- Mock HTTP for external APIs
- Coverage: config loading, JSON detection, each tool with required and optional parameters

## Pando Improvements vs Original Plugins
1. **Structured Markdown responses** with headers, bold, etc. (original plugins return plain text)
2. **Pretty-printed JSON responses** in code blocks when the API returns JSON
3. **Integrated config** in Pando's config system (unified JSON/TOML/env vars)
4. **TUI panel** to manage API keys and toggles without editing files
5. **Permission system** integrated with Pando's permission system
6. **Context cancellation** respected in all HTTP requests

## Files to Create/Modify
| File | Action |
|---------|--------|
| internal/config/config.go | Modify: add InternalToolsConfig |
| internal/tui/page/settings.go | Modify: add buildInternalToolsSection |
| internal/llm/tools/fetch.go | Modify: JSON detection + auto format |
| internal/llm/tools/search_google.go | NEW |
| internal/llm/tools/search_brave.go | NEW |
| internal/llm/tools/search_perplexity.go | NEW |
| internal/llm/tools/context7.go | NEW |
| internal/llm/agent/tools.go | Modify: conditional registration |
| tests/test_internal_tools_*.py | NEW (6 test files) |
