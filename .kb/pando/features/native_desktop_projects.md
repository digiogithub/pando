# Native desktop: projects area and project workspaces

Status: done in workspace pando-native-desktop (not committed). Plan: [[native_desktop_mygo]].

## What
- `internal/nativeui/client/projects.go`: Project/ProjectWebInstance DTOs, list/add/rename/delete/init/stop/deactivate, `OpenProjectWeb` (+`ParseProjectError`), `ListProjectWebInstances`, `SubscribeProjectEvents`, `BrowseDirectories`. `client/derive.go`: `Client.Derive(prefix)` makes a client for another API root through the same server (shares transport, takes the TOKEN FROM THE PARENT: the proxy answers the child's /token with a placeholder and swaps the real child token). `client.go`: `parent` field, token/refresh delegate to it.
- `app/context.go`: `Context.ForClient(client, cwd)` = child context (own Router, shared dispatcher/toasts/ctx/logger). Areas are instantiated against it unchanged because they only use `Context.Client`.
- `app/shellext.go` + `layout.go`: `ShellExt` (tabs, Active, Select, Close, Build, Dialogs), `RegisterShellExt`, tab bar (Main + extension tabs, shown only when a tab exists), `NewEmbeddedRoot(ctx, brand)` (no theme/toasts/tab bar/theme toggle, hides Projects in its sidebar, does not set the window title).
- `app/projects/`: `manager.go` (per-window manager registered as ShellExt; restores running instances from `/projects/web`, follows `/projects/events`, open/close/stop, init-needed flow, instance limit, errors), `tab.go` (running tab = embedded Root over derived client; starting/stopped/error screens; init wizard and close stop/keep dialogs, choice remembered per session), `view.go` (Projects table, add form with directory browser, rename, stop confirm when delegations, two-click delete, clear delegation target, open in new window via server), `dirbrowser.go`.
- Dev aids: `PANDO_NATIVE_OPEN_PROJECT=<name|path|id>` opens a workspace at start; `PANDO_NATIVE_CHILD_PATH=/logs` sets the route inside workspaces.

## Verification
- `go vet` + `go test -race ./internal/nativeui/app ./internal/nativeui/app/projects ./internal/nativeui/client` OK (derived client token test; headless list, init dialog, tab open, child area reading through derived client with the parent token).
- Real app (isolated HOME, Wayland): list, init dialog, and a real child `pando serve` shown through the proxy (embedded Logs of project proj-a). Screens in scratchpad `screens/projects-*.png`.

## Gaps
Keyboard shortcuts for tabs (Ctrl+Alt+1-9/W/arrows), tab reorder, busy/child title bridge (busy = delegations>0 only), "open in new window" delegates to server (separate window), child chat streaming untested until chat parity lands (the child uses the same chat area).
