---
created_at: 2026-08-19T08:08:07.104452192Z
updated_at: 2026-08-19T09:34:22.231126437Z
tags:
    - feature
    - tools
    - pando_setup
    - mesnada
    - projects
    - pando
---
# Feature: `pando_setup projects` + discoverability from the spawn tools

Date: 2026-08-19

## What

`pando_setup` gained a `projects` command listing the project registry (id, name, path,
status, `--search TERM`) — the ids/paths/names `mesnada_spawn_agent`'s `project` argument
resolves against. Bridge: `SetupBridge.Projects(ctx)` implemented in
`internal/llm/agent/setup_bridge.go` over a process-wide `globalProjectServiceForTools`
wired from `internal/app/app.go` via `agent.SetProjectServiceForTools(projects)`.

## Problem found after the first build

The command worked, but no agent ever used it: nothing advertised it. `pando_setup`'s tool
description does not enumerate commands (they are only visible via `command="help"`), and the
spawn tools' `project` parameter never said where the ids come from.

## Fix (terse on purpose — these strings are in every prompt)

- `internal/llm/tools/pando_setup.go` — `pandoSetupDescription` now names the capability:
  `list registered projects (command="projects", for mesnada_spawn_agent's "project" argument)`.
- `internal/llm/tools/mesnada.go` (`MesnadaSpawnTool.Info`, `project` param) — added one
  sentence: `List the registered projects with pando_setup command="projects".`
- `internal/mesnada/server/tools.go` — same sentence on the MCP-gateway copy of the spawn
  tool schema, since tool discovery and the MCP gateway now share tools.

## Verification

- `go build ./...` — clean.
- `go test ./internal/llm/tools -run Setup` — ok.

Related: [[webui_steering_user_turn_rendering]]
