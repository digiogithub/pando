---
created_at: 2026-06-22T07:54:04.121113189Z
updated_at: 2026-06-22T07:54:04.121113189Z
tags:
    - changes
    - serialization
    - toon
    - toml
    - json
---
# TOON-first serializer refactor

## What changed
Refactored the shared structured response serializer in `internal/llm/tools/json_output.go` so it now serializes structured values in this order:

1. TOON
2. TOML
3. indented JSON

The implementation now normalizes values through JSON-compatible data first, then tries `toon.Marshal(...)`, falls back to `toml.Marshal(...)`, and only then falls back to `json.MarshalIndent(...)`.

## Files and symbols touched
- `go.mod`: added `github.com/toon-format/toon-go`
- `internal/llm/tools/json_output.go`
  - `NewStructuredResponse`
  - `FormatStructuredData`
  - `FormatJSONLikeContent`
  - `normalizeStructuredValue`
  - `tryFormatAsTOON`
  - `tryFormatAsTOMLNormalized`
  - `tryFormatRawJSONAsStructured`
- `internal/llm/tools/json_output_test.go`
- `internal/app/app_noninteractive_test.go`

## Motivation
There was a mismatch between the documentation/tests language and the actual implementation: comments and tests referred to TOON, but the serializer was actually emitting TOML. This refactor aligns runtime behavior with the intended TOON-first design while preserving compatibility through TOML and JSON fallbacks.

## Verification
- Reviewed TOON reference documentation and syntax guidance from `toonformat.dev`
- Added the Go TOON implementation dependency `github.com/toon-format/toon-go`
- Updated tests to assert actual TOON syntax rather than TOML syntax
- Ran: `go test ./internal/llm/tools ./internal/app`

## Notes
This refactor changes the output shape of all tools that depend on `NewStructuredResponse(...)` or `FormatJSONLikeContent(...)`, because they now emit TOON by default where possible instead of TOML.