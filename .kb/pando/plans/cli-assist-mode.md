# CLI Assist Mode — Implementation Plan

## Feature Description
Add `--cli-assist` flag to pando. When invoked, pando detects OS/shell, builds a focused prompt, calls a configured LLM to generate a shell command, and presents an interactive menu (no full TUI). The user can execute, edit, re-prompt, or quit. Execution streams stdout/stderr and propagates the exit code.

**Example usage:**
```
pando --cli-assist find all text files containing "hola" in all subdirectories of the current folder
```

---

## Phases

### Phase 1 — Config & CLI Flag
**Fact key:** `cli_assist_phase1_config_flag`

- Add `--cli-assist` bool flag to cobra in `cmd/root.go`
- Add `CLIAssistConfig` struct to `internal/config/config.go` (model, timeout)
- Add `AgentCLIAssist` agent name constant
- Route execution to `cliassist.Run()` when flag is set

### Phase 2 — OS & Shell Detection
**Fact key:** `cli_assist_phase2_sysinfo`

- New file: `internal/cliassist/sysinfo.go`
- `SysInfo` struct: `{OS, ShellPath, ShellName}`
- Detect via `runtime.GOOS` + `$SHELL` / `$COMSPEC` / `$PSModulePath` env vars
- Handles linux, macos, windows (cmd + powershell)

### Phase 3 — Prompt Builder
**Fact key:** `cli_assist_phase3_prompt_builder`

- New file: `internal/cliassist/prompt.go`
- `BuildSystemPrompt(SysInfo) string` — role-focused, OS/shell-aware, instructs model to return ONLY the command
- `BuildUserPrompt([]string) string` — joins args
- `CleanCommand(string) string` — strips markdown fences from model response

### Phase 4 — LLM Call (lightweight)
**Fact key:** `cli_assist_phase4_llm_call`

- New file: `internal/cliassist/llm.go`
- Uses `provider.NewProvider()` directly — no session, no agent loop, no tools
- Single `SendMessages()` call with system + user messages
- Timeout via `context.WithTimeout` using `cfg.CLIAssist.Timeout`
- Model priority: `cfg.CLIAssist.Model` → `cfg.Agents["coder"].Model`
- Optional spinner on stderr during LLM call

### Phase 5 — Interactive Menu
**Fact key:** `cli_assist_phase5_interactive_menu`

- New file: `internal/cliassist/menu.go`
- Displays command in a Unicode box (no BubbleTea fullscreen)
- Single-keypress capture via `golang.org/x/term` raw mode
- Keys: `[e]` Execute, `[p]` Edit prompt (re-fetch), `[c]` Edit command (re-show), `[q]` Quit
- Fallback to line-based input when stdin is not a TTY
- Edit command: pre-filled input line with current command

### Phase 6 — Command Execution & Exit Propagation
**Fact key:** `cli_assist_phase6_runner`

- New file: `internal/cliassist/runner.go`
- `RunCommand(SysInfo, command) int` — runs via `exec.Command(shell, "-c", cmd)`
- `cmd.Stdin/Stdout/Stderr = os.Stdin/Stdout/Stderr` — full passthrough (supports interactive commands)
- Captures exit code from `*exec.ExitError` and calls `os.Exit(code)`
- Windows: branches for `cmd.exe /C` vs `powershell.exe -Command`
- Echoes `$ command` to stderr before execution for clarity

---

## New Package Layout
```
internal/cliassist/
├── cliassist.go    # Run() entrypoint, main loop
├── sysinfo.go      # OS/shell detection
├── prompt.go       # Prompt builder + CleanCommand
├── llm.go          # LLM call (direct provider, no session)
├── menu.go         # Interactive menu (raw terminal)
└── runner.go       # Command execution + exit code
```

## Config additions
```toml
[CLIAssist]
  Model = "copilot.gpt-4o-mini"  # optional, falls back to coder model
  Timeout = 30                    # seconds
```

## Dependencies
- `golang.org/x/term` — raw terminal mode (likely already in go.mod via BubbleTea)
- No new external dependencies expected

## Implementation order
1 → 2 → 3 → 4 → 6 → 5 (can implement menu last; test with auto-execute first)
