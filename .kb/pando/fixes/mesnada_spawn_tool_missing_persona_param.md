---
created_at: 2026-07-23T07:08:23.999694506Z
updated_at: 2026-07-23T07:08:23.999694506Z
tags:
    - fix
    - mesnada
    - persona
    - tools
---
# Fix: `mesnada_spawn_agent` tool ignored the `persona` parameter

## Problem
The `MesnadaSpawnTool` in `internal/llm/tools/mesnada.go` did not expose or forward
the `persona` parameter, so persona instructions could never be applied to
sub-agents spawned via the `mesnada_spawn_agent` MCP/LLM tool. The orchestrator
already supported personas end-to-end (`SpawnRequest.Persona`,
`Orchestrator.ApplyPersona`, `Orchestrator.ListPersonas`, `Task.Persona`) and the
standalone MCP server (`internal/mesnada/server/tools.go`) already advertised
personas — only the in-process LLM tool bridge was missing the wiring.

Reported/analysed by a third party (PR-fix-missing-persona-param.md).

## Changes — `internal/llm/tools/mesnada.go`
1. `Info()`: build a `personaParam` schema entry. When
   `t.orchestrator.ListPersonas()` returns names, the description lists them and a
   `enum` constrains the choice; otherwise the parameter is still accepted free-form.
   Added `"persona": personaParam` to the tool `Parameters` map. This mirrors the
   dynamic advertisement pattern used by the standalone server tools.
2. `spawnParams` struct (inside `Run`): added `Persona string `json:"persona"``.
3. `Spawn` call: added `Persona: req.Persona` to `models.SpawnRequest`.

Relaunch path (task_id) intentionally unchanged — it preserves the stored task's
persona.

## Motivation
Personas are a documented feature but were unusable from the primary tool used by
Pando's own coder agent to delegate work. Exposing the catalogue via enum lets the
model pick a valid persona instead of guessing.

## Verification
- `go build ./internal/llm/tools/` — OK
- `go test ./internal/llm/tools/` — ok (1.2s)
- Confirmed against current code: orchestrator `SpawnRequest.Persona`
  (pkg/mesnada/models/task.go:274), `ApplyPersona` (orchestrator.go:605),
  `ListPersonas` (orchestrator.go:1347).
