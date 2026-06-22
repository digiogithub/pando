---
created_at: 2026-06-22T21:03:11.216195331Z
updated_at: 2026-06-22T21:03:11.216195331Z
tags:
    - change
    - mesnada
    - delegation
    - conclusion
    - format
    - artifacts
    - memory-refs
    - d2
---
# Change: Delegation D2 — Richer artifact / memory_ref resolution in FormatForParent

Date: 2026-06-22. Backlog item D2 from `pando/plans/delegation_future_improvements.md`. Implemented via a worktree subagent (engine claude/sonnet) and unified by the parent. Default-graceful: with NO resolver, output is byte-for-byte identical to the pre-D2 compact form. See feature `pando/features/delegated_conclusions_resurrection.md`.

## What changed / motivation
`conclusion.FormatForParent` rendered `artifacts:` / `memory_refs:` as a single plain comma-joined list of raw strings — the parent agent could not tell which artifact paths actually exist or what a memory ref points to without a separate fetch. D2 lets those references be RESOLVED to richer (still compact) detail via injected resolvers, keeping the "pointers, not dumps" contract.

## Files & symbols
- `internal/mesnada/conclusion/resolve.go` (NEW) — resolver plumbing (function-value pattern, no new package imports, mirrors existing `ProjectResolver`):
  - `type ArtifactResolver func(ref string) string`, `type MemoryRefResolver func(ref string) string` (return input unchanged when unable to enrich).
  - `type ResolveOptions struct { Artifacts ArtifactResolver; Memory MemoryRefResolver }` (nil-safe).
  - `func NewFilesystemArtifactResolver(baseDir string) ArtifactResolver` — resolves relative refs against baseDir, stats the file → `"<ref> (1.2 KB)"` / `"<ref> (missing)"` / bare `<ref>` on error/blank baseDir; helper `humanBytes` (B/KB/MB). Uses only `os`/`path/filepath`/`fmt`/`strings`.
- `internal/mesnada/conclusion/format.go` — `FormatForParent(task)` → `FormatForParent(task, res *ResolveOptions)`. nil res or nil per-field resolver → legacy comma-joined line (unchanged). With a resolver, each entry rendered on its own line under the field header (`\n  - <resolved>`); blanks still dropped; empty list still omits the section. Also renders the D1 `warnings:` line (unification glue).
- Call sites updated to pass a filesystem artifact resolver from the task's `ProjectPath` (fallback `WorkDir`); Memory resolver left nil with a `// TODO(D2-followup): wire memory_ref resolver from kb store`:
  - `internal/llm/tools/mesnada_await.go` — local `taskResolveOptions(task)` helper at the await-satisfied response.
  - `internal/app/delegation_supervisor.go` — local `supervisorResolveOptions(task)` helper used at all 3 `FormatForParent` sites (`injectLive`, `buildResurrectionContent`, `buildAwaitResurrectionContent`).
  - `internal/app/delegation_e2e_test.go` — updated to the new signature via `supervisorResolveOptions`.

## Design notes
- Conclusion package stays import-cycle-free: filesystem resolution is pure stdlib; kb/memory resolution is deferred (nil resolver = graceful current behavior) and will be wired from the kb store as a follow-up without touching the conclusion package signature again.
- Memory-ref enrichment is plumbed but intentionally not yet wired (keeps D2 self-contained and out of the larger app wiring); the path is a trivial future wire at the supervisor's `supervisorResolveOptions`.

## Verification
- `gofmt -l` clean on all changed files; `go build` of `internal/mesnada/conclusion`, `internal/llm/tools`, `internal/app` OK (repo-wide `./...` fails only on pre-existing missing embed artifacts — unrelated).
- `format_test.go`: nil-resolver path asserts byte-identical legacy output; resolver path tests (fake artifact + memory resolvers, per-line rendering, blank-only omission, nil==empty-options equality); `NewFilesystemArtifactResolver` tests with `t.TempDir()` (existing→size, missing→`(missing)`, absolute path, blank baseDir); `humanBytes` table.
- `go test ./internal/mesnada/conclusion/... ./internal/app/... ./internal/llm/tools/...` green; `-race` green. Broader suites (`internal/llm/agent`, `internal/api`, `internal/project`, `internal/mesnada/...`) green.
