---
created_at: 2026-06-12T07:24:10.559077837Z
updated_at: 2026-06-12T07:24:10.559077837Z
tags:
    - plan
    - mesnada
    - engines
    - custom-engines
    - template
---
# Custom Engine Templates for Mesnada

**Created:** 2026-06-12  
**Status:** In Progress

## Overview

Allow users to define new Mesnada engines via YAML template files placed in `<mesnada_config_dir>/engines/` (e.g., `~/.mesnada/engines/` or `.pando/mesnada/engines/`). These templates describe how to invoke an external CLI agent, how to pass the prompt, what output format to expect, and how to parse JSONL output.

This enables:
- Overriding existing engine args/flags when a tool has breaking changes
- Adding support for new CLI agents not yet compiled into Pando
- Custom output parsing for non-ACP agents that emit structured JSONL

## YAML Template Format

File: `<engines_dir>/<engine_name>.template.yaml`

```yaml
# Required fields
name: my-agent            # Engine ID used in spawn calls (no spaces)
description: "My custom AI agent CLI"
command: my-agent-cli     # Binary name (resolved via PATH) or absolute path

# How to pass the prompt to the process
prompt_mode: arg          # "arg" | "stdin"
prompt_arg: "-p"          # CLI flag for prompt (when prompt_mode=arg); default "-p"

# Model configuration
default_model: "my-model-v1"
models:
  - id: "my-model-v1"
    description: "Stable version"
  - id: "my-model-v2"
    description: "Latest version"

# How the model is passed to the CLI
model_arg: "--model"      # CLI flag for model; omit to not pass model

# Output format
output_format: text       # "text" | "jsonl"

# JSONL parsing (only when output_format=jsonl)
jsonl_output_field: "content"       # Dot-path to extract as human-readable output
jsonl_filter_field: "type"          # Optional: only process lines where this field...
jsonl_filter_value: "assistant"     # ...equals this value

# Fixed CLI args always prepended
args:
  - "--no-color"
  - "--non-interactive"

# Environment variables
env:
  MY_AGENT_LOG: "0"
  NO_COLOR: "1"
```

### Template variables available in `args` values:
- `{{.Model}}` — the model ID for this task
- `{{.WorkDir}}` — the task working directory
- `{{.TaskID}}` — the task ID
- `{{.LogFile}}` — the log file path

## Architecture

### New files

1. **`internal/mesnada/agent/engine_template.go`**
   - `EngineTemplate` struct (YAML deserialization)
   - `LoadEngineTemplate(path string) (*EngineTemplate, error)`
   - `ScanEnginesDir(dir string) ([]*EngineTemplate, error)` — scans for `*.template.yaml`

2. **`internal/mesnada/agent/spawner_template.go`**
   - `TemplateSpawner` struct — generic spawner driven by `EngineTemplate`
   - `TemplateProcess` struct — tracks running process
   - Implements full `Spawner` interface pattern (same as ClaudeSpawner)
   - Handles `prompt_mode: arg` (appends flag+prompt to args)
   - Handles `prompt_mode: stdin` (pipes prompt to process stdin)
   - Handles `output_format: text` — capture lines as-is
   - Handles `output_format: jsonl` — parse each line as JSON, extract `jsonl_output_field`, filter by `jsonl_filter_field`/`jsonl_filter_value`
   - Shares `openOrCreateLogFile` from `spawner.go`

3. **`internal/mesnada/agent/template_registry.go`**
   - `TemplateRegistry` struct
   - `NewTemplateRegistry(enginesDir string) (*TemplateRegistry, error)`
   - `LoadAll()` — scans dir and creates a `TemplateSpawner` per template
   - `Get(engineName string) (*TemplateSpawner, bool)`
   - `ListEngines() []string`
   - `ListEngineDetails() []EngineTemplateInfo` (for tool descriptions)

### Modified files

4. **`internal/mesnada/agent/manager.go`**
   - Add `templateRegistry *TemplateRegistry` field
   - Add `templateSpawners map[string]*TemplateSpawner` (keyed by engine name)
   - In `NewManager(...)`: accept `enginesDir string` param, call `NewTemplateRegistry`
   - In `Spawn`: after the switch/default, check `m.templateSpawners[string(engine)]`
   - In `Cancel/Pause/Wait/IsRunning/RunningCount/Shutdown`: same pattern
   - `ValidateEngine`: accept any engine name present in `templateSpawners`

5. **`internal/mesnada/config/config.go`**
   - Add `EnginesDir string` to `OrchestratorConfig`
   - Default: `filepath.Join(mesnadaDir, "engines")` (derived from `~/.mesnada/engines`)

6. **`internal/config/config.go`** (Pando config)
   - Add `EnginesDir string` to `MesnadaOrchestratorConfig`

7. **`internal/llm/tools/mesnada.go`**
   - Add `customEngines []string` field to `mesnadaTool`
   - Factory functions accept optional engine list
   - `Info()` for `MesnadaSpawnTool`: add custom engines to enum and description

8. **`internal/mesnada/server/tools.go`**
   - `getToolDefinitions()`: append custom engines from registry to the `engine` enum
   - `detectEngineForModel()`: also check template registry

9. **`internal/mesnada/orchestrator/orchestrator.go`**
   - Pass `enginesDir` through to `NewManager` call

10. **`internal/app/app.go`** (Pando app wiring)
    - Pass `cfg.Mesnada.Orchestrator.EnginesDir` to orchestrator/manager

## Implementation Phases

### Phase 1: Data structures + YAML loader
- Define `EngineTemplate` struct with all YAML fields
- Implement `LoadEngineTemplate` and `ScanEnginesDir`
- Unit test: load a sample template file

### Phase 2: TemplateSpawner
- Implement `TemplateSpawner` with full Spawner interface
- Handle `prompt_mode: arg` and `prompt_mode: stdin`
- Handle `output_format: text` and `output_format: jsonl`
- JSONL parser: dot-path field extraction, filter support
- Unit test: test both prompt modes and output formats

### Phase 3: TemplateRegistry
- Implement registry that scans and caches spawners
- `ListEngines()` for tool description generation

### Phase 4: Manager integration
- Wire registry into Manager constructor
- Route unknown engine names through template registry in all lifecycle methods

### Phase 5: Config + EnginesDir wiring
- Add `EnginesDir` to both Mesnada and Pando config structs
- Wire through orchestrator → manager
- Auto-create engines dir if it doesn't exist (with a README)

### Phase 6: Tool description updates
- Update `spawn_agent` enum in both `mesnada.go` and `tools.go` to include custom engines
- Add custom engines to `ValidateEngine`
- Startup log listing loaded custom engines

## Key Design Decisions

- **Engine name = filename stem**: `my-agent.template.yaml` → engine name `my-agent`
- **Template `name` field overrides filename** if present; filename is fallback
- **Hot reload**: not in scope; templates are loaded once at startup
- **JSONL dot-path**: simple dot notation (`content.text`), no array indexing in v1
- **No MCP config support** for template engines in v1 (they can get it via env/args)
- **Template engines appear in `ValidEngine`** check only when registry is loaded
- **Pando engines dir**: derived from `cfg.Mesnada.Orchestrator.LogDir` parent if `EnginesDir` not set

## Example template: override `claude` engine with new flags

File: `~/.mesnada/engines/claude-new.template.yaml`

```yaml
name: claude-new
description: "Claude CLI with new 2026 flags"
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

## Example template: new agent with JSONL output

File: `~/.mesnada/engines/my-jsonl-agent.template.yaml`

```yaml
name: my-jsonl-agent
description: "Custom agent that emits JSONL"
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
  MY_AGENT_KEY: ""
```
