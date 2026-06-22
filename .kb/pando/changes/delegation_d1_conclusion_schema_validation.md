---
created_at: 2026-06-22T21:02:54.955537005Z
updated_at: 2026-06-22T21:02:54.955537005Z
tags:
    - change
    - mesnada
    - delegation
    - conclusion
    - validation
    - schema
    - d1
---
# Change: Delegation D1 — Soft schema-validation of the `<pando:conclusion>` block

Date: 2026-06-22. Backlog item D1 from `pando/plans/delegation_future_improvements.md`. Implemented via a worktree subagent (engine claude/sonnet) and unified by the parent. Principle: **warn, don't discard** — parsing a subagent's conclusion block never rejects data; instead it accumulates human-readable warnings describing what was malformed/missing/out-of-range, so malformed conclusions are observable without losing the salvaged content. See feature `pando/features/delegated_conclusions_resurrection.md`.

## What changed / motivation
Before D1 the conclusion parser (`internal/mesnada/conclusion/parse.go`) was tolerant (unparseable YAML → salvage raw text as summary; unknown status → "") but SILENT: there was no signal that a block was malformed. D1 keeps the same tolerance but records why, surfacing it to the parent loop.

## Files & symbols
- `pkg/mesnada/models/task.go` — `Conclusion` gains `Warnings []string` (`json:"warnings,omitempty"`); never causes the conclusion to be discarded.
- `internal/mesnada/conclusion/parse.go` — `Parse` now accumulates `c.Warnings` (deterministic order):
  - **unknown status**: raw status non-empty but `normalizeStatus`→"" → `unknown status "<raw>"; left for software default`.
  - **confidence out of range**: raw confidence outside [0,1] (before `clampConfidence`) → `confidence <v> out of range [0,1]; clamped`.
  - **missing summary**: structured body parsed but summary blank → `missing summary`.
  - **blank artifacts/memory_refs**: whitespace-only entries dropped (kept non-blank) → `dropped N blank <field> entr(y/ies)` (helper `filterBlanks`).
  - **malformed YAML body** (existing salvage path) → `conclusion body was not valid YAML; salvaged raw text`.
  - **non-mapping YAML** (existing zero-content salvage path) → `conclusion body was not a YAML mapping; salvaged raw text`.
- `internal/mesnada/conclusion/brief.go` — the prompt instruction is now more normative: `status` + `summary` marked REQUIRED, allowed `status` values and the `0.0`–`1.0` confidence range made explicit, example confidence shown as a typical non-zero value — so subagents emit fewer malformed blocks.
- **Package constraint preserved**: no new imports (only models + stdlib + `gopkg.in/yaml.v3`); warnings are STORED on the model, not logged in the package.

## Cross-cut with D2 (unification glue)
`FormatForParent` (D2's file) now renders a trailing compact `warnings: <a, b, …>` line when `Conclusion.Warnings` is non-empty (omitted otherwise) so the parent agent learns the delegated conclusion was malformed even though the data was salvaged. Test: `TestFormatForParentRendersWarnings`.

## Verification
- `gofmt -l` clean; `go build` of touched packages OK (repo-wide `./...` fails only on pre-existing missing embed artifacts `bin/pando-desktop`, `webui/dist/**` — unrelated).
- New tests in `parse_test.go`: `TestParse_WarnUnknownStatus`, `TestParse_WarnConfidenceOutOfRange` (high/low), `TestParse_WarnMissingSummary`, `TestParse_WarnBlankArtifactsDropped`, `TestParse_WarnBlankMemoryRefsDropped`, `TestParse_WarnMalformedYAMLSalvaged`, `TestParse_WarnNonMappingYAMLSalvaged`, `TestParse_HappyBlockNoWarnings`, `TestParse_MultipleWarningsAccumulate` — each also asserts the underlying data is preserved (warn-don't-discard).
- `go test ./internal/mesnada/conclusion/... ./pkg/mesnada/models/...` green; `-race` green. Broader suites (`internal/llm/agent`, `internal/api`, `internal/project`, `internal/mesnada/...`) green.
