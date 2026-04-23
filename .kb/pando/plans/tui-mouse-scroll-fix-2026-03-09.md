# Plan: TUI Mouse Scroll & Chat Panel Fix

## Overview
3 issues to fix in Pando TUI:
1. Mouse wheel scrolls ALL components instead of only the one under the mouse
2. Mouse wheel doesn't work in AI chat viewport (only PageUp/PageDown work)
3. Can't type in chat input when chat is shown in the right panel

## FASE 1: Forward mouse events to viewport in messagesCmp
**File:** `internal/tui/components/chat/list.go`
**Problem:** `messagesCmp.Update()` only forwards `tea.KeyMsg` to the viewport (lines 104-110). Mouse events never reach the viewport even though `MouseWheelEnabled=true`.
**Fix:** Add `tea.MouseMsg` case in Update() that forwards to `m.viewport.Update(msg)`.

## FASE 2: Route mouse events only to hovered component  
**File:** `internal/tui/page/chat.go`
**Problem:** `routeMessage()` sends all non-KeyMsg messages to ALL components. Mouse wheel events reach every viewport simultaneously.
**Fix:** Add `tea.MouseMsg` case in `routeMessage()` that routes based on mouse X position and panel widths from `GetSize()`.

Panel routing by layout mode:
- ChatOnly: all → chatLayout
- SidebarChat: X < ftWidth → fileTree, else → chatLayout
- SidebarEditor: X < ftWidth → fileTree, else → editorWorkspace
- EditorChatSplit: X < ftWidth → fileTree, X < ftWidth+edWidth → editorWorkspace, else → chatLayout
- EditorChatTab: X < ftWidth → fileTree, else → chatTabWorkspace

## FASE 3: Fix chat input in right panel
**Files:** `internal/tui/page/chat.go`, `internal/tui/components/chat/editor.go`
**Problem:** When chat is in right panel (EditorChatSplit/EditorChatTab), textarea doesn't accept input.
**Root causes:**
1. textarea.Focus() not called when switching to focusChatRight
2. In EditorChatTab, advanceFocus() doesn't cycle to focusChatRight
3. tabBar.Update(msg) may consume key events before chat receives them

## Key Architecture
- `appModel.Update()` (tui.go:135): handles MouseMsg via handleMouse() for clicks only, then falls through to page.Update()
- `ChatPageModel.routeMessage()` (chat.go:496): routes KeyMsg by focus, sends all other msgs to ALL panels
- `splitPaneLayout.Update()` (split.go:54): forwards ALL messages to ALL panels
- `messagesCmp.Update()` (list.go:85): only forwards KeyMsg to viewport, ignores MouseMsg
- Chat editor textarea is created focused by default but focus may be lost during layout changes
- Panel focus cycle managed by advanceFocus() in chat.go

## Module: github.com/digiogithub/pando
## Go project, BubbleTea TUI framework