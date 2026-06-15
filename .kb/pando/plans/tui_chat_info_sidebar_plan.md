---
created_at: 2026-06-15T22:01:35.446819058Z
updated_at: 2026-06-15T22:01:35.446819058Z
tags:
    - pando
    - plan
    - tui
    - sidebar
    - chat
    - architecture
---
# Pando TUI — Chat Info Sidebar (Right Column) — Implementation Plan

Goal: add a right-hand **information column** to pando's TUI, inspired by
opencode (see KB `opencode/research/tui_sidebar_info_column.md`). Requirements
from the user:

- Show only when the terminal is **wide enough** (width >= threshold).
- Show only in the **Chat view** (the "Chat" workspace tab = `ChatOnly` layout mode) of TUI mode.
- **Hide automatically** when the window is resized below the width threshold.
- **Hide automatically** when switching views (other workspace tabs, or leaving the Chat page).

## Key discovery — most of the work already exists

`internal/tui/components/chat/sidebar.go` already contains a complete, **orphaned**
`sidebarCmp` (inherited from the crush fork, `NewSidebarCmp` is referenced
nowhere). It already renders the exact content we want:

- `header(width)` + `sessionSection()` (session title)
- `lspsConfigured(width)` (LSP status)
- `todosSection()` → **"Plan"** block from `tools.GetSessionTodos` / `TodosUpdatedMsg` (icons: ✓ completed, → in_progress, ○ pending)
- `modifiedFiles()` → **"Modified Files:"** with per-file `+additions`/`-removals`, fed by the `history.Service` (initial vs latest version diff via `diff.GenerateDiff`)

It already handles the messages it needs in `Update`: `SessionSelectedMsg`,
`pubsub.Event[session.Session]`, `pubsub.Event[history.File]`,
`TodosUpdatedMsg`, `GoalUpdatedMsg`. It implements `tea.Model` + `Sizeable`
(`SetSize`/`GetSize`) but NOT `Bindings` (no `BindingKeys`).

Data plumbing already flows through the app:
- `TodosUpdatedMsg` is dispatched in `internal/tui/tui.go:642`.
- session / history pubsub events flow through the TUI.
- `app.App.History` (`internal/app/app.go:66`) is the `history.Service`.

So this is primarily an **integration + responsive-gating** task, not new rendering.

## Relevant architecture (verified)

- Chat page: `internal/tui/page/chat.go` (`ChatPageModel`).
  - `ChatLayoutMode`: `ChatOnly`, `SidebarChat`, `SidebarEditor`, `EditorChatSplit`, `EditorChatTab`.
  - Workspace tabs (`maintabs.go`): **Chat→ChatOnly**, Editor→SidebarEditor, Editor+Chat→EditorChatSplit.
  - `rebuildLayout()` builds `p.layout` per mode; `ChatOnly` default = single left panel `p.chatContainer`.
  - `Update`(`tea.WindowSizeMsg`) and `SetSize(w,h)` call `p.layout.SetSize(width, layoutHeight())`.
  - `applyLayoutMode(mode)` → sets mode, `normalizeFocus()`, `rebuildLayout()`, then `layout.SetSize`.
  - NOTE: `keyMap.ToggleSidebar` (**Ctrl+B**) is already taken — it toggles ChatOnly↔SidebarChat (the LEFT file-tree sidebar). Do NOT reuse it for the info column.
- Layout primitives: `internal/tui/layout/split.go` `SplitPaneLayout` supports left+right (`WithLeftPanel`/`WithRightPanel`/`WithRatio`) and bottom. `internal/tui/layout/container.go` `Container` = `tea.Model` + `Sizeable` + `Bindings`; wrap any `tea.Model` with `layout.NewContainer(...)`.
- Config: `internal/config/config.go` `TUIConfig` (has `Theme`, `ShowHiddenFiles`); update helper pattern `UpdateShowHiddenFiles` at line 2643 (mutates in-memory + persists JSON, with rollback).
- Top-level page switching is `a.currentPage` in `tui.go`; when not on `ChatPage` the chat view isn't rendered, so leaving the page hides the sidebar for free.

## Threshold

opencode uses `width > 120`. Adopt a configurable minimum, default **120**
columns. Below it, the chat is too cramped (chat content + 40-ish col sidebar),
so the sidebar auto-hides and the chat reclaims full width.

---

## Phase 1 — Config: threshold + opt-out

- Extend `TUIConfig` (`internal/config/config.go`):
  - `ChatSidebar string` json `chatSidebar` — `"auto"` (default) | `"off"`. (Keep it a tri-state-ready string rather than bool so a future `"always"` is easy.)
  - `ChatSidebarMinWidth int` json `chatSidebarMinWidth,omitempty` — default 120 when 0.
- Add `UpdateChatSidebar(mode string) error` (+ optional `UpdateChatSidebarMinWidth`) mirroring `UpdateShowHiddenFiles` (in-memory mutate + persist + rollback on error).
- Add small accessors, e.g. `config.ChatSidebarEnabled()` and `config.ChatSidebarMinWidth()` resolving defaults.
- Tests: config round-trip + default resolution.

## Phase 2 — Make `sidebarCmp` embeddable

File: `internal/tui/components/chat/sidebar.go`.

- Add `BindingKeys() []key.Binding { return nil }` so it satisfies `Bindings` (lets it be used directly or cleanly wrapped). (Optional since `layout.NewContainer` already provides delegating Bindings, but explicit is safer.)
- Change `NewSidebarCmp` to return the concrete `*sidebarCmp` (or add `NewChatInfoSidebar(session, history) *sidebarCmp`) so the chat page can keep a typed pointer and re-assign on `Update`.
- Add a `SetSession(session.Session)` convenience and ensure `loadModifiedFiles` + `GetSessionTodos` run on session change (already wired via `SessionSelectedMsg`).
- Decouple the history subscription: currently `Init()` blocks on `<-filesCh`. When embedded in the chat page we instead rely on the chat page forwarding `pubsub.Event[history.File]` (Phase 5). Keep `Init` returning the modified-files preload but make the channel read defensive/optional so it works whether or not the page drives it.
- Tests: `View()` renders Plan + Modified Files given a fake history + todos.

## Phase 3 — Mount the info column in `ChatOnly` layout

File: `internal/tui/page/chat.go`.

- Add fields: `infoSidebar *chat.sidebarCmp` (typed) and `infoSidebarContainer layout.Container`.
- In `NewChatPage(app)`: construct `infoSidebar = chat.NewChatInfoSidebar(p.session, app.History)`; wrap `infoSidebarContainer = layout.NewContainer(infoSidebar, <left border + small padding>)`.
- Add helper:
  ```go
  func (p *ChatPageModel) chatSidebarVisible() bool {
      if config.ChatSidebar disabled { return false }
      return p.layoutMode == ChatOnly && p.width >= config.ChatSidebarMinWidth()
  }
  ```
- In `rebuildLayout()` `ChatOnly`/`default` branch:
  ```go
  if p.chatSidebarVisible() {
      p.layout = layout.NewSplitPane(
          layout.WithLeftPanel(p.chatContainer),
          layout.WithRightPanel(p.infoSidebarContainer),
          layout.WithRatio(0.72), // ~28% to sidebar; or compute fixed ~42 cols
      )
  } else {
      p.layout = layout.NewSplitPane(layout.WithLeftPanel(p.chatContainer))
  }
  ```
  Consider a fixed sidebar width (~40-44 cols like opencode's 42) instead of a pure ratio so the chat doesn't shrink oddly on ultra-wide terminals — if so, derive the ratio from `min(42, width*0.3)/width` at rebuild time.

## Phase 4 — Reactive show/hide on resize & view change

File: `internal/tui/page/chat.go`.

- View switching is already covered: `applyLayoutMode` calls `rebuildLayout()`, and only `ChatOnly` builds the sidebar — switching to Editor / Editor+Chat tabs (or any non-Chat mode) rebuilds without it. Leaving the Chat page entirely is handled at `tui.go` (page not rendered).
- Resize: in `Update`(`tea.WindowSizeMsg`) and in `SetSize`, detect a **threshold crossing** while in `ChatOnly`:
  ```go
  prev := p.chatSidebarVisible() // compute against old width before assigning
  p.width, p.height = msg.Width, msg.Height
  if p.layoutMode == ChatOnly && p.chatSidebarVisible() != prev {
      p.rebuildLayout()
  }
  cmds = append(cmds, p.layout.SetSize(msg.Width, p.layoutHeight()))
  ```
  (Same guard added to `SetSize`.) This adds the column when the window grows past the threshold and removes it when it shrinks below.

## Phase 5 — Message routing & live data sync

File: `internal/tui/page/chat.go` `Update`.

- Forward the sidebar's input messages so its content stays current (update it even while hidden, so it's ready when shown — it's cheap):
  - `chat.SessionSelectedMsg` (also already sets `p.session`),
  - `chat.TodosUpdatedMsg`,
  - `chat.GoalUpdatedMsg`,
  - `pubsub.Event[session.Session]`,
  - `pubsub.Event[history.File]`.
  Pattern:
  ```go
  m, cmd := p.infoSidebar.Update(msg)
  p.infoSidebar = m.(*chat.sidebarCmp)
  cmds = append(cmds, cmd)
  ```
  Place this routing near the existing message handling; keep the typed re-assignment so the container View reflects updates (note: `layout.NewContainer` holds the model by value/pointer — verify the container re-reads the pointer; if it stores a `tea.Model` copy, route through the container's `Update` instead, or rebuild the container reference).
- Ensure `infoSidebar.Init()`'s modified-files preload runs once (call from `ChatPageModel.Init`).
- On `SessionSelectedMsg`, the sidebar reloads todos + modified files (already in its `Update`).

## Phase 6 — Polish, optional toggle, settings UI, tests, docs

- Visual: left border between chat and sidebar via the container; width-aware truncation already present (`ansi.Truncate`). Match theme (`backgroundPanel`-equivalent) — check `styles.BaseStyle()`.
- Optional manual override keybind (NOT Ctrl+B — taken). e.g. add `ToggleChatInfo` on a free chord; flips config `chatSidebar` auto↔off and rebuilds. Document in keymap help.
- Settings UI: add a "Chat info sidebar" toggle + min-width field to the General settings page (`internal/tui/components/settings/...`), persisting via the Phase 1 helpers (mirror the ShowHiddenFiles wiring from the hidden-files feature).
- Hide entirely for sub-sessions if pando exposes a parent/sub-session notion (parity with opencode's `parentID` rule) — optional.
- Tests (`tests/` per project convention + `go test ./internal/tui/...`):
  - threshold crossing adds/removes the right panel in `ChatOnly`;
  - switching to Editor/Editor+Chat removes it;
  - `config off` never shows it;
  - sidebar content updates on `TodosUpdatedMsg` / history events.
- Docs/KB: update this plan's status and the feature memory when implemented.

---

## Sequencing & risk notes

- Phases 1→6 are mostly independent; **Phase 3 + 4** are the core. Phase 2 is a small enabling refactor. Phase 5 is required for live updates but the column will already render static content from session load without it.
- Lowest-risk MVP = Phases 2+3+4 (auto show/hide by width in Chat tab, content from session-load). Add Phase 5 for live refresh, Phase 1/6 for config + UX.
- Watch the `layout.NewContainer` value-vs-pointer semantics when re-assigning `infoSidebar` after `Update` (Phase 5) — easiest is to route updates through the container or keep the sidebar as a pointer the container dereferences.
- Sidebar width: prefer a capped fixed width (~42 cols) converted to a ratio at rebuild time so chat width stays sane on wide terminals.

## Reference
- opencode behavior analyzed in KB: `opencode/research/tui_sidebar_info_column.md` (breakpoint `width>120`, docked vs overlay, plugin slot sections Context/MCP/LSP/Todo/Modified Files, live SSE updates).
