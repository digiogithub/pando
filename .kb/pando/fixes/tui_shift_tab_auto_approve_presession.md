---
created_at: 2026-07-25T00:27:21.503861237Z
updated_at: 2026-07-25T00:27:44.517106201Z
tags:
    - fix
    - tui
    - permissions
    - keybindings
---
# Fix: TUI shift+tab auto-approve no feedback before first message

Date: 2026-07-25

## Symptom
Pressing `shift+tab` in the TUI did nothing — no "auto mode" chip, no info message. Enabling auto-approve from Settings/config DID show the chip.

## Root cause
Sessions are created lazily (`ChatPageModel.ensureSession`, `internal/tui/page/chat.go:1654`) — only on the first message. Before that `a.selectedSession.ID == ""`. The `ToggleAutoApprove` handler guard (`internal/tui/tui.go`) required `a.selectedSession.ID != ""`, and `toggleAutoApprove()` early-returned nil when no session. So a shift+tab pressed before typing produced no state change and no feedback. The config path still showed the chip because it emits `core.AutoApproveMsg{SessionID: ""}` and the status component accepts an empty SessionID (`internal/tui/components/core/status.go:168`).

## Change
`internal/tui/tui.go`:
- New field `appModel.autoApproveOverride *bool` — remembers a user auto-mode choice made before any session exists (nil = use config default).
- Removed `a.selectedSession.ID != ""` from the `ToggleAutoApprove` key guard.
- `toggleAutoApprove()`: when a session exists, flips per-session state as before; when none, flips the desired state. Always records the intent in `autoApproveOverride` and emits `AutoApproveMsg` (SessionID may be "") + `ReportInfo("Auto-approve: on/off")`, so the chip and message update immediately.
- New helper `autoApproveDesired()` — prefers `autoApproveOverride` over `cfg.Permissions.AutoApproveTools`.
- `applyDefaultAutoApprove(sessionID)` (called on `chat.SessionSelectedMsg`): now seeds from `autoApproveDesired()` and both enables AND disables (previously only enabled), so the pre-session choice is applied when the session is finally created.

Net: `shift+tab` before the first message shows the auto-mode chip + info instantly, and the choice takes effect (auto-approve on the permission service) once the lazy session is created.

## Follow-up: chip missing at startup (same day)

After the change above the toggle worked but the user still never saw "auto mode".

Second root cause: with `[Permissions] AutoApproveTools = true` in the config, the auto-approve chip (`⏵⏵ auto-accept`) was only ever emitted from `applyDefaultAutoApprove`, which runs on `chat.SessionSelectedMsg` — i.e. after the lazily created session exists. At startup no `core.AutoApproveMsg` was sent at all, so the chip was hidden even though auto-approve was effectively ON. The first `shift+tab` therefore computed `enabled = !autoApproveDesired()` = **false** and reported `Auto-approve: off`, which looked like "the toggle does nothing / auto mode never appears".

Fix — `appModel.Init()` (`internal/tui/tui.go`, right after `a.status.Init()`):

```go
cmds = append(cmds, util.CmdHandler(core.AutoApproveMsg{Enabled: a.autoApproveDesired()}))
```

The status component already accepts `SessionID == ""`, so the chip renders from boot and the first `shift+tab` now toggles from the real visible state.

## Verification
- `go build ./internal/tui/...` and `go build .` — clean.
- `go vet ./internal/tui/` — clean.
- `go test ./internal/permission/` — ok.
- End-to-end: TUI driven in a real pty (`pty.fork`, 160x45) with `\x1b[Z` (shift+tab) injected. Before the Init fix: no `auto-accept` chip at boot, `Auto-approve: off` after the keypress. After the fix: `⏵⏵ auto-accept` chip present at boot, chip gone + `Auto-approve: off` message after the keypress.

## Debugging technique worth reusing
Driving the real TUI binary through a forked pty and injecting raw escape sequences (`\x1b[Z` for shift+tab) is a reliable way to verify TUI keybindings end-to-end without a human at the keyboard: capture the pty output before and after the keypress and grep the (ANSI-stripped) text for the expected chip/message.

Related: [[pando/features/tui_chat_copy_scroll]], [[pando/features/tui_icon_fallback]]
