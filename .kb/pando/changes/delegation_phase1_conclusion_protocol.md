---
created_at: 2026-06-21T11:22:28.092289558Z
updated_at: 2026-06-21T11:22:28.092289558Z
tags:
    - change
    - mesnada
    - delegation
    - phase1
    - conclusion
    - orchestrator
    - enricher
    - parser
---
# Change: Delegation Phase 1 — Conclusion protocol (brief + parse + enrich)

Implemented 2026-06-21 (delegated subagent + parent review/cleanup). Status: DONE,
verified. Part of plan `pando/plans/delegated_conclusion_resurrection_plan.md`
(Phase 1). Builds on Phase 0 (`pando/changes/delegation_phase0_foundations.md`).
**Still no re-entry behavior — default-OFF.** This phase captures, enriches and
persists a `Conclusion` on each delegated task; nothing consumes it yet (Case A/B
are Phases 2/3).

## What changed & why
A delegated subagent emits only a thin `<pando:conclusion>` sentinel block; the
software fills in all launch metadata it owns. This phase implements: (1) the
brief that instructs the subagent to emit the block, (2) a tolerant parser, (3) an
enricher that fills software-owned metadata + synthesizes a fallback when the block
is absent, and (4) wiring so capture runs on task completion.

## New package `internal/mesnada/conclusion/`
- `parse.go` — `Parse(raw) (*models.Conclusion, bool)`: scans for the LAST
  `<pando:conclusion>...</pando:conclusion>` block (final summary wins), parses the
  inner body as YAML (`gopkg.in/yaml.v3`). Tolerant: malformed body → raw inner
  text salvaged as Summary; `normalizeStatus` → success|partial|failed|blocked|"";
  `clampConfidence` → [0,1]. (Parent cleanup: renamed local `close`/`open` →
  `closeIdx`/`openIdx` to not shadow the builtin `close`.)
- `brief.go` — `BriefInstruction()` returns the centralized trailing prompt text
  (lists only model-known fields + an example block; tells the model NOT to restate
  task id/engine/model/paths).
- `enrich.go` — `Enrich(task, DelegationOptions{SynthesizeFallback}, ProjectResolver)`:
  parses `task.Output` then `task.OutputTail`; if absent and `SynthesizeFallback`
  → deterministic synthesis (completed→success / non-zero-exit→partial; failed→
  failed; cancelled→blocked; Summary from last ~1000 bytes of tail or Error+exit).
  Always sets `CapturedAt`, defaults Status from terminal state, resolves project
  id/name via resolver (falls back to `filepath.Base(ProjectPath)`). Nil-safe, no
  globals. `// TODO(phase-later)`: optional cheap-model summarization.
  `ProjectResolver func(canonicalPath)(id,name)` + `DelegationOptions` defined here
  to keep the package free of config/project import cycles.

## Wiring
- `pkg/mesnada/models/task.go` — added `Task.ProjectName` (`json:"project_name,omitempty"`).
- `internal/mesnada/orchestrator/orchestrator.go` — `orchestrator.Config` &
  `Orchestrator` gained `DelegationConfig{Enabled, SynthesizeFallback}` +
  `ProjectResolver` (plain fields, mirrors `ModelResolver`, no internal/config
  import). `Spawn` appends `conclusion.BriefInstruction()` after persona ONLY when
  `delegation.Enabled`. New panic-safe `captureConclusion(task)` runs the enricher,
  stores `task.Conclusion`/`ProjectID`/`ProjectName`, re-persists. **Invoked in
  `onTaskComplete` BEFORE the subscriber-notify loop** (orchestrator.go:185 vs 195)
  — so the `*Task` pointer delivered to subscribers already carries the enriched
  `Conclusion`. Important for Phases 2/3.
- `internal/app/app.go` — `convertMesnadaConfig` sets `orchCfg.Delegation` from
  `cfg.Mesnada.Delegation`; `makeProjectResolver(ctx, app.Projects)` backs the
  resolver with `project.Service.GetByPath` (already existed) and is assigned before
  `mesnadaOrch.New`. `app.Projects` is initialized in the app struct literal
  (app.go:205), well before orchestrator construction (481), so it is non-nil.

## Tests
- `internal/mesnada/conclusion/parse_test.go`, `enrich_test.go`, `brief_test.go`
  (16 tests): happy/missing/partial/multi-block/malformed parsing, status
  normalization, confidence clamp; enrich parsed-vs-synthesized paths, terminal
  status defaults, resolver vs filepath.Base, CapturedAt set; brief mentions the
  sentinel.

## Verification (parent-run)
- `go build ./...` → clean. (Editor compiler diagnostic about `task.ProjectName`
  undefined was a STALE mid-edit snapshot; field exists at task.go:122, build clean.)
- `go vet ./internal/mesnada/conclusion ./internal/app ./internal/config` → clean
  (two pre-existing warnings in `spawner_template.go`, untouched, unrelated).
- `go test -count=1 ./internal/mesnada/... ./internal/config ./internal/llm/tools
  ./internal/app` → all pass.

## Notes for Phases 2/3
- Read the conclusion off `task.Conclusion` (durable in FileStore; also present on
  the subscriber-delivered `*Task` pointer). Project framing ("agent X of project
  Y") can read `ProjectID/ProjectName/ProjectPath` directly off the task.
- Only `Enabled`+`SynthesizeFallback` are threaded into `orchestrator.DelegationConfig`
  so far. Phases 2/3 will need `MaxResurrections/MaxDepth/MaxConcurrent/
  ResurrectionTimeout` threaded the same way (plain fields, translated in app.go).
- Keep the supervisor subscription gated behind `InjectIntoLiveLoop` /
  `ResurrectIdleLoop`, same default-off discipline.
- Deferred: in-memory `subscribersBySession` index (Phase 0 plan note) belongs with
  the Phase 2/3 supervisor; optional cheap-model summarization of fallbacks.
