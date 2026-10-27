---
tags: [feature, desktop, mygo, native-ui]
---
# Native desktop P3 workbench: editor, terminal, agentvcs

Date: 2026-10-27. Workspace `/www/MCP/Pando/pando-native-desktop` (jj, uncommitted). Plan: [[native_desktop_mygo]]. Foundation: [[native_desktop_p0_foundation]].

## What changed
- `client/files.go` (list/read/write/create/rename/delete, `ReadRaw`, `GitBranch`), `client/agentvcs.go` (sessions, log, commit detail, blob as raw text, revert, revert-files, snapshots CRUD), `client/terminal.go` (`ConnectPty`: gorilla websocket already in go.mod, token as `?token=`, local-CA TLS reused from the HTTP transport; binary frames = I/O, text = ready/exit/resize/close). Tests: `client/workbench_test.go`.
- `app/editor` (id `editor`, `/editor/{path...}`): explorer (MyGo `ui.Outline`, lazy dirs, 3 s poll like web, filter, context menu new file/folder/rename/delete, hidden-files toggle via `?hidden=`), tabs with dirty dot + close-without-saving confirm, MyGo `ui/editor` code editor (syntax, find bar, folding, multi-cursor, git-style gutter changes vs last saved text), Ctrl/Cmd+S + Save button, image viewer (`ui.Bitmap`), PDF placeholder, status bar (branch, path, language, UTF-8, Ln/Col), reload banner when a dirty file changes on disk (clean open file auto-reloads, so agent edits show up).
- `app/terminal` (id `terminal`): tabs of PTY shells, New / Clear (Ctrl+L) / close (terminates the shell; leaving the area detaches, shells keep running server side), exit and disconnect notices, theme from kit tokens, started at the real size (connect on first Resize). Engine: MyGo terminal plugin (libghostty-vt).
- `app/agentvcs` (id `agentvcs`, `/snapshots/{id...}`): sessions | commit log (timeline) | commit detail with changed files, multi-select + revert selected / revert file / Revert All (confirm), full-area diff viewer (`editor.DiffView`, split/unified, +/- stats, Esc), second tab Snapshots (list, create, revert, delete). Deep links: `/snapshots/<session>[/<commit>[/<file>]]`.
- `nativeui.go`: 3 blank imports. New icon SVGs in `kit/icons` (save, folder-tree, file-code, image, folder-plus/file-plus, eye/eye-off, undo-2, file-diff, git-compare); constants are local to each area to avoid clashes.

## Terminal trade-off (important)
libghostty-vt is **pure Go + purego, no cgo** (verified `CGO_ENABLED=0` vet for darwin/windows), but it is a native shared library that is NOT in the binary: MyGo downloads it once into `~/.cache/mygo/natives/<sha256>` (checked by SHA-256) when a terminal first opens, or uses `$MYGO_GHOSTTY_VT`. Packaged apps built by the `mygo` CLI embed it; Pando builds with plain `go build`, so P5 must either ship the library with the app (copy next to the binary / signed in the macOS bundle and set MYGO_GHOSTTY_VT) or accept first-run download. If loading fails the area shows the error with Retry and points to `pando app`. Offline first run = no terminal.

## Gaps
- LSP: Pando exposes no diagnostics/hover/completion over HTTP, so none shown (editor hooks `SetDiagnostics/OnHover/OnCompletion` are ready).
- Terminal: no exec fallback mode (web falls back to POST /terminal/exec when the websocket fails; here a banner + Retry), no tab persistence across app restarts (web reattaches via sessionStorage), no split panes.
- Editor: PDF preview not rendered; no unsaved-changes prompt when quitting the app; saves are whole-file PUT (like web).
- Navigating from `/snapshots/x` to another `/snapshots/y` while the area is shown panics MyGo ("a ListState shown by two Lists", the view is built twice in the route-transition frame). Navigation from another area works; a router-level fix (or one build per frame) is needed before deep-linking in place.
- Snapshots tab has no "apply" action (web store has it but no button either).

## Verification
- `go test -race ./internal/nativeui/client ./internal/nativeui/app/{editor,terminal,agentvcs}` OK (httptest/fake ws; terminal test runs the real libghostty-vt: output rendered, typed input forwarded). `go vet` of those + `CGO_ENABLED=0 GOOS=darwin|windows GOARCH=arm64 go vet` OK.
- Real server (`pando serve`, isolated HOME, seeded agent-vcs session via the real service) + temporary harness (removed) on XWayland: screenshots in scratchpad `screens/`: `editor-main.png` (tree, tab, highlighted Go, status bar), `agentvcs-commit.png`, `agentvcs-diff.png` (word-level split diff), `terminal.png` (real login shell, prompt, cursor).
- `go build ./...` was broken at the end by another agent's duplicate `CopilotLogin` in client/setup.go vs settings_custom.go (not mine).
