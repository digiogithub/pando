---
created_at: 2026-07-17T08:17:05.727809661Z
updated_at: 2026-07-17T08:17:05.727809661Z
tags:
    - feature
    - webui
    - terminal
    - pty
    - xterm
    - websocket
    - security
    - pando
---
# WebUI interactive terminal: PTY over WebSocket + xterm.js (2026-07-17)

Implements [[pando/plans/webui_terminal_pty_parity_plan]]. Follow-up to
[[pando/fixes/webui_terminal_macos_interactive_shell]], which fixed the macOS
symptom; this removes the cause (no PTY at all).

## What changed

The WebUI terminal was a one-shot `POST /api/v1/terminal/exec` with no PTY: no
interactive programs, no live output, no job control. It now runs a real shell
in a PTY streamed over a WebSocket to xterm.js — the same model as the TUI
(`internal/tui/components/terminal/terminal.go`, `creack/pty`).

### Backend

- **`internal/api/terminal_pty.go`** (new) — `ptySession`: a shell in a PTY with
  a single reader goroutine (`pump`) that appends to a bounded replay buffer
  (`ptyReplayBytes` = 256KB) and fans out to subscribers. `ptyRegistry` keeps
  sessions alive across reconnects with a 30-min idle TTL reaper.
  `ptyShellCommand()` runs the shell interactive (`-i`) — correct here because a
  controlling terminal exists, unlike the PTY-less `/exec` path.
- **`internal/api/handlers_terminal_pty.go`** (new) — `GET /api/v1/terminal/pty`.
  Binary frames = raw PTY bytes both ways; JSON text frames = control
  (`ready` / `resize` / `exit` / `close`). `pumpPTYToClient` is the sole
  websocket writer (gorilla allows only one concurrent writer), with ping/pong
  keepalive. `checkPtyOrigin` allows same-origin plus loopback origins (so the
  Vite dev server on :5173 works) and rejects everything else.
- **`internal/api/routes.go`** — route registered.
- `gorilla/websocket` promoted from indirect to direct dependency.

### Frontend

- **`web-ui/src/services/terminalPty.ts`** (new) — `connectPty()`; token travels
  as `?token=` because the browser WebSocket API cannot set headers (the server's
  `authMiddleware` already accepts it). Also a tabId→connection registry so the
  store can terminate a shell on tab close without importing components.
- **`web-ui/src/components/terminal/TerminalPtyPane.tsx`** (new) — xterm +
  FitAddon, `onData` → socket, binary → `terminal.write`. Panes stay mounted
  when their tab is in the background (hidden via CSS) so shells keep running.
- **`web-ui/src/stores/terminalStore.ts`** — `mode: 'pty' | 'exec'`,
  `ptySessionId`, `attachPtySession` / `markPtyExited` / `fallbackToExec`. Tab
  ids + PTY session ids persist in `sessionStorage` so a reload reattaches;
  scrollback is not persisted (the server replays it).
- **`TerminalView.tsx`** — renders all PTY panes, keeps the legacy
  `TerminalOutput`+`TerminalInput` for `exec` fallback with an i18n notice;
  Clear sends Ctrl+L in PTY mode.
- i18n key `terminal.ptyUnavailable` in en/es/de/pt.

## Security decision (confirmed with user)

**No dangerous-command filter on the PTY path**, matching the TUI. A PTY takes
keystrokes, not commands: a filter is bypassed by aliases, scripts or a shell-out
from vim, so it would only give false assurance. The boundary is the auth token
plus the default `localhost` bind.

**This is a threat-model change**: `pando serve --host 0.0.0.0` now exposes an
interactive shell to anyone holding the token. Documented in the README feature
list. `POST /api/v1/terminal/exec` is kept as a fallback and **retains** its
`isDangerousShellCommand` filter (verified: still returns 403 on `rm -rf /`).

## Verification

- `go build ./...`, `go vet ./internal/api` — clean.
- `go test ./internal/llm/agent ./internal/api` — pass; PTY tests also pass under
  `-race -count=2`. New tests in `handlers_terminal_pty_test.go` drive a **real
  shell**: interactive echo, reattach-with-replay after a simulated reload,
  unknown session id starts a fresh shell, resize reaches the shell (`tput cols`
  → 132), origin table, replay-buffer bound.
- `cd web-ui && bun run typecheck && bun run lint` — clean (2 pre-existing
  warnings in `KeyValueEditor.tsx`).
- **End-to-end against a real `pando serve` (TLS + authMiddleware)** via a bun
  WebSocket client mimicking the browser contract: bad token rejected, shell
  echoes, raw ANSI escapes reach the client, reattach replays scrollback. The Go
  tests mount the handler directly and bypass auth/TLS, so this closed that gap.
- **NOT verified: the frontend in a real browser** (no browser automation
  available in the session). The xterm wiring, tab-switch survival and the
  reload-reattach UX are unexercised. Needs a manual pass — especially on macOS
  (mac-mini-de-digio), the original bug report.

## Known gaps / follow-ups

- No automated frontend test (`web-ui` has no test runner configured).
- On reattach the replay is raw bytes, so a full-screen app (`vim`) repaints only
  on its next redraw; a `Ctrl+L`-style nudge could improve it.
- PTY sessions are per-server-process and not shared across IPC instances.
