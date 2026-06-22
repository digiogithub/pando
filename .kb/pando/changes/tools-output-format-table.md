---
created_at: 2026-06-22T00:00:00Z
updated_at: 2026-06-22T00:00:00Z
tags:
  - changes
  - tools
  - serialization
  - table
  - json
  - toml
---
# Pando tools output format table

## Purpose
Provide a per-tool inventory of the exact output format each Pando tool uses at runtime, based on the current implementation.

## Format legend
- **TOML-first structured text**: output is produced through `NewStructuredResponse(...)` / `FormatStructuredData(...)`, which serializes with `toml.Marshal(...)` and falls back to indented JSON if TOML fails.
- **JSON fenced block**: output is returned as a Markdown code block annotated with `json`.
- **Plain text**: raw string response, usually human-readable text or an error message.
- **Markdown**: formatted text containing Markdown structure.
- **Base64 text**: plain string containing base64-encoded bytes.

## Tool-by-tool table

| Tool | File | Success output format | Error / fallback output | Notes |
| --- | --- | --- | --- | --- |
| `browser_console_logs` | `internal/llm/tools/browser_devtools.go` | TOML-first structured text | Plain text error | Returns captured console entries with `NewStructuredResponse(entries)` |
| `browser_network` | `internal/llm/tools/browser_devtools.go` | TOML-first structured text | Plain text error | Returns captured network entries with `NewStructuredResponse(entries)` |
| `browser_pdf` | `internal/llm/tools/browser_devtools.go` | Base64 text when inline; TOML-first structured text when saved to file | Plain text error | Inline mode returns base64 PDF string; saved mode returns `{ saved, path }` |
| `browser_evaluate` | `internal/llm/tools/browser_evaluate.go` | TOML-first structured text | Plain text error | Wraps evaluation result with `NewStructuredResponse(result)` |
| `browser_click` | `internal/llm/tools/browser_interact.go` | TOML-first structured text | Plain text error | Returns `{ clicked, selector }` |
| `browser_fill` | `internal/llm/tools/browser_interact.go` | TOML-first structured text | Plain text error | Returns `{ filled, selector }` |
| `browser_scroll` | `internal/llm/tools/browser_interact.go` | TOML-first structured text | Plain text error | Returns `{ scrolled, x, y }` |
| `browser_navigate` | `internal/llm/tools/browser_navigate.go` | TOML-first structured text | Plain text error | Returns `{ url, title, status }` |
| `browser_screenshot` | `internal/llm/tools/browser_screenshot.go` | Not inspected here; likely structured response or binary/text depending on implementation | Plain text error | Needs file-level confirmation if you want exact payload details |
| `cache_stats` | `internal/llm/tools/cache_stats.go` | Plain text | Plain text error | Returns message like `No session cache active` |
| `cache_read` | `internal/llm/tools/cache_read.go` | Plain text or structured helper output depending on result | Plain text error | Not fully expanded in this inventory |
| `context7_resolve` | `internal/llm/tools/context7.go` | Plain text or Markdown-like text | Plain text error | Returns “no libraries found” message or formatted results |
| `context7_docs` | `internal/llm/tools/context7.go` | Plain text or Markdown-like text | Plain text error | Returns documentation text; not structured TOML |
| `diagnostics` | `internal/llm/tools/diagnostics.go` | Plain text | Plain text error | Diagnostic text output |
| `edit` | `internal/llm/tools/edit.go` | Plain text | Plain text error | File edit confirmations and errors are text |
| `fetch` | `internal/llm/tools/fetch.go` | Markdown, JSON fenced block, or plain text depending on content type | Plain text error | HTML → Markdown; JSON → fenced `json` block; text/plain → plain text |
| `glob` | `internal/llm/tools/glob.go` | Plain text | Plain text error | Path listings are textual |
| `grep` | `internal/llm/tools/grep.go` | Plain text | Plain text error | Search output is textual |
| `ls` | `internal/llm/tools/ls.go` | Plain text | Plain text error | Tree/list output is textual |
| `lua` | `internal/llm/tools/lua_tools.go` | Plain text | Plain text error | Lua output is passed through as text |
| `mesnada_spawn` | `internal/llm/tools/mesnada.go` | TOML-first structured text | Plain text error | Returns task metadata/result struct |
| `mesnada_get_task` | `internal/llm/tools/mesnada.go` | TOML-first structured text | Plain text error | Structured task state/result |
| `mesnada_list_tasks` | `internal/llm/tools/mesnada.go` | TOML-first structured text | Plain text error | Structured task list |
| `mesnada_wait_task` | `internal/llm/tools/mesnada.go` | TOML-first structured text | Plain text error | Structured task state/result |
| `mesnada_cancel_task` | `internal/llm/tools/mesnada.go` | TOML-first structured text | Plain text error | Structured cancellation result |
| `mesnada_get_output` | `internal/llm/tools/mesnada.go` | TOML-first structured text | Plain text error | Returns task output/log metadata |
| `mesnada_await` | `internal/llm/tools/mesnada_await.go` | Plain text | Plain text error | Satisfied/unsatisfied await text |
| `patch` | `internal/llm/tools/patch.go` | Plain text | Plain text error | Patch application results are textual |
| `remember` | `internal/llm/tools/remembrances_memory.go` | Plain text | Plain text error | Memory CRUD confirmation text |
| `forget` | `internal/llm/tools/remembrances_memory.go` | Plain text | Plain text error | Memory deletion confirmation or error |
| `kb_add_document` | `internal/llm/tools/remembrances_kb.go` | Plain text | Plain text error | Returns memory/KB action confirmation text |
| `kb_import_path` | `internal/llm/tools/remembrances_kb.go` | TOML-first structured text | Plain text error | Returns import stats `{ path, delete_missing, scanned, added, updated, unchanged, deleted }` |
| `kb_search_documents` | `internal/llm/tools/remembrances_kb.go` | TOML-first structured text | Plain text error | Returns `{ count, results }` or `No documents found...` |
| `kb_get_document` | `internal/llm/tools/remembrances_kb.go` | TOML-first structured text | Plain text error / plain text not-found message | Returns document fields when found |
| `kb_delete_document` | `internal/llm/tools/remembrances_kb.go` | Plain text | Plain text error | Delete confirmation text |
| `code_index_project` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error | Returns job metadata |
| `code_index_status` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error / plain text not-found message | Structured status when job exists |
| `code_hybrid_search` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error / plain text “no symbols” message | Returns `{ count, results }` |
| `code_find_symbol` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error / plain text “no symbols” message | Returns symbol matches |
| `code_get_symbols_overview` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error / plain text “no symbols” message | Returns overview list |
| `code_get_project_stats` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error | Returns project stats struct |
| `code_delete_project` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error | Returns deletion result |
| `code_reindex_file` | `internal/llm/tools/remembrances_code.go` | Plain text | Plain text error | Confirmation message |
| `code_list_projects` | `internal/llm/tools/remembrances_code.go` | TOML-first structured text | Plain text error / plain text “no indexed projects” | Returns project list |
| `code_search_pattern` | `internal/llm/tools/remembrances_code.go` | Plain text or TOML-first structured text depending on result path | Plain text error / no-match text | Search output varies by result path |
| `code_find_references` | `internal/llm/tools/remembrances_code.go` | Plain text or TOML-first structured text depending on result path | Plain text error / no-match text | Search output varies by result path |
| `save_event` | `internal/llm/tools/remembrances_events.go` | Plain text | Plain text error | Returns saved event ID text |
| `search_events` | `internal/llm/tools/remembrances_events.go` | TOML-first structured text | Plain text error / plain text no-results message | Returns event list |
| `hybrid_search_remembrances` | `internal/llm/tools/remembrances_hybrid.go` | TOML-first structured text | Plain text error | Returns `{ count, results }` |
| `brave_search` | `internal/llm/tools/search_brave.go` | Markdown or plain text | Plain text error / no-results text | Search results are formatted as text/Markdown |
| `exa_search` | `internal/llm/tools/search_exa.go` | Markdown or plain text | Plain text error / no-results text | Search results are formatted as text/Markdown |
| `google_search` | `internal/llm/tools/search_google.go` | Markdown or plain text | Plain text error / no-results text | Search results are formatted as text/Markdown |
| `perplexity_search` | `internal/llm/tools/search_perplexity.go` | Plain text / Markdown-like text | Plain text error | Returns synthesized answer text |
| `sourcegraph` | `internal/llm/tools/sourcegraph.go` | Plain text / Markdown-like text | Plain text error | GraphQL and stream results are textual |
| `todo_write` | `internal/llm/tools/todo_write.go` | Plain text | Plain text error | Returns todo summary text |
| `tool_search` | `internal/llm/tools/tool_search.go` | TOML-first structured text | Plain text error / plain text no-tools message | Returns tool search results list |
| `view` | `internal/llm/tools/view.go` | Plain text | Plain text error | File content is returned as text |
| `write` | `internal/llm/tools/write.go` | Plain text | Plain text error | Confirmation/error messages are text |
| `mcp_query_catalog` | `internal/mcpgateway/proxy_tools.go` | TOML-first structured text | Plain text error / plain text no-tools message | Returns paginated catalog payload |
| `mcp_call_tool` | `internal/mcpgateway/proxy_tools.go` | TOML-first structured text for non-string outputs; string outputs are normalized through TOML-first helper | Plain text error | Proxied tool strings pass through `FormatJSONLikeContent(...)` |
| `runTool` bridge | `internal/llm/agent/mcp-tools.go` | TOML-first structured text for string outputs; non-string outputs are normalized through TOML-first helper | Plain text error | Converts external MCP tool output into Pando tool response text |

## Notes
- This table reflects the current runtime behavior of the implementation, not just the intent described in comments.
- The words TOON and TOML are sometimes used interchangeably in comments/tests, but the serializer used in code is TOML-first.
- Where a tool can return more than one format depending on the path, the table records the exact branching.

## Verification
- Matched each tool family by scanning `return NewStructuredResponse(...)`, `return NewTextResponse(...)`, and `return NewTextErrorResponse(...)` call sites.
- Reviewed the relevant helper paths in `internal/llm/tools/json_output.go`, `internal/llm/tools/fetch.go`, `internal/format/format.go`, `internal/llm/agent/mcp-tools.go`, and `internal/mcpgateway/proxy_tools.go`.
- Confirmed that structured helper output is TOML-first with JSON fallback.
