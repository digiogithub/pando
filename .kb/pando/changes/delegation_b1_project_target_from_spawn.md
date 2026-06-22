---
created_at: 2026-06-22T10:58:23.486288565Z
updated_at: 2026-06-22T10:58:23.486288565Z
tags:
    - change
    - mesnada
    - delegation
    - orchestrator
    - warm-instance
    - spawn-tool
    - projects
---
# Change: B1 — Target a registered project by id/name/path from the spawn tool

Date: 2026-06-22. Backlog item **B1** from `pando/plans/delegation_future_improvements.md`.
Part of the delegated-conclusions + warm-instance feature (see
`pando/features/delegated_conclusions_resurrection.md`).

## What changed

Let the agent (and the standalone Mesnada MCP server) aim a delegated task at a
**specific registered project** by passing a new optional `project` argument to
the spawn tool. The reference may be the project's registry **id**, **display
name** (case-insensitive, trimmed), or **directory path** (canonicalised). The
task is then routed to that project's warm per-project ACP instance (when warm
reuse is enabled), and its `work_dir` defaults to the project's directory. An
unknown reference fails fast with a helpful error listing the known projects, so
a typo never silently runs in the wrong place.

The warm-routing plumbing already carried `ProjectID` end-to-end
(`SpawnRequest.ProjectID` → `Task.ProjectID` → `tryStartWarm` → `RunWarm` →
`Manager.WarmDelegate` → `resolveProjectID`, which uses a non-empty id as-is).
B1 only had to let the spawn tool **set** that id from a user-facing reference and
resolve it against the registry. Default behaviour is unchanged when `project` is
omitted (id stays empty → resolution by canonical path as before).

## Files / symbols touched

- `internal/mesnada/orchestrator/warm.go` — new `ProjectRef{ID,Name,Path}` struct
  and `ProjectRefResolver` interface (`ResolveProjectRef`, `ListProjectRefs`),
  mirroring the `WarmTargetResolver` injection pattern (no internal/project import
  cycle). New `Orchestrator` accessors `ResolveProjectRef` / `ProjectRefsSupported`
  / `ListProjectRefs` (nil-guarded, delegate to the injected resolver).
- `internal/mesnada/orchestrator/orchestrator.go` — `projectRefResolver` field on
  `Orchestrator`, `ProjectRefResolver` field on `Config`, wired in `New`.
- `internal/app/app.go` — `projectRefResolverAdapter` over `project.Service`
  (resolves by exact id `Get`, then canonical path `GetByPath` via
  `config.CanonicalProjectPath`, then case-insensitive exact name from `List`);
  `makeProjectRefResolver(svc)` (nil for nil service); wired as
  `mesnadaCfg.ProjectRefResolver = makeProjectRefResolver(app.Projects)`.
- `internal/llm/tools/mesnada.go` — `project` parameter in `MesnadaSpawnTool.Info`;
  `Project` field in spawnParams; resolution block after the prompt check
  (rejects when unsupported, errors with `knownProjectsHint` when unknown, sets
  `targetProjectID` + defaults `WorkDir` to the project path); `ProjectID:
  targetProjectID` added to the `SpawnRequest`; new `knownProjectsHint([]ProjectRef)`
  helper (lists up to 10 `name (id)` entries, path fallback when name empty,
  "…and N more" truncation).
- `internal/mesnada/server/tools.go` — same `project` parameter in the standalone
  MCP `spawn_agent` schema + request struct + resolution block + `ProjectID` on the
  `SpawnRequest`; sibling `serverKnownProjectsHint` helper.
- `README.md` — extended the warm-instance bullet describing the `project` argument.

## Why

Without B1 a delegated task could only be warm-routed to the project implied by
its `work_dir`/canonical path. Item B1 lets the model (or an MCP client) explicitly
target an already-registered project — useful for cross-project delegation and for
reusing a specific warm instance — while keeping the anti-fork-bomb caps and the
cold fallback intact (an unresolved id is rejected at the tool boundary rather than
becoming a terminal-failed task deep in the warm path).

## Verification

- `go build ./...` clean; `go vet` clean on tools/orchestrator/server/app.
- `gofmt -l` clean on all touched Go files.
- New tests:
  - `internal/app/project_ref_resolver_test.go` — `projectRefResolverAdapter`
    resolution by id / path / case-insensitive name / miss / empty ref +
    `ListProjectRefs` + nil-service → nil resolver (fake `project.Service`).
  - `internal/llm/tools/mesnada_project_target_test.go` — `knownProjectsHint`
    empty / lists `name (id)` with path fallback / truncation ("and 5 more").
  - `internal/mesnada/orchestrator/warm_test.go` — `TestProjectRefAccessorsNil`
    (struct built directly to avoid `New()`'s background store writers racing
    `t.TempDir` cleanup) + `TestProjectRefAccessorsWired` (delegates to a fake
    resolver).
- Suites green: `internal/llm/tools`, `internal/mesnada/orchestrator`,
  `internal/mesnada/server`, `internal/app`, `internal/llm/agent`, `internal/api`.

## Notes / future

- Resolution is exact (no fuzzy/prefix name match) to avoid ambiguous targeting;
  on duplicate names the newest-first `List` order wins.
- The standalone MCP `spawn_agent` and the in-process `mesnada_spawn_agent` now
  share identical `project` semantics but keep separate hint helpers (different
  packages, no shared util layer).
