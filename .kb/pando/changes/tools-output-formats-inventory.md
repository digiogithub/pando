---
created_at: 2026-06-22T00:00:00Z
updated_at: 2026-06-22T00:00:00Z
tags:
  - changes
  - tools
  - serialization
  - toon
  - toml
  - json
---
# Pando tools output format inventory

## Goal
Document the actual output format used by Pando tools and related helpers, with emphasis on whether the response is TOON, JSON, TOML, or another representation.

## Scope
This inventory covers the tool implementations currently observed in the codebase, especially `internal/llm/tools/*`, `internal/format/format.go`, `internal/llm/agent/mcp-tools.go`, and `internal/mcpgateway/proxy_tools.go`.

## Findings

### Shared structured response path
Most built-in tools that return structured data call `tools.NewStructuredResponse(...)`.
That path is implemented in `internal/llm/tools/json_output.go` and behaves as follows:

- attempts to render the value with `toml.Marshal(...)`
- if TOML encoding succeeds, the response content is **TOML** text
- if TOML encoding fails, it falls back to **pretty-printed JSON** via `json.MarshalIndent(...)`

This means the code does **not** produce TOON in the strict sense; it produces TOML-first structured text with JSON fallback.

### JSON-like text normalization
`tools.FormatJSONLikeContent(...)` also uses the same structured rendering path:

- parses incoming text as JSON when possible
- re-renders it through the TOML-first structured formatter
- falls back to indented JSON only if TOML cannot represent the value

### Plain JSON responses
Some tools still emit plain JSON-formatted text directly, usually for compatibility or because the helper explicitly wraps it:

- `internal/llm/tools/fetch.go` → `formatJSONResponse(...)` returns a fenced ```json code block with indented JSON
- some error paths return plain text error strings via `NewTextErrorResponse(...)`

### Mixed output tools
A few tools return different formats depending on the result shape:

- `mcp_call_tool` / `internal/mcpgateway/proxy_tools.go`
  - if the proxied tool returns a string, it passes through `FormatJSONLikeContent(...)`
  - otherwise it uses `NewStructuredResponse(...)`
- browser tools and KB/Mesnada tools mostly return `NewStructuredResponse(...)` for success and plain text for errors
- `browser_pdf` returns base64 text when generating a PDF inline, but structured data when the PDF is saved to disk

## Inventory by file / tool family

### `internal/llm/tools/json_output.go`
- `NewStructuredResponse(value any)` → **TOML first**, fallback **JSON**
- `FormatStructuredData(value any)` → **TOML first**, fallback **JSON**
- `FormatJSONLikeContent(content string)` → **TOML first**, fallback **JSON**
- `tryFormatRawJSONAsStructured(...)` → same behavior

### `internal/format/format.go`
- `FormatOutput(content, "json")` → structured **TOML** when possible, otherwise **JSON**
- `GetHelpText()` describes this as structured output with JSON fallback

### `internal/llm/tools/fetch.go`
- `formatJSONResponse(body []byte)` → fenced **JSON** code block
- successful HTML conversions are **Markdown**
- plain text responses remain **plain text**

### Browser tools (`internal/llm/tools/browser_*.go`)
- `browser_navigate` → structured **TOML-first** output via `NewStructuredResponse`
- `browser_click`, `browser_fill`, `browser_scroll`, `browser_console_logs`, `browser_network`, `browser_pdf` (saved-to-file mode) → structured **TOML-first** output via `NewStructuredResponse`
- `browser_pdf` inline output → **base64 text**
- error paths → **plain text** error messages

### Mesnada tools (`internal/llm/tools/mesnada.go`)
- structured task results → **TOML-first** output via `NewStructuredResponse`
- error paths → **plain text**

### KB / remembrances tools (`internal/llm/tools/remembrances_*.go`)
- search/get/import/delete/list/recall/hybrid tools mostly return **TOML-first structured output** via `NewStructuredResponse`
- no dedicated TOON encoder is present
- empty / not-found cases may return **plain text**

### MCP gateway helpers
- `internal/mcpgateway/proxy_tools.go`
  - catalog results → **TOML-first** structured output
  - proxied tool string outputs → normalized with `FormatJSONLikeContent(...)` (**TOML-first**, fallback **JSON**)

### Agent MCP wrapper
- `internal/llm/agent/mcp-tools.go`
  - raw MCP tool string output → `FormatJSONLikeContent(...)` (**TOML-first**, fallback **JSON**)

## Bottom line
If a tool returns structured data through the shared helper, the effective output format is:

1. **TOML** when the value can be marshaled
2. **pretty JSON** when TOML marshaling fails
3. **plain text** or **Markdown** for specific helpers and error paths
4. **base64 text** for inline PDF output

There is no evidence in the implementation of a dedicated TOON encoder. The codebase uses the word TOON in comments and tests, but the actual serializer in use is **TOML**.

## Verification
- Reviewed the structured response helper in `internal/llm/tools/json_output.go`
- Reviewed `internal/format/format.go`
- Traced the main tool families that call `NewStructuredResponse(...)` or `FormatJSONLikeContent(...)`
- Confirmed `fetch.go` uses fenced JSON for JSON responses
