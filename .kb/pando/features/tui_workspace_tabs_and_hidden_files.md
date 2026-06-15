---
created_at: 2026-06-15T21:30:18.643382058Z
updated_at: 2026-06-15T21:30:18.643382058Z
tags:
    - feature
    - tui
    - filetree
    - config
    - webui
    - implemented
---
# TUI Workspace Tabs + File-Tree Hidden-Files Toggle (2026-06-15)

Two related TUI features implemented together. Code uses English throughout per project conventions.

---

## Feature 1: Clickable Workspace Tab Header

Workspace tab header added to the chat page TUI, inspired by GustavoCaso/docker-dash (which uses bubblezone for clickable regions — the same pattern Pando already uses for terminal tabs). Three clickable tabs with icon+text that act as shortcuts to existing `ChatLayoutMode`s:

- **Chat** (`ChatIcon`) → `ChatOnly` (default selected)
- **Editor** (`EditorIcon`) → `SidebarEditor` (file tree left + open files right)
- **Editor+Chat** (`SplitViewIcon`) → `EditorChatSplit`

### Files
- `internal/tui/page/maintabs.go` (new) — `mainTabBar` component + `mainTabs` table; `mainTabIndexFor(mode)` (mode→active tab), `mainTabModeFor(idx)` (tab→mode). `mainTabBarHeight = 1`.
- `internal/tui/page/chat.go` — `tabHeader *mainTabBar` field; `layoutHeight()` helper reserves the header row; all 4 `layout.SetSize` call sites (SetSize/Init/WindowSizeMsg/applyLayoutMode) use it; `View()` joins `tabHeader.View(layoutMode)` above the layout via `lipgloss.JoinVertical`; public `SelectMainTab(idx)` + `MainTabCount()`.
- `internal/tui/zone/zone.go` — `MainTabPrefix`, `MainTabID(idx)`, `MarkMainTab(idx, content)`.
- `internal/tui/styles/icons.go` — added `EditorIcon = "󰈮"`, `SplitViewIcon = "󰜫"` (reused existing `ChatIcon`).
- `internal/tui/tui.go` — `handleMouse` left-click loop over `MainTabID(i)` calls `a.chatPage.SelectMainTab(i)`. `a.chatPage` is the same `*ChatPageModel` pointer stored in `a.pages[ChatPage]`, so the mutation is visible.

### Keybindings
`alt+1/2/3` jump directly to a tab (`ChatKeyMap.SelectTab`, handled in `handleKey` before the textarea sees the key; the trailing digit selects the tab index). Auto-appears in help via `KeyMapToSlice(keyMap)`.

### Notes
- Active-tab highlight collapses internal modes: `SidebarChat`→tab0, `EditorChatTab`→tab2.
- Tab styling: active = `Primary` bg + `BadgeText` fg bold; inactive = `BackgroundDarker` + `TextMuted`.
- The overlay (completion dialog) is placed on `layoutView` first, then the header is joined above it, so zone Scan re-computes mouse coordinates correctly (everything shifts down 1 row).
- Tests: `internal/tui/page/maintabs_test.go`.

---

## Feature 2: File-Tree Hidden-Files Toggle

Show/hide hidden files (dotfiles) and directories in the file tree. Default = hidden (historical behavior).

### Config
- `TUIConfig.ShowHiddenFiles bool` (json `showHiddenFiles`) in `internal/config/config.go`.
- New `config.UpdateShowHiddenFiles(enabled)` persists to the config file (mirrors `UpdateAutoCompact`).

### Filetree component (`internal/tui/components/filetree/filetree.go`)
- `Component` interface gains `ShowHidden() bool`, `SetShowHidden(show) tea.Cmd`, `ToggleHidden() tea.Cmd`.
- Private `reload()` re-runs `LoadFileTree` + `LoadGitStatus` (+ `LoadFilteredTree` when a fuzzy filter is active), honoring `showHidden`.
- The `showHidden` field + `WithShowHidden` option already existed; this exposes live toggling.
- New message `SetShowHiddenMsg{ShowHidden bool}` handled in `FileTree.Update` → `SetShowHidden`.

### Chat page (`internal/tui/page/chat.go`)
- `filetree.New` now passed `WithShowHidden(showHiddenFilesFromConfig())` (reads `config.Get().TUI.ShowHiddenFiles`).
- `ChatKeyMap.ToggleHiddenFiles` = `ctrl+shift+h`, handled in `handleKey` → `ToggleHiddenFiles()`: toggles the tree, persists via `UpdateShowHiddenFiles`, reports info ("Hidden files: shown/hidden").

### Settings page (`internal/tui/page/settings.go`)
- "Show Hidden Files" toggle field key `tui.showHiddenFiles` in `buildGeneralSection`.
- `persistSetting` case calls `config.UpdateShowHiddenFiles`.
- **Live reload:** `saveField` appends `util.CmdHandler(filetree.SetShowHiddenMsg{...})` when the key is `tui.showHiddenFiles`.

### App-level live reload (`internal/tui/tui.go`)
- New `case filetree.SetShowHiddenMsg` in app `Update` broadcasts the message to all pages (same pattern as `TodosUpdatedMsg`); the chat page's `routeMessage` default branch forwards it to the live tree. So changing the setting in the settings page reloads the tree instantly.

### Web-UI parity
- API `internal/api/handlers_settings.go`: `SettingsResponse.ShowHiddenFiles` + `SettingsUpdateRequest.ShowHiddenFiles` (json `show_hidden_files`), wired to `config.UpdateShowHiddenFiles`.
- API `internal/api/handlers_files.go` `handleListFiles`: hidden filtering now honors `cfg.TUI.ShowHiddenFiles` (was a hardcoded skip), with a per-request `?hidden=true/1` override.
- Frontend: `web-ui/src/types/index.ts` (`show_hidden_files`), `settingsStore.ts` DEFAULTS (`false`), `components/settings/GeneralSettings.tsx` Toggle, i18n keys `settings.general.showHiddenFiles(+Description)` in all 7 locales (en/es/de/fr/pt/ja/zh).

### Known follow-ups
- Settings-page change in the web-UI persists and the files API honors it on next request, but the web file explorer does not auto-refresh on save (would need a re-fetch after `PUT /settings`).
- `ctrl+shift+h` may collide with `ctrl+h` on terminals that don't distinguish shift; in practice the chat page intercepts it first. Consistent with the existing `ctrl+shift+n` (filetree new file).

### Verification
- `go build ./...` clean; tests green for `internal/tui/page`, `internal/config`, `internal/api`.
- web-ui `tsc --noEmit` clean; all 7 locale JSON files valid.
