---
created_at: 2026-09-18T11:01:36.652592371Z
updated_at: 2026-09-18T11:01:36.652592371Z
tags:
    - feature
    - sandbox
    - security
    - bash
    - landlock
    - seatbelt
---
# Host command sandbox, on by default (PANDO-EP-0009, 2026-09-18)

Epic PANDO-EP-0009 (stories PANDO-US-0040..0049, all done). Design and Grok Build comparison: [[pando/analysis/grok-build-sandbox-research.md]]. Backlog context: [[pando/changes/backlog-grok-build-sandbox-and-memory-epics.md]]. User docs: `docs/sandbox.md`, `docs/sandbox-coverage.md`.

## What it does
Agent-driven host processes run confined, per spawn (Pando itself stays unconfined, so toggles apply live). Default: `workspace-write` mode (write workspace + temp + dependency caches, read everywhere), network allowed, bash auto-approved only while protection is FULL. Modes: workspace-write, read-only, strict, off. Protected paths: `.pando`, `.pando.toml/.json`, `.git/hooks`, `.git/config`, global config dirs, data dir. Env scrubbing removes `*_API_KEY`, `*TOKEN*`, `*SECRET*`, `*PASSWORD*`.

## Architecture
- `internal/sandbox`: Policy/Mode/Network, `Resolve` (precedence: enterprise lock > `PANDO_SANDBOX` env > project config (may only tighten) > global > default), `ScrubEnv`, `Wrapper` per OS, `WrapCmd(cmd, purpose)`, `Current/CurrentPolicyHash/Active/AutoAllowBash`, `Guarantees(p,c)` full vs partial, `Classify` denial detection (localized glibc messages), events (`Emit`, JSONL `<data>/sandbox-events.jsonl`, counters in `pando_stats`, slog → logs pages + opt-in telemetry).
- Linux: `internal/sandbox/helper` leaf pkg dispatched from `init()` on `__sandbox-exec` (spec via memfd fd): no_new_privs, raw-syscall Landlock (best ABI), hand-rolled seccomp (namespace lockdown, TIOCSTI, optional network), bwrap prefix when available (ro-binds protected paths, pins ancestors with `--bind X X` so `mv .git` → EBUSY, masks D-Bus/systemd/container sockets). Landlock-only fallback = partial (deep protected paths not enforced) → bash keeps prompting.
- macOS: `sandbox-exec` + generated SBPL (`sbpl.go`, params via `-D`, ancestor rename guards, guarded-port denies). NOT yet run on real Mac → PANDO-US-0050.
- Windows: not enforced; Job Object `AttachProcessTree` for tree cleanup.
- Guarded ports: `internal/sandbox/portguard` registry; every Pando listener (API, AG-UI, MCP HTTP, LLM proxy, IPC bus + primary ports, design preview, OAuth callbacks, browser CDP) registers; mirrored to `~/.config/pando/run/ports/<pid>.json` so other Pando instances are covered. Linux Landlock ABI ≥4 (kernel ≥6.7) blocks connect to those ports (65 534 allow rules, ~40 ms per wrapped spawn); macOS SBPL denies them. Without it → partial.
- Integration: persistent shell respawns on policy hash change, process-group kill when a launcher is in front (`internal/llm/tools/shell`); bash escalation `sandbox_permissions: "require_escalated"` + `justification` → `execute_unsandboxed` permission with `NeverAutoApprove` (never auto-granted by yolo/goal/auto-approve/AG-UI/ACP always-allow unless `Sandbox.AllowAutoEscalation`); ACP terminals and skills wrapped by default, MCP stdio and sub-agents opt-in (`extendTo`, `MCPServer.Sandbox`); `internal/procgroup`.
- Settings: `/api/v1/config/sandbox` (GET/PUT, 409 on locked keys), TUI Settings > Sandbox + footer/sidebar badge, WebUI `SandboxSettings.tsx` + badge; `pando_setup` refuses sandbox writes; `UpdateSandbox` writes the global file.
- CLI: `pando sandbox status [--json]`, `pando sandbox exec -- cmd`.

## Security review
Adversarial review found two critical escapes, both fixed and re-verified by running the attacks: (1) sandboxed curl fetched the API token over loopback and PUT `disabled:true` → fixed by guarded ports; (2) `mv .git` + recreate + plant hook → fixed by ancestor pinning. Residual (documented): `git init` + hook where no `.git` exists yet; port-number granularity; background processes keep old policy; signals to Pando = DoS only; ACP client-terminal bash, Lua, LSP unsandboxed.

## Verification
`go build ./...`, `go vet ./...`, `go test ./internal/... ./cmd/ -count=1` all pass; `-race` on sandbox/shell/permission; real Linux e2e (Landlock ABI 9 + bwrap 0.11) incl. attack reproductions; `python3 -m unittest tests/test_sandbox_cli.py` 15/15; `bun run build` web-ui; cross-OS vet darwin/windows for sandbox pkgs. CI: `.github/workflows/sandbox-e2e.yml` (ubuntu + macos), not yet run on GitHub.

## Side fixes
Nil `*Config` on comment-only `.pando.toml` crash; crash exit code 2 (`cmd/entrypoint.go`); IPC `primaryResponds` treats EACCES/EPERM as alive; `GrantPersistant` stores grant before answering; WebUI permission dialog strings moved to i18n. Follow-ups: PANDO-US-0050 (macOS real hardware), PANDO-T-0007 (savings ledger channel race).
