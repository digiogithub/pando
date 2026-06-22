---
created_at: 2026-06-21T21:10:25.97496848Z
updated_at: 2026-06-21T21:10:25.97496848Z
tags:
    - changes
    - serialization
    - toon
    - json
    - responses
---
# TOON-first structured response serialization

## What changed
- Updated `internal/llm/tools/json_output.go` so JSON-like text is always reformatted through the structured renderer first, preferring TOON/TOML and falling back to a formatted JSON representation instead of returning raw JSON text unchanged after TOON serialization failure.
- Updated `internal/format/format.go` so non-interactive `json` output now means structured TOON/TOML output when possible, with JSON fallback only when TOON cannot represent the value.
- Updated `internal/app/app.go` so non-interactive goal results use the shared structured formatter instead of direct `json.MarshalIndent` output.
- Updated `internal/llm/tools/tool_search.go` to return structured responses via `NewStructuredResponse` instead of raw marshaled JSON text.
- Added/updated tests in `internal/llm/tools/json_output_test.go` and `internal/app/app_noninteractive_test.go` to verify TOON-first formatting and fallback behavior.

## Files and symbols touched
- `internal/llm/tools/json_output.go`: `FormatJSONLikeContent`, `NewStructuredResponse`
- `internal/format/format.go`: `GetHelpText`, `formatAsJSON`
- `internal/app/app.go`: `formatNonInteractiveGoalResult`, `mustGoalResultJSON`
- `internal/llm/tools/tool_search.go`: `toolSearchTool.Run`
- `internal/llm/tools/json_output_test.go`
- `internal/app/app_noninteractive_test.go`

## Motivation
The user requested that responses be serialized in TOON format wherever Pando exposes structured output, and that any TOON failure should degrade to JSON formatting without dumping raw JSON directly.

## Verification
- `go test ./internal/app ./internal/llm/tools`
- Searched indexed code and KB to identify places still returning raw JSON text for user-facing structured responses.
