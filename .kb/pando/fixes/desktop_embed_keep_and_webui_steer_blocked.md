---
created_at: 2026-08-19T09:16:31.004278502Z
updated_at: 2026-08-19T09:16:31.004278502Z
tags:
    - fix
    - build
    - macos
    - desktop
    - webui
    - steering
    - pando
---

# Fix: `make desktop-embed` deletes Pando.app/.keep placeholder + WebUI blocked mid-run feedback

Date: 2026-08-19

## Problem 1 — `internal/desktop/bin/Pando.app/.keep` disappears

`make desktop-embed` (Makefile:110-129) always did `rm -rf internal/desktop/bin/Pando.app`
before delegating to `scripts/embed_desktop_artifact.py`. On non-macOS build hosts (or any
build where `desktop/build/bin/Pando.app` doesn't exist, e.g. only `pando-desktop` binary is
produced), the script copied `pando-desktop` instead and never recreated the `Pando.app`
directory. Since `internal/desktop/bin/Pando.app/.keep` is the only thing keeping that empty
directory tracked in git (needed by `go:embed` and by the osx build tooling that expects the
dir to pre-exist), any `make desktop-embed` run on Linux permanently deleted the tracked
placeholder from the working tree (`D internal/desktop/bin/Pando.app/.keep` in git status).

Only `desktop-clean` (Makefile:136-141) recreated the placeholder; `desktop-embed` did not.

### Fix
`scripts/embed_desktop_artifact.py`: added `restore_app_placeholder()`, called whenever the
script does NOT copy a real `Pando.app` bundle (the `pando-desktop`/`.exe` file-copy branch,
and the not-found error branch). Recreates `Pando.app/.keep` so the directory stays present
for `go:embed` and for a subsequent osx build.

Also manually restored `internal/desktop/bin/Pando.app/.keep` in the working tree (it had been
deleted by a prior `make` run, per `git status` at session start).

## Problem 2 — WebUI can't send mid-run feedback (steering)

Backend (`internal/llm/agent/agent.go` `Service.Steer`/`PendingSteering`/`drainSteeringInto`),
the `POST /api/v1/sessions/{id}/steer` handler (`internal/api/handlers_chat.go`), and the WebUI
hook (`web-ui/src/hooks/useChat.ts` `steer()`/`sendMessage()` routing) were all correctly wired
— this is the same "agent loop steering" feature already documented in
`pando/features/agent_loop_steering.md` (complete since 2026-06-16) and confirmed working in
TUI/ACP.

Root cause was purely in `web-ui/src/components/chat/ChatInput.tsx`:
- `handleSend()` had `if (!text || streaming || disabled) return` — blocked submission
  entirely while `streaming` was true, so `onSend`/`sendMessage`/`steer()` was never reached
  from the Enter key.
- The action-button area rendered EITHER the Stop button OR the Send button (ternary on
  `streaming`), so while the agent was running there was no Send button in the DOM at all —
  clicking to queue feedback was impossible even ignoring the Enter-key guard.

### Fix
- Removed `streaming` from the `handleSend` early-return guard.
- Changed the action-button area to render Send always, and Stop additionally when `streaming`
  is true (both visible side by side), so clicking Send while the agent is busy now reaches
  `onSend` → `useChat.sendMessage` → `steer()` (queues via `POST .../steer`), exactly like
  Enter already does.

### Verification
- `cd web-ui && npx tsc --noEmit` — clean.
- `go build ./...` — clean.
- Not yet manually clicked through in a running browser session; TUI/ACP steering already
  covered by existing agent-level tests (`internal/api/handlers_steer_test.go`,
  `internal/llm/agent` tests).
