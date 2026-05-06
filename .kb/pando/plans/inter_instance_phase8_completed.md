# Inter-Instance Communication — Phase 8 Completed

**Date:** 2026-05-06  
**Status:** Complete

## Summary

Phase 8 extends the TUI Instances Browser (already scaffolded in previous phases) with
the remaining interactive features from the original plan:

1. **Remote message send** (`m` keybinding) — opens an inline `textinput` overlay at
   the bottom of the live-view pane that lets the user compose and submit a message to
   the selected remote session via `remoteview.RemoteControl.SendMessage()`.
2. **Remote session switch** (`s` keybinding) — activates the selected session on the
   remote instance via `remoteview.RemoteControl.SwitchSession()`.
3. **Transient status notifications** — after RPC operations (send, switch, interrupt)
   a status line appears in the header bar for 3 seconds then disappears naturally on
   the next render.
4. **Live-view keybind hints** — a dimmed help line at the bottom of the live view
   reminds the user of `m: send message`, `i: interrupt`, `tab: switch panel`.
5. **`m` also works from the live-view pane** — both the sessions pane and the live-
   view pane accept `m` to open the message dialog so users don't have to change focus.

## Files changed

| File | Change |
|---|---|
| `internal/tui/components/instances/instances.go` | Added `textinput` import; added `sendMessageResultMsg`, `switchSessionResultMsg` message types; added `showMsgInput`, `msgInput`, `statusLine`, `statusExpiry` fields to `Model`; initialized `textinput` in `New()`; added `sendMessageCmd()`, `switchSessionCmd()` functions; added `setStatus()` helper. |
| `internal/tui/components/instances/update.go` | Added `SendMsg` and `Switch` key bindings; added `sendMessageResultMsg` and `switchSessionResultMsg` handlers; added `handleMsgInput()` method; added `m`/`s` cases to `handleSessionsPane()`; converted live-view from read-only to `handleLiveViewPane()` with `m`/`i` support. |
| `internal/tui/components/instances/view.go` | Updated `renderHeader()` to accept a `status` string; computed transient status in `View()`; added `renderWithMsgInputOverlay()` for the inline input bar; added keybind hint row in `renderLiveViewPane()`. |

## Architecture notes

- The message input overlay replaces the last line of the rendered output so total
  terminal height stays stable (no flicker or resize).
- Status expiry is checked at render time (`time.Now().Before(m.statusExpiry)`) — no
  extra tick is needed.
- All RPC calls are made asynchronously via `tea.Cmd` functions so the TUI never
  blocks.
- The `handleMsgInput()` routing is checked first in `Update()` so that when the
  overlay is visible, normal pane navigation is suspended and all keystrokes go to the
  text field.

## What was already present before Phase 8

- Three-panel layout (instances list, session list, live-view) — implemented in
  Phase 7.
- `Ctrl+Alt+I` keybinding to open the page — implemented in Phase 7.
- Auto-refresh of instance list every 2 s via `tickMsg` — implemented in Phase 7.
- `i` interrupt keybinding — implemented in Phase 7.
- ZMQ PUB subscription for live streaming — implemented in Phase 7.
- REST API and Web-UI components — implemented in Phase 7 (web).
