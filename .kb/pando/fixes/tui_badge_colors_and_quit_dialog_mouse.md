---
created_at: 2026-07-24T13:51:10.774776133Z
updated_at: 2026-07-24T13:51:10.774776133Z
tags:
    - fix
    - tui
    - theme
    - dialog
    - mouse
---
# Fix: TUI badge contrast + clickable quit dialog buttons

Date: 2026-07-24

## Problem

Two TUI polish issues reported by the user:

1. The floating chat affordances added with the copy/scroll feature — the
   "▼ jump to bottom (ctrl+end)" button and the "✓ copied" clipboard
   confirmation badge — were rendered with `Foreground(t.Background())` over
   `Background(t.Primary())`. On themes where the background is light (the
   `light` theme in particular) that produced near-white text on a light
   primary surface, i.e. unreadable. The correct source of truth is the
   theme's badge-text color, which is black-ish on dark themes and white on
   the light theme.
2. The quit confirmation dialog ("Are you sure you want to quit?") had
   Yes/No buttons that were keyboard-only: no mouse zone was registered, so
   clicking them did nothing.

## Changes

### `internal/tui/components/chat/list.go`

In `chatViewportView()`, both overlays now use the theme's badge foreground:

- the `MarkChatScrollBottom` button (jump-to-bottom / "✓ copied" label)
- the standalone "✓ copied" badge rendered when the viewport is already at
  the bottom

`Foreground(t.Background())` → `Foreground(t.BadgeText())`.

`Theme.BadgeText()` (`internal/tui/theme/theme.go`) already existed for
exactly this purpose: it returns `BadgeTextColor` when a theme sets one and
falls back to `BackgroundColor` otherwise. Only `light.go` sets it
(`#ffffff`), so every dark theme keeps the previous dark-on-primary look and
the light theme finally gets readable white-on-primary. Same helper is
already used by permission/init/models/session dialogs, goal_status and the
editor viewer, so this makes the chat overlays consistent with the rest of
the UI.

### `internal/tui/zone/zone.go`

Added two zone ids next to the existing permission ones:

```go
QuitYes = "quit-yes"
QuitNo  = "quit-no"
```

### `internal/tui/components/dialog/quit.go`

- Imported `tuizone "github.com/digiogithub/pando/internal/tui/zone"`.
- The Yes/No buttons are now wrapped with
  `tuizone.MarkDialogButton(tuizone.QuitYes/QuitNo, ...)`. bubblezone markers
  are ANSI-escape based and therefore zero-width for lipgloss, so the
  existing `lipgloss.Width(buttons)` right-alignment math is unaffected.
- Added a `case tea.MouseMsg` to `Update`: when the pointer is inside a
  button zone the selection follows it, and on
  `MouseActionPress` + `MouseButtonLeft` the button acts — Yes returns
  `tea.Quit`, No returns `util.CmdHandler(CloseQuitMsg{})`. Mirrors the
  pattern already used by `permission.go`.

Note: the app enables `tea.EnableMouseCellMotion` (not all-motion), so plain
hover is not reported; motion only arrives while a button is held. The
selection-follow is therefore a drag-time nicety, the click is the real fix.

## Why the mouse event reaches the dialog

`appModel.handleMouse` returns `handled = false` as soon as any modal
(`a.showQuit`, …) is open, so the `tea.MouseMsg` case in `appModel.Update`
does not return early and execution falls through to the
`if a.showQuit { a.quit.Update(msg) }` block. Only `tea.KeyMsg` is blocked
from propagating further, so no extra wiring was needed. The quit dialog is
composed with `layout.PlaceOverlay` exactly like the permission dialog, whose
zones already work, so the zone markers survive to the final `zone.Scan`.

## Verification

- `go build ./...` — clean.
- `go vet ./internal/tui/...` — clean.
- `go test ./internal/tui/...` — all packages pass (chat, dialog, core,
  filetree, settings, snapshots, page, styles, theme).

## Related

- [[feature_tui_chat_copy_scroll]] — the feature that introduced the badges
  fixed here.
- [[pando/features/tui_chat_copy_scroll]]
