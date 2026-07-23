---
created_at: 2026-07-23T11:33:18.77423647Z
updated_at: 2026-07-23T12:31:55.819032995Z
tags:
    - feature
    - tui
    - chat
    - clipboard
    - scroll
    - osc52
---
# TUI chat: selection copy, per-message copy button, jump-to-bottom (2026-07-23)

## What was added
Three chat-panel usability features in the TUI messages component.

1. **Selection copy** — drag-select text in the chat viewport, then copy with
   **Ctrl+Shift+C** or **right-click**. `Ctrl+C` is intentionally NOT bound to
   copy: it is reserved for the app-level quit dialog (per user). Left-button
   drag still auto-copies on release.
2. **Per-message copy button** — a clickable ` <doc-icon> copy` row rendered
   directly under each finished assistant answer block. Clicking it copies that
   message's raw text (`message.Content().String()`).
3. **Jump-to-bottom** — while the viewport is scrolled up, a centered floating
   badge `▼ jump to bottom (ctrl+end)` overlays the lower chat area; clicking it
   or pressing **Ctrl+End** scrolls to the bottom. A `✓ copied` confirmation
   badge reuses the same overlay slot for ~2s after any copy.

## Two root-cause bugs fixed
1. **Dead `chat-viewport` zone.** `MarkChatViewport` (zone `chat-viewport`) was
   **defined but never called**, so the pre-existing mouse-selection code in
   `list.go` (and the focus check at `tui.go` `InBounds(ChatViewport,...)`) never
   registered a zone → selection was dead since it was written.
   `messagesCmp.chatViewportView()` now wraps the composed viewport (mark applied
   LAST, after the overlay, so it always wraps the final string). Verified via a
   throwaway test that both the outer viewport zone and the inner per-message
   copy-button zone survive the `viewport.View()` + `lipgloss` MaxWidth/MaxHeight
   pipeline once the async bubblezone `zoneWorker` drains.
2. **Clipboard silently failed on Wayland/SSH.** `atotto/clipboard.WriteAll`
   needs an OS helper (`xclip`/`xsel`/`wl-copy`/`pbcopy`); on the user's COSMIC
   (Wayland) session none were installed, so every copy errored and — because
   feedback was gated on success — there was NO visible effect for either the
   button or selection. Fix: new `copyToClipboard()` makes a best-effort
   `clipboard.WriteAll` AND always emits an **OSC 52** escape
   (`\x1b]52;c;<base64>\a`, tmux-wrapped when `$TMUX` set) straight to
   `os.Stdout` via `github.com/aymanbagabas/go-osc52/v2` (already an indirect dep
   of bubbletea, promoted to direct). OSC 52 needs no helper binary and works
   over SSH / Wayland; WezTerm and COSMIC Terminal honor it. `copyAndFeedback()`
   now flips the `✓ copied` indicator unconditionally for non-empty text.

## Files / symbols touched
- `internal/tui/zone/zone.go` — `ChatCopyPrefix`, `ChatScrollBottom`;
  `ChatCopyID`, `MarkChatCopy`, `MarkChatScrollBottom`.
- `internal/tui/components/chat/message.go` — `renderAssistantMessage` appends a
  `MarkChatCopy(msg.ID, ...)` button uiMessage after the answer block.
- `internal/tui/components/chat/list.go` — `copyToClipboard` (OS helper + OSC 52),
  `copyAndFeedback`; mouse handlers for `ChatScrollBottom` / per-message
  `ChatCopyID` zones + right-click copy; `ctrl+end` GotoBottom; selection copy on
  `ctrl+shift+c` only; `chatViewportView()` (zone mark + jump-to-bottom / copied
  overlay via `layout.PlaceOverlay`).
- `go.mod` — `github.com/aymanbagabas/go-osc52/v2` promoted to direct.

## Verification
- `go build ./...` clean; `go test ./internal/tui/components/chat/ ./internal/tui/page/` ok.
- OSC 52 sequence checked: `osc52.New("hola mundo").String()` →
  `"\x1b]52;c;aG9sYSBtdW5kbw==\a"`.

## Note / gotcha
Clipboard copy now does NOT depend on `xclip`/`xsel`/`wl-copy`. If a terminal
disables OSC 52 writes, copy still falls back to the OS helper when present. This
is why the first cut "didn't work" on WezTerm + COSMIC: no helper installed and
copy was OS-helper-only.

Related: [[tui-mouse-scroll-fix-2026-03-09]], [[tui_chat_info_sidebar_plan]].
