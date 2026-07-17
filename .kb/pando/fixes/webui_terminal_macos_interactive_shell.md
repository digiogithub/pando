---
created_at: 2026-07-17T08:01:58.580275093Z
updated_at: 2026-07-17T08:01:58.580275093Z
tags:
    - fix
    - macos
    - webui
    - terminal
    - shell
    - pando
---
# WebUI terminal broken on macOS (2026-07-17)

## Problem

On macOS, commands typed in the WebUI terminal panel produced no output / hung.
The TUI terminal worked fine on the same machine.

## Root cause

The two surfaces spawn the shell very differently:

- **TUI** — `internal/tui/components/terminal/terminal.go:210` starts the shell
  through a real PTY (`creack/pty` `pty.StartWithSize`). The shell gets a
  controlling terminal, so interactive mode is legitimate.
- **WebUI** — `internal/api/handlers_terminal.go` `handleTerminalExec` runs a
  one-shot `exec.CommandContext(shell, "-i", "-c", cmd)` with **no PTY** and
  stdin detached.

`choosePreferredShell()` prefers `$SHELL`, which is `/bin/zsh` on macOS. So the
WebUI ran `zsh -i -c "cd <dir> && <cmd>"` with no controlling terminal. An
interactive shell without a controlling terminal tries to take job control
(`tcsetpgrp`) and is stopped by `SIGTTOU`; it then hangs until the 30s
`terminalExecTimeout` and returns empty output. `-i` also sources the full
`~/.zshrc` (oh-my-zsh, powerlevel10k instant prompt), which assumes a tty.

Why Linux hid it: `$SHELL` is usually bash, and `bash -i` without a controlling
terminal degrades instead of dying — but it still polluted the WebUI output with
`bash: cannot set terminal process group (-1)` / `bash: no job control in this shell`.

Verified locally (Linux, bash):

    setsid --wait bash -i -c 'echo HOLA'  # job-control warnings prepended to output
    setsid --wait bash -l -c 'echo HOLA'  # clean

## Fix

`internal/api/handlers_terminal.go`:

- New `defaultShellExecArgs()` returning `{"-l", "-c"}`, replacing the two
  hardcoded `{"-i", "-c"}` defaults in `resolveShellFromConfig()` and
  `shellCommandForExec()`. `-i` buys nothing in a request/response terminal (no
  prompt, no job control) while `-l` still sources the login profile so PATH
  matches a real terminal.
- New `ensureCommandFlag(args)` — appends `-c` when the user's configured
  `Shell.Args` only carries profile flags (e.g. `-l`). Without it, a configured
  `Shell.Args = ["-l"]` made the command string be treated as a script name
  instead of a command.

Trade-off: non-interactive `zsh -l` reads `.zprofile`/`.zlogin` but not
`.zshrc`, so user aliases defined in `.zshrc` are not available in the WebUI
terminal. The TUI PTY terminal is unaffected and keeps full interactive
behaviour.

## Files touched

- `internal/api/handlers_terminal.go` — `defaultShellExecArgs`, `ensureCommandFlag`,
  `resolveShellFromConfig`, `shellCommandForExec`.
- `internal/api/handlers_terminal_test.go` — new.

## Verification

- `go build ./internal/api` — OK.
- `go test ./internal/api` — OK. New tests: `TestDefaultShellExecArgsAreNotInteractive`,
  `TestEnsureCommandFlag`, `TestShellExecWithoutPTY` (runs the real
  `shellCommandForExec()` selection with stdin detached and asserts it completes
  before the deadline).
- NOT yet confirmed on macOS hardware — the root cause is inferred from the
  code path plus a Linux reproduction of the job-control mechanism. Needs a
  manual check on mac-mini-de-digio: open WebUI terminal, run `ls`.

## Cross-references

- [[plans/web-ui-terminal-parity]] — earlier plan for TUI/WebUI terminal parity;
  the real fix for full parity is giving the WebUI a PTY too, not just flags.
- [[fix_macos_wasm_hardened_runtime_jit_kill]] — different macOS-only failure, same repo.
