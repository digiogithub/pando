---
created_at: 2026-07-23T12:44:00.89871038Z
updated_at: 2026-07-23T13:08:07.587252594Z
tags:
    - fix
    - tui
    - chat
    - clipboard
    - selection
---
# TUI chat copy/selection follow-up fixes (2026-07-23)

Follow-up to [[tui_chat_copy_and_scroll_to_bottom]] after user testing on WezTerm
+ COSMIC.

## Round 1 problems + fixes
- No visual selection feedback → `applySelectionHighlight()` overlays plain
  segments styled with `theme.SelectionBackground()/SelectionForeground()` over
  the selected column range of each visible line (via `layout.PlaceOverlay`),
  live while dragging and after finalize.
- Copy button copied the whole rendered chat + button glyphs → the click was not
  always delivered as `MouseActionPress`, fell through to the selection state
  machine, and the auto-copy-on-release copied ansi-stripped `contentLines`.
  Fix: copy button + jump-to-bottom resolved on BOTH press and release (return
  early), copy fires on release only using `message.Content().String()` (stored
  session text, no glyphs); release finalize guarded by `m.mouseDown`.

## Round 2 problem — selection copy came out EMPTY
User: highlight now visible, but right-click / Ctrl+Shift+C copied nothing
(clipboard empty).

Root cause: **Ctrl+Shift+C is reserved by WezTerm and COSMIC Terminal for their
own native copy** — it never reaches the app; the terminal copies its (empty)
native selection, so the paste is blank. Keyboard copy is therefore unreliable
here. `selectedText()` itself is correct (unit test `selcopy_test.go`:
`world\nsecond`).

Fix (internal/tui/components/chat/list.go):
- **Re-added copy-on-release** of the clean, column-sliced selection, but ONLY
  for a genuine drag (`m.mouseDown` + actual movement) — a plain click or a
  release after a button press never copies. This is the dependable path (no
  terminal-reserved key needed); OSC 52 from `copyToClipboard` does the actual
  copy and works on WezTerm/COSMIC.
- **Ctrl+Shift+C** kept but now uses the same `selectedText()` (column-precise)
  instead of joining whole `contentLines`; works only where the terminal
  delivers the key.
- **Right-click** on an active selection still copies.
- `selectedText()` now `TrimRight`s each line's trailing spaces so the padded
  `contentLines` width padding is not copied.

## UX summary
- Drag to select → auto-copies clean selection on release (+ `✓ copied` badge).
- Right-click on selection → copy. Ctrl+Shift+C → copy where delivered.
- Per-message ` copy` button → copies that block's stored response text.
- Ctrl+C stays reserved for the quit dialog.

## Verification
`go build ./...` clean; `go test ./internal/tui/components/chat/ ./internal/tui/page/` ok
(incl. `TestSelectedTextBasic`).

Related: [[tui_chat_copy_and_scroll_to_bottom]].
