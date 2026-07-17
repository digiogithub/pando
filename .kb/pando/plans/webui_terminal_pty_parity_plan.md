---
created_at: 2026-07-17T08:05:46.042596995Z
updated_at: 2026-07-17T08:05:46.042596995Z
tags:
    - plan
    - webui
    - terminal
    - pty
    - xterm
    - websocket
    - pando
---
# Plan: WebUI terminal PTY parity via WebSocket + xterm.js (2026-07-17)

Supersedes the frontend/backend phases of [[plans/web-ui-terminal-parity]].
Follow-up to [[fix_webui_terminal_macos_interactive_shell]], which fixed the
symptom (`-i` without a PTY) but not the architecture.

## Context

The WebUI terminal is a one-shot request/response `POST /api/v1/terminal/exec`
with no PTY. The TUI runs a real shell in a PTY (`creack/pty`). Consequences:
no interactive programs (`vim`, `htop`, `less`, `ssh`), no live output, no
`.zshrc` aliases, no cwd persistence across commands beyond a stored dir.

Already in place (do not rebuild):
- `web-ui` already depends on `@xterm/xterm` + `@xterm/addon-fit`.
- `TerminalOutput.tsx` already renders through xterm — but read-only
  (`disableStdin: true`) and it `reset()`s and replays all entries on every
  change.
- Terminal tabs already exist in `terminalStore.ts` / `TerminalView.tsx`.
- `gorilla/websocket v1.5.3` is already an indirect dependency.
- `authMiddleware` (`internal/api/server.go:176`) already accepts `?token=`,
  which is what a browser WebSocket needs (no custom headers allowed).

## Decisions (confirmed with user, 2026-07-17)

1. **Drop `isCommandDangerous` for the PTY path.** A real PTY makes it
   unenforceable: input arrives keystroke by keystroke, and a dangerous command
   can come from inside `vim`, an alias or a script. Parity with the TUI, which
   has no such filter. Security boundary = the auth token + the default
   `localhost` bind. This is a **threat-model change** and must be documented:
   binding `pando serve` to `0.0.0.0` now exposes an interactive shell to anyone
   holding the token.
2. **Keep `POST /api/v1/terminal/exec`** as a fallback for non-WebSocket
   clients and API consumers (SDK/scripts). PTY becomes the primary path.

## Phases

### P1 — Backend PTY session manager + WebSocket endpoint
- New `internal/api/terminal_pty.go`: `ptySession` (id, cmd, pty master,
  scrollback ring buffer, mutex, done chan) + a registry with idle TTL reaping.
- New `internal/api/handlers_terminal_pty.go`:
  `GET /api/v1/terminal/pty?session_id=&cols=&rows=`.
  - Upgrade with `CheckOrigin` restricted to same-origin.
  - Binary frames = raw PTY bytes both directions.
  - Text frames = JSON control: `{"type":"resize","cols":N,"rows":N}`.
  - Server → client `{"type":"exit","code":N}` on shell exit.
- Reattach: reconnecting with a known `session_id` replays the ring buffer, so
  a tab survives a page reload.
- Shell selection reuses config `Shell.Path`/`Shell.Args`, `$SHELL`, and runs
  interactive (`-i`) — legitimate here because there IS a controlling terminal.
- Promote `gorilla/websocket` to a direct dependency.
- Route in `routes.go`.

### P2 — Frontend interactive xterm
- Rewrite `TerminalOutput.tsx` into an interactive component: `disableStdin`
  false, `onData` → `ws.send(bytes)`, ws binary message → `terminal.write`.
- `FitAddon` + `ResizeObserver` → send `resize` control frames.
- Stop the reset/replay-everything effect; the PTY owns the screen state.
- One xterm instance + one WebSocket per tab, kept alive across tab switches
  (hide with CSS, don't unmount) so background tabs keep running.
- `TerminalInput.tsx` becomes unnecessary in PTY mode (the shell draws its own
  prompt); keep it only for the fallback path.

### P3 — Fallback + polish
- If the WebSocket fails to connect, fall back to the existing `/exec` +
  `TerminalInput` UI, with a visible notice.
- Close the session on tab close; reap on server side.

### P4 — Verify
- `go build ./...`, `go test ./internal/api`.
- `cd web-ui && bun run typecheck && bun run lint`.
- Manual: `vim`, `htop`, `ls --color`, Ctrl+C, resize, reload page.
- macOS check on mac-mini-de-digio (the original bug report).

## Acceptance criteria

- WebUI terminal runs `vim`/`htop` and renders colors.
- Ctrl+C interrupts; resize reflows.
- Multiple tabs, each an independent shell, surviving tab switches.
- Page reload reattaches to the running shell.
- `/exec` still answers for non-WebSocket clients.
