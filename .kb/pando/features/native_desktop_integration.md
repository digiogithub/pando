---
tags: [feature, desktop, mygo, native-ui, integration]
---
# Native desktop: integration pass (MyGo)

Date: 2026-10-27. Workspace `/www/MCP/Pando/pando-native-desktop` (uncommitted). Plan: [[native_desktop_mygo]]. Builds on the per-area summaries `native_desktop_*`.

## What changed
1. **Router double build** ("a ListState shown by two Lists" going /chat -> /chat/s/{id}, /snapshots/x -> /snapshots/y): MyGo's TransitionAuto slides deeper routes and builds the leaving page too, so the one stateful area view was built twice per frame. Fix: `app/context.go newRouter()` uses `ui.TransitionNone` (as the Web UI; a fade was also tried but left pages transparent while the window got no frames, seen on the Mac mini with a locked screen), plus a guard `Root.leavingSameArea` in `app/layout.go` (leaving page of the same area renders an empty column). Regression tests: `app/router_test.go` (forces TransitionAuto), `agentvcs TestAgentVCSDeepLinksInPlace`.
2. **Chat <-> shell** (`app/chat/shellwire.go`): route sync (session click pushes `/chat/s/{id}`, new chat `/chat`, a new chat's session replaces the route, Back/deep links open the session; simple chat uses `/chat/simple/s/{id}`; `ActiveSessionID` understands both), `Shell().NextModel` used for the first prompt of a new chat (child workspaces keep a local pending model), `Shell().ModelChanged` chained (session model / default), persona selector in header slot `persona` order 10 (composer chip + dialog only in project workspaces: `Context.IsChild`), Shift+Tab toggles auto-approve (composer key + chat shortcut), composer consumes `TakeChatDraft`. Test `TestChatShellWiring`.
3. **Extension slots**: settings lists `extensions.Panels(a, SlotSettings)` each frame and draws `extensions.Slot(..., 640)`; chat info sidebar draws `SlotChatSide`; new `app.RegisterStatusItem` status strip under the main tab, the extensions package registers the `status-bar` slot (strip hidden when nothing to show).
4. **Theme at start**: `followPrefs` falls back to the backend `theme` field when the UI preferences have no theme (`app.ParseThemeID`, legacy claude/clay/starbucks mapped, TUI-only ids ignored); Appearance no longer overrides the live theme when opened, saving it also persists the prefs (`SetTheme`), accent clicks persist `themeAccent` at once. `client.DesktopSettings.Theme` added. Test `TestStartThemeFallsBackToBackendTheme`.
5. **i18n**: tray labels via `a.T` (`shell.restoreWindow`, `nav.settings`, `nav.projects`) and relabelled on language change (`Controller.SetTrayLabels`); quick menu `Command.TitleKey` (header.toggleTheme, header.toggleSidebar, header.simpleChat, shell.documentation); missing keys fixed (`common.cancel` -> `projects.view.common.cancel`, `settings.categories.extensions` and `projects.toasts.error` don't exist in the web catalogs -> literals). No catalog keys added (the sync test requires byte-equal copies of web-ui locales).
6. **Web bug**: `CreateTaskDialog` posted `{name, description, model}`; the API needs `prompt`. New store action `createTask` + `buildTaskPrompt` (name + blank line + details) posting `{prompt, background, model?}`; test `orchestratorStore.test.ts`.
7. **Consistency**: `kit.CopyText`, `app.ShortenPath`, `cmp.Or`/`slices.Contains` replace duplicated helpers (chat/setup/projects/instances/settings); chat uses `kit.IconSuccess`; kit disabled buttons are a readable muted neutral (dimmed accent made "Save" unreadable); Appearance theme cards stack preview + name (names overflowed). Wails mentions removed from README/comments (P5 coordination). OS notifications go through MyGo (`mygo.NewNotification`, freedesktop on Linux, native on macOS app bundles), independent of the removed beeep.
8. **Dev aid**: `PANDO_NATIVE_CAPTURE_PATHS` (+`_STEP`) screenshots a list of routes in one launch (`capture.go`).

## Verification
- `gofmt -l internal/nativeui` clean; `go vet ./internal/nativeui/...` (linux, `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64`, `GOOS=windows`) OK; `go test -race ./internal/nativeui/...` OK; `go build ./...` OK.
- web-ui: `npx tsc -p tsconfig.app.json --noEmit` OK; `npx vitest run` 28 files / 133 tests OK.
- Real app Linux (Wayland, isolated HOME, port 18877, `PANDO_DESKTOP_MULTI=1`): every area captured, scratchpad `screens/integration-*.png`. Fixed from them: Appearance cards overflow, unreadable disabled Save.
- Mac mini (temporary worktree from origin/main + workspace tarball, removed afterwards): `go vet`/`go test ./internal/nativeui/...` OK, build with `CGO_LDFLAGS=-framework UniformTypeIdentifiers`, locked screen, captures `screens/integration-macos-*.png` (blank pages with the fade -> fixed by TransitionNone).

## Gaps
- Quick menu, banners, model switcher, agentvcs, evaluator texts stay English like the web (no catalog keys exist).
- `themeAccent` "theme default" cannot be cleared through the prefs patch (omitempty).
- A first Linux run showed the chat model dialog open, not reproducible (likely a stray pointer click on the shared desktop; headless test asserts it stays closed).
- Persona header chip follows the last built chat of the window; when the app starts outside the chat it appears once the chat was shown.
