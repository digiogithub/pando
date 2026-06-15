---
created_at: 2026-06-15T22:20:59.658065339Z
updated_at: 2026-06-15T22:20:59.658065339Z
tags:
    - pando
    - feature
    - tui
    - sidebar
    - chat
    - config
---
# Pando TUI — Chat Info Sidebar (FEATURE, implemented)

Right-hand information column in the TUI, inspired by opencode (KB
`opencode/research/tui_sidebar_info_column.md`). Shows **Session title · LSPs ·
Plan (TodoWrite) · Modified Files**. Visible **only in the Chat workspace tab
(`ChatOnly`)** and **only when terminal width >= threshold**; auto-hides on
resize below the threshold and on switching tabs/pages.

Implemented across 4 phases (1-4 = MVP+config, 6 = UX/settings). Phases 2-5
plan in KB `pando/plans/tui_chat_info_sidebar_plan.md`.

## Behavior
- Auto-shows when `config.ChatSidebarEnabled()` (mode != "off") AND `layoutMode == ChatOnly` AND `width >= config.ChatSidebarMinWidth()` (default 120).
- Reserves a capped ~42-col panel (`chatInfoSidebarWidth`); chat keeps the rest. Ratio recomputed on every resize while in ChatOnly.
- Switching to Editor / Editor+Chat tabs (or leaving the Chat page) hides it automatically.

## Config (internal/config/config.go)
- `TUIConfig.ChatSidebar string` (json `chatSidebar`): `"auto"` (default) | `"off"`.
- `TUIConfig.ChatSidebarMinWidth int` (json `chatSidebarMinWidth`): default 120 (`defaultChatSidebarMinWidth`).
- Helpers: `config.ChatSidebarEnabled()`, `config.ChatSidebarMinWidth()`.
- Updaters: `config.UpdateChatSidebar(mode)`, `config.UpdateChatSidebarMinWidth(width)` (mirror `UpdateShowHiddenFiles`: in-memory + `updateCfgFile` + rollback).

## Component (internal/tui/components/chat/sidebar.go)
- `ChatInfoSidebar` interface + `NewChatInfoSidebar(session, history)`; `SetSession`, `BindingKeys`.
- Data: `history.Service` (Modified Files via initial-vs-latest `diff.GenerateDiff`), `tools.GetSessionTodos` + `TodosUpdatedMsg` (Plan), session pubsub.
- Subscription fix: subscribes to history **once** (stored `filesCh`), re-reads via `waitForFileEvent` (the inherited code re-`Subscribe`d per event with a background ctx → subscriber leak; also fixed loop death when event session != current).

## Chat page (internal/tui/page/chat.go)
- Fields `infoSidebar` / `infoSidebarContainer` (built in `NewChatPage`, left border + padding).
- `chatSidebarVisible()` (config-driven), `chatSidebarRatio()` (caps at `chatInfoSidebarWidth`).
- `rebuildLayout()` default/ChatOnly branch adds the right panel when visible.
- `Update`(WindowSizeMsg) + `SetSize` rebuild while ChatOnly (threshold crossing + ratio drift).
- `routeInfoSidebar(msg)` forwards SessionSelectedMsg / TodosUpdatedMsg / GoalUpdatedMsg / pubsub session+history; `Init` starts `infoSidebar.Init()`.
- `ChatSidebarConfigChangedMsg` handler rebuilds live.
- Keybind **Ctrl+Shift+B** (`ToggleInfoSidebar`) flips config auto↔off, persists, rebuilds (Ctrl+B is the LEFT file-tree sidebar — intentionally different).

## Settings UI (internal/tui/page/settings.go), General section
- "Chat Info Sidebar" `FieldSelect` (auto/off) → `tui.chatSidebar`.
- "Chat Sidebar Min Width" `FieldText` (int) → `tui.chatSidebarMinWidth`.
- `persistSetting` calls the config updaters; on save emits `chat.ChatSidebarConfigChangedMsg` (live-apply), broadcast to all pages in `tui.go` (same pattern as `filetree.SetShowHiddenMsg`).
- Helper `chatSidebarValue(mode)` normalizes display.

## Live-apply path
settings save → `chat.ChatSidebarConfigChangedMsg` → tui.go broadcasts to all pages → chat page rebuilds layout (no restart).

## Tests
- `internal/tui/page/chat_sidebar_test.go`: visibility matrix, ratio reservation, rebuild toggles right panel, `chatSidebarValue`, config-changed rebuild.
- `internal/config/config_test.go`: `ChatSidebarEnabled` / `ChatSidebarMinWidth` resolution.
- `go build ./...`, `go vet`, `gofmt`, and `go test ./internal/tui/... ./internal/config/ ./internal/llm/agent ./internal/api` all green.

## Not done / future
- Hide for sub-sessions (opencode parity) — pando has no obvious parent/sub-session notion exposed here.
- Optional Web-UI parity.
