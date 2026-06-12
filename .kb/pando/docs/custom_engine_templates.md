---
created_at: 2026-06-12T07:37:34.751432678Z
updated_at: 2026-06-12T07:37:34.751432678Z
tags:
    - docs
    - mesnada
    - engines
    - custom-engines
    - template
    - reference
---
# Custom Engine Templates for Mesnada

**Updated:** 2026-06-12  
**Status:** Implemented and stable

## Overview

Users can define new Mesnada engines — or override existing ones — by placing `*.template.yaml` files in the engines directory. No recompilation required. Templates are loaded at startup and appear as first-class engines in `mesnada_spawn_agent` / `spawn_agent`.

## Engines directory

| Context | Default path |
|---|---|
| Standalone Mesnada | `~/.mesnada/engines/` |
| Pando (via `mesnada.*`) | sibling of `LogDir`, e.g. `.pando/mesnada/engines/` |

Override via config:
- Standalone: `orchestrator.engines_dir` in `~/.mesnada/config.yaml`
- Pando: `mesnada.orchestrator.enginesDir` in pando config

The directory is created automatically on first startup, with a `README.md` explaining the format.

## Template file format

File name: `<engine-name>.template.yaml`  
The `name` field overrides the filename stem when present.

```yaml
# ── Identity ───────────────────────────────────────────────────────────────
name: my-agent          # engine ID used in spawn calls (no spaces)
description: "My custom agent CLI"

# ── Command ────────────────────────────────────────────────────────────────
command: my-agent-cli   # binary name (resolved via PATH) or absolute path

# ── Prompt delivery ────────────────────────────────────────────────────────
prompt_mode: arg        # "arg" (default) — appends flag+prompt to CLI args
                        # "stdin"         — pipes prompt to process stdin
prompt_arg: "-p"        # CLI flag for the prompt (prompt_mode=arg only, default "-p")

# ── Model ──────────────────────────────────────────────────────────────────
model_arg: "--model"    # CLI flag for model ID (omit to not pass model)
default_model: "my-model-v1"
models:
  - id: "my-model-v1"
    description: "Stable version"
  - id: "my-model-v2"
    description: "Latest version"

# ── Output format ──────────────────────────────────────────────────────────
output_format: text     # "text" (default) — stdout captured as-is
                        # "jsonl"          — stdout parsed as newline-delimited JSON

# JSONL settings (only when output_format=jsonl):
jsonl_output_field: "delta.text"        # dot-path to the text field in each JSON line
jsonl_filter_field: "type"              # optional: only process lines where...
jsonl_filter_value: "content_block_delta"  # ...this field equals this value

# ── Fixed CLI arguments ────────────────────────────────────────────────────
# Always prepended before dynamic args. Support template variables.
args:
  - "--no-color"
  - "--non-interactive"
  - "--work-dir={{.WorkDir}}"   # template variable example

# ── Environment variables ──────────────────────────────────────────────────
# Map style — concise, good for static values:
env:
  NO_COLOR: "1"
  API_KEY: "my-secret"
  MODEL_ID: "{{.Model}}"       # template variable in env value

# List style — explicit name/value, good for readability or dynamic values:
env_vars:
  - name: WORK_DIR
    value: "{{.WorkDir}}"
  - name: TASK_ID
    value: "{{.TaskID}}"
  - name: STATIC_FLAG
    value: "enabled"
```

Both `env` and `env_vars` can be used simultaneously. `env_vars` is applied after `env`, so it takes precedence when a name appears in both.

## Template variables

Supported in `args` values and in all environment variable values:

| Expression | Replaced with |
|---|---|
| `{{.Model}}` | Model ID passed to the task |
| `{{.WorkDir}}` | Task working directory (absolute path) |
| `{{.TaskID}}` | Unique task identifier |
| `{{.LogFile}}` | Absolute path to the task log file |

## Validation rules

| Rule | Error |
|---|---|
| `name` contains whitespace | rejected |
| `command` is empty | rejected |
| `output_format` is not `text` or `jsonl` | rejected |
| `output_format: jsonl` without `jsonl_output_field` | rejected |
| `env_vars` entry without `name` | rejected |
| Template syntax error in `args` or env values | rejected at spawn time |

Invalid template files are logged as warnings and skipped; the rest of the registry loads normally.

## How engines are discovered

1. At startup `NewTemplateRegistry(enginesDir, logDir, onComplete)` calls `ScanEnginesDir()`
2. Every `*.template.yaml` / `*.template.yml` file is loaded and validated
3. A `TemplateSpawner` is created for each valid template
4. Names are added to the `engine` enum in `mesnada_spawn_agent` / `spawn_agent` tool descriptions
5. `ValidateEngine()` accepts any registered template engine name

## Lifecycle

- `Spawn` — builds final args and env (with template expansion), starts the process
- `Cancel` / `Pause` — sends SIGTERM, waits 5 s, then SIGKILL
- `Wait` — blocks on `done` channel
- `IsRunning` / `RunningCount` / `Shutdown` — standard management

JSONL output: each stdout line is parsed as JSON; lines that fail JSON parsing or don't match the filter are silently skipped. Only the field at `jsonl_output_field` (dot-path) is accumulated as human-readable output.

## Example: override claude with new flags

```yaml
# ~/.mesnada/engines/claude-new.template.yaml
name: claude-new
description: "Claude CLI with updated 2026 flags"
command: claude
prompt_mode: arg
prompt_arg: "-p"
model_arg: "--model"
output_format: text
default_model: "claude-sonnet-4-6"
models:
  - id: "claude-sonnet-4-6"
    description: "Latest Sonnet"
args:
  - "--print"
  - "--output-format"
  - "text"
  - "--dangerously-skip-permissions"
env:
  NO_COLOR: "1"
```

## Example: new agent with JSONL output and dynamic env

```yaml
# ~/.mesnada/engines/my-jsonl-agent.template.yaml
name: my-jsonl-agent
description: "Custom agent with streaming JSONL output"
command: my-agent
prompt_mode: stdin
output_format: jsonl
jsonl_output_field: "delta.text"
jsonl_filter_field: "type"
jsonl_filter_value: "content_block_delta"
default_model: "my-agent-v1"
models:
  - id: "my-agent-v1"
    description: "Version 1"
args:
  - "--stream"
env:
  NO_COLOR: "1"
env_vars:
  - name: AGENT_MODEL
    value: "{{.Model}}"
  - name: AGENT_WORKDIR
    value: "{{.WorkDir}}"
```

## Implementation files

| File | Role |
|---|---|
| `internal/mesnada/agent/engine_template.go` | `EngineTemplate`, `EnvVar` structs; `LoadEngineTemplate`, `ScanEnginesDir` |
| `internal/mesnada/agent/spawner_template.go` | `TemplateSpawner` — full `Spawner` interface, `buildArgs`, `buildEnv`, JSONL parser |
| `internal/mesnada/agent/template_registry.go` | `TemplateRegistry` — scan, cache, `Get/Has/ListEngines`, `EnsureEnginesDirExists` |
| `internal/mesnada/agent/engine_template_test.go` | 14 unit tests covering load, scan, validation, env vars, dot-path, template expansion |
| `internal/mesnada/agent/manager.go` | Routes Spawn/Cancel/Pause/Wait/IsRunning/RunningCount/Shutdown through registry |
| `internal/mesnada/config/config.go` | `EnginesDir` field in `OrchestratorConfig` |
| `internal/config/config.go` | `EnginesDir` field in `MesnadaOrchestratorConfig` |
| `internal/app/app.go` | Maps Pando config → Mesnada config for `EnginesDir` |
| `internal/mesnada/orchestrator/orchestrator.go` | `EnginesDir` in `Config`; `ListCustomEngines()` method |
| `internal/llm/tools/mesnada.go` | `MesnadaSpawnTool.Info()` includes custom engines in enum + description |
| `internal/mesnada/server/tools.go` | `getToolDefinitions()` spawn_agent engine enum includes custom engines |
