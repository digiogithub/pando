---
created_at: 2026-06-22T07:55:42.938957968Z
updated_at: 2026-06-22T07:55:42.938957968Z
tags:
    - changes
    - documentation
    - tools
    - serialization
    - toon
---
# Tool descriptions output review

## What changed
Reviewed built-in tool descriptions for output-format wording and updated descriptions that still implied raw JSON output where the implementation now returns structured TOON-first responses or other exact formats.

## Files and symbols touched
- `internal/llm/tools/browser_evaluate.go`
  - `browserEvaluateToolDescription`
- `internal/llm/tools/browser_content.go`
  - `browserGetContentToolDescription`
- `internal/llm/tools/browser_screenshot.go`
  - `browserScreenshotToolDescription`
- `internal/llm/tools/fetch.go`
  - `fetchToolDescription`

## Motivation
After the TOON-first serializer refactor, some tool descriptions still said they returned JSON even though they actually return structured responses through Pando's formatter, or they were imprecise about the exact text/binary shape returned. This pass aligns the descriptions with runtime behavior.

## Verification
- Searched tool descriptions for JSON-return wording and RETURNS sections
- Updated mismatched descriptions to reflect structured TOON-first output or exact text formats
- Ran: `go test ./internal/llm/tools ./internal/app`

## Notes
The current pass focused on built-in tool descriptions under `internal/llm/tools`. It did not rewrite historical KB documents that describe previous behavior.