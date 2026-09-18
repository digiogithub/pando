---
id: PANDO-EP-0009
type: epic
title: Host command sandbox, on by default and container-free
status: backlog
priority: high
labels: [security, sandbox, bash, grok-build]
created: 2026-09-18T08:36:24Z
updated: 2026-09-18T08:36:24Z
---

## Description

Pando runs agent-generated shell commands directly on the developer's host, with the full privileges of the user. Only a first-word banned list and approval prompts limit this. The epic adds an OS-native sandbox:
- Linux: Landlock + seccomp, with bwrap when available
- macOS: Seatbelt through `sandbox-exec`
- Windows: graceful degradation with Job Object containment

It confines commands the agent executes to the workspace and temp dirs, protects Pando's own configuration and git hooks, optionally blocks child network, and lets the user approve one-off unsandboxed runs when a command legitimately needs more access. The sandbox is on by default, applies per spawned process (Pando itself stays unconfined), and can be switched off or tuned from TUI settings, WebUI settings and config. The design follows Grok Build's `xai-grok-sandbox` crate (profiles, protected paths, seccomp set, event log), adapted to Go and to per-spawn wrapping.

The sandbox is **on by default** and can be switched off or tuned from the TUI and WebUI settings screens (and `[Sandbox]` in config). No containers are involved; when `Container.Runtime` resolves to docker or podman the host sandbox does not apply.

### What Grok Build does (reference, `/www/MCP/Pando/grok-build`, crate `crates/codegen/xai-grok-sandbox`)

- Delegates enforcement to the `nono` crate (=0.53.0): Landlock on Linux, Seatbelt on macOS, nothing on Windows (`src/lib.rs:208-240`).
- Confines its **whole process once, irreversibly, at startup** (`xai-grok-shell/src/config/mod.rs:1431-1588`); children inherit it. Off by default; no per-command escalation.
- Linux extras: bubblewrap re-exec for read-only / unreadable paths inside allowed trees (`src/lib.rs:281-368`); per-child seccomp network filter and namespace-lockdown filter (`src/child_net.rs:178-216`); D-Bus / systemd / docker socket masks (`src/runtime_sockets.rs`).
- Profiles `workspace`, `devbox`, `read-only`, `strict`, `off`, custom via `sandbox.toml` (`profiles.rs:69-499`); its own config, trust and hook files are write-denied so the agent cannot disable the sandbox.
- With `auto_allow_bash`, bash is auto-approved only while the sandbox is enforced, still subject to a forced-prompt floor (`xai-grok-workspace/src/permission/manager/mod.rs:390,1109-1125`).
- Violations are inferred from EACCES/EPERM and logged to `~/.grok/sessions/sandbox-events.jsonl`; the model gets no hint for blocked bash.

### Pando design (differences from Grok)

- **Per-spawn wrapping**, not process-wide: Pando itself (DB, KB, WebUI, desktop) stays unconfined and the toggle applies live. Linux: hidden helper `pando __sandbox-exec` (policy on fd 3; no-new-privs, Landlock best-effort, seccomp namespace lockdown and optional network filter, then exec), with bwrap read-only binds for protected paths when available. macOS: `/usr/bin/sandbox-exec` with a generated SBPL profile (network restriction actually enforced, unlike Grok). Windows: honest "not enforced" + Job Object; WSL counts as Linux.
- **Modes**: `workspace-write` (default: write workspace, temp and dependency caches; read everywhere; child network allowed), `read-only`, `strict`, `off`.
- **Protected paths** (read-only even inside the workspace): `.pando.toml`, `.pando/`, `$HOME/.pando.toml`, global config dir, `.git/hooks`, `.git/config`, plus user `DenyPaths`. Env scrubbing of `*_API_KEY`, `*TOKEN*`, `*SECRET*`.
- **Codex-style escalation** (Grok has none): denial classifier appends a `[sandbox]` hint; bash gains `sandbox_permissions: "require_escalated"` + `justification`, raising an explicit-approval `execute_unsandboxed` permission request. Auto-approve and goal mode never grant it.
- **Precedence**: enterprise locked key > `PANDO_SANDBOX` env > project config (may only tighten) > global config > default. `pando_setup` cannot write `sandbox.*`.

Host spawn sites in Pando today: bash tool persistent `$SHELL -l` (`internal/llm/tools/bash.go:307-507`, `internal/llm/tools/shell/shell.go:47-130`, inherits full `os.Environ()`), ACP terminals served to sub-agents (`internal/mesnada/acp/client.go:401`), skill CLI tools (`internal/skills/tool_bridge.go:111`), "embedded" runtime (`internal/runtime/embedded_runtime.go:258`), MCP stdio (`internal/mcpclient/client.go:65`), mesnada spawners, Lua `cmd`/`sh`, LSP. User terminals (`internal/api/terminal_pty.go`, TUI terminal) and `cliassist` are never sandboxed.

## Acceptance Criteria

- [ ] On Linux (kernel ≥5.13) and macOS, a fresh install runs the bash tool sandboxed in `workspace-write` mode without any user action.
- [ ] Under the default mode, a sandboxed command cannot write outside the workspace, temp dirs or allow-listed caches, and cannot modify `.pando.toml`, `.pando/` or `.git/hooks`. This is verified by e2e tests on both OSes.
- [ ] The sandbox can be disabled or its mode changed from TUI settings, WebUI settings, and `[Sandbox]` in config. The change takes effect on the next command without restarting Pando.
- [ ] The agent cannot disable or loosen the sandbox (not through `pando_setup`, project config, or editing config files from a sandboxed command).
- [ ] A command blocked by the sandbox returns an explanatory hint to the model. An escalated re-run requires explicit user approval in TUI, WebUI and ACP permission flows.
- [ ] On unsupported platforms (Windows, old kernels), Pando shows "sandbox unavailable", keeps permission prompts, and logs the reason once.
- [ ] Sandbox status, denials and escalations are logged (slog plus optional telemetry, with commands redacted) and shown as a status badge.
- [ ] Docs and a KB entry are added. `go test ./internal/llm/agent ./internal/api ./internal/sandbox/... ./internal/llm/tools/...` passes.

## Notes

Children: 10 stories, 52 points. Critical path: core (1) → Linux / macOS / Windows backends and settings UI in parallel → bash + shell integration → escalation → other spawn sites and observability → tests, CI and docs.

Optional follow-ups not estimated here: network allow-list proxy (~8 pts), Windows AppContainer spike (~5 pts), sandboxing the Lua `sh`/`cmd` modules (~3 pts).

Risks: real workflows breaking under `workspace-write` (global npm, `~/.gitconfig`, docker socket) — mitigated by cache allow-list, escalation and one-click Off; Ubuntu 24.04+ AppArmor may block unprivileged bwrap (weaker fallback); kernels without Landlock get no enforcement; Apple has deprecated `sandbox-exec` (still used by Codex, Claude Code, Gemini CLI); denial detection from output is heuristic; ACP-client bash and Lua remain uncovered.

Full research with file:line citations for both repositories: KB `pando/analysis/grok-build-sandbox-research.md`. Motivated by the user's external comparison of Grok Build features.
