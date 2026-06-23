---
created_at: 2026-06-23T13:57:26.740123767Z
updated_at: 2026-06-23T13:57:26.740123767Z
tags:
    - change
    - mesnada
    - delegation
    - e2
    - observability
    - persona
    - model
    - orchestrator
    - tui
    - webui
    - panel
---
# Change: E2 — surface per-task (per-session) model/persona in the orchestrator panel

Date: 2026-06-23. Implements backlog item E2 (Observability, alongside E1) from
`pando/plans/delegation_future_improvements.md`. Default behaviour unchanged
(read-only display; no config knob).

## Context / scope decision
The orchestrator `models.Task` already carries both `Model string` and
`Persona string`. The **model** was already surfaced in both panels (a Model
column + a detail line in the TUI orchestrator page, and a model cell in the WebUI
`TaskRow` + a Model line in `TaskDetail`). The **persona** was not surfaced
anywhere. So E2's real gap was persona.

Persona is only populated for delegated / persona-scoped runs (most tasks leave it
empty), so a dedicated mostly-blank table column would be poor UX. It is surfaced
in the **detail panel** of each UI — where Model already appears — shown only when
non-empty. This matches "per-session model/persona surfaced in the panel" without
adding a low-signal column.

## What changed

### Backend API
- `internal/api/handlers_orchestrator.go`:
  - `TaskResponse` gained `Persona string json:"persona,omitempty"` (after Model).
  - `taskToResponse` maps `Persona: t.Persona`. `omitempty` drops it when unset.

### TUI (orchestrator page)
- `internal/tui/page/orchestrator.go` `buildDetailContent`: the existing
  `Engine: … Model: …` line now appends `  Persona: <name>` only when
  `strings.TrimSpace(task.Persona) != ""`. No new table column (avoids the
  resizeColumns/width/hit-test churn for a usually-empty field).

### WebUI
- `web-ui/src/types/index.ts`: `OrchestratorTask` gained optional `persona?: string`.
- `web-ui/src/components/orchestrator/TaskDetail.tsx`: a new `faUserTie` Persona row
  rendered after the Model row, conditional on `task.persona` (matches the existing
  hardcoded-label style of the Agent/Model/Created rows — these labels are not
  i18n'd, so no locale changes were needed).

## Files touched
- `internal/api/handlers_orchestrator.go` (DTO field + mapping)
- `internal/api/handlers_orchestrator_test.go` (NEW: `TestTaskToResponseSurfacesModelAndPersona`, `TestTaskToResponseOmitsEmptyPersona`)
- `internal/tui/page/orchestrator.go` (detail line)
- `web-ui/src/types/index.ts` (type field)
- `web-ui/src/components/orchestrator/TaskDetail.tsx` (Persona detail row)

## Why
Closes the observability gap so an operator can see which persona a delegated /
persona-scoped task ran under (the model was already visible). Complements the
B3 follow-up that made persona/model actually apply on the delegation target
(`pando/changes/delegation_b3_followups_persona_model_correlation.md`): personas
requested via the spawn tool / delegation now both take effect AND are visible.

## Verification
- `gofmt -l` clean on touched Go files; `go vet ./internal/api ./internal/tui/page` clean.
- `go build ./internal/...` clean.
- `go test ./internal/api ./internal/tui/page ./internal/llm/agent` all ok; new
  `TestTaskToResponse*` pass.
- web-ui `npx tsc --noEmit` → exit 0.

## Remaining backlog
- A2 cross-restart re-attach for external peers atop the B3 IPC transport (now
  feasible, not yet built) — the last open delegation item.
