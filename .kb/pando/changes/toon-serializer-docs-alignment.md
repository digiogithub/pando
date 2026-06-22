---
created_at: 2026-06-22T07:54:57.917924458Z
updated_at: 2026-06-22T07:54:57.917924458Z
tags:
    - changes
    - documentation
    - serialization
    - toon
    - toml
    - json
---
# TOON serializer documentation alignment

## What changed
Performed a follow-up pass to align comments, help text, and user-facing descriptions with the new TOON-first serializer behavior.

## Files and symbols touched
- `internal/format/format.go`
  - `JSON` output format comment
  - `GetHelpText`
  - `formatAsJSON`
- `internal/llm/tools/browser_navigate.go`
  - `browserNavigateToolDescription`
- `internal/app/app_noninteractive_test.go`
  - assertion message updated to describe structured output more precisely

## Motivation
After switching the shared serializer to TOON-first behavior, several comments and descriptions still implied the old TOON/TOML ambiguity or described the response simply as JSON. This pass makes the documentation consistent with the actual runtime order: TOON first, then TOML, then JSON.

## Verification
- Reviewed remaining references to TOON/TOML wording in code comments and descriptions
- Ran: `go test ./internal/llm/tools ./internal/app`

## Notes
There may still be historical KB documents describing the previous TOML-first implementation, but the code comments and user-facing strings updated in this pass now reflect the current behavior.