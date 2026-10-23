---
created_at: 2026-10-23T13:11:14.725710159Z
updated_at: 2026-10-23T13:12:33.303455873Z
tags:
    - fix
    - agentvcs
    - webui
    - desktop
    - macos
---
# Fix: WebUI/desktop diff left pane empty + macOS context menu missing (2026-10-23)

## 1. Diff viewer: left (original) pane empty

### Symptoms
WebUI / desktop diff viewer (FileChangesBar -> DiffViewer) showed the final file on the right and an empty left pane, even locally. The local agent-vcs store (`.pando/data/agent-vcs/`) had empty `commits/`, `sessions/`, `trees/` and no blobs.

### Root causes
1. **agent-vcs Cleanup destroyed shared objects.** `deleteSession` deleted the trees of the pruned session's commits, but trees are content-addressed and shared (every session started on the same working tree has the same baseline tree). Pruning one session removed the baseline tree of surviving sessions; the blob GC (refs only from remaining trees) then deleted their blobs. `SessionDiff` failed with `load tree ...: no such file or directory`. Cleanup runs on every process shutdown (`app.go`, `Snapshots.AutoCleanupDays` / `MaxSnapshots`), and several processes share the dir.
2. **Fallback cannot rebuild a `write`.** `DiffViewer.loadDiffContent` fallback reverts the recorded edits on the current file; a `write` replaces the whole file, so `revertEdits` yields `''`.
3. **Late baseline.** A session without a baseline (resumed after restart, or created before snapshots were enabled) got its baseline from the first per-turn `Record`, i.e. after the turn's edits, hiding them.

### Changes
- `internal/agentvcs/service.go`: `deleteSession` only deletes commits + session log. New `collectGarbage()` collects trees unreachable from surviving sessions' commits, then blobs unreferenced by surviving trees; objects younger than `gcGracePeriod` (10 min) are kept (another process may have saved them before writing its commit/log); aborts if a session log is unreadable. New `mu sync.Mutex` serialises `NewChange`/`Record` (`newChangeLocked`).
- `internal/agentvcs/storage.go`: `SaveTree`/`StoreBlob` refresh modtime (`touch`) when reusing an existing object; `ListAllBlobHashes` returns mtimes; new `ListAllTrees`.
- `internal/session/session.go`: `EnsureSnapshotBaseline(ctx, sessionID)` — synchronous "start" snapshot (idempotent `NewChange`).
- `internal/llm/agent/agent.go`: coder agent calls `session.EnsureSnapshotBaseline` before `processGeneration`.
- New `GET /api/v1/sessions/{id}/file-original?path=` (`internal/api/handlers_file_history.go`, route in `routes.go`): returns the `initial` version from the per-session file history (`history.Service`, recorded by edit/write/patch tools) — independent of agent-vcs. Path matching: exact, or relative suffix.
- `web-ui/src/components/chat/DiffViewer.tsx`: fallback order now agent-vcs -> file on disk + file-history original -> revert edits -> snippets.

### Verification
- New tests `TestCleanupKeepsObjectsSharedWithSurvivingSessions`, `TestCleanupKeepsRecentUnreferencedObjects` (agentvcs), `TestHandleSessionFileOriginal` (api). Bug reproduced first with a temporary test (SessionDiff error after Cleanup).
- `go test ./internal/llm/agent ./internal/api ./internal/agentvcs ./internal/session` ok on Linux and on the Mac mini; `npx tsc -b` + eslint clean.

## 2. macOS desktop: no context menu on prompt/inputs

### Root cause
`desktop/main.go` had `EnableDefaultContextMenu: false` (from the initial scaffold). Wails on macOS then adds a document-end WKUserScript `window.wails.flags.disableDefaultContextMenu = true` (forMainFrameOnly false). Pando pages get the Wails runtime replayed by `App.OnDomReady` (ExecJS, `internal/desktop/app.go`); when the user script lands after that replay the flag is set and the runtime `contextmenu` listener calls `preventDefault()` on every event, inputs included. When it lands first it throws ("Script error.", `window.wails` undefined) — a race, which is why one synthetic probe could not tell old and new builds apart. No native (WailsWebView / our .m) override blocks the menu. Cmd+C/V worked via the Edit menu ([[pando/fixes/macos_desktop_edit_menu_and_tray.md]]).

### Change
`EnableDefaultContextMenu: true`: no flag script; the runtime applies its "auto" policy (`contextmenu.js`): native menu on input/textarea/contentEditable/selected text, hidden elsewhere; per-element override via CSS `--default-contextmenu: show|hide`.

### Verification (Mac mini, user right-clicking, one window at a time)
- Local build (patch over origin/main, `wails build -platform darwin/arm64`): flag false, trusted `contextmenu` on the prompt textarea not prevented, menu shown.
- v1.4.3 `pando-desktop` binary (copied into a temp bundle): flag true, trusted `contextmenu` on the textarea `defaultPrevented`, no menu.
- Probe method: python proxy in front of `pando` (localhost:8765) injecting a script that POSTs `mousedown`/`contextmenu` events (isTrusted, defaultPrevented) back; screencapture/System Events are blocked by TCC over ssh. Run each build alone: several windows at once made the user report ambiguous. Mac repo restored to origin/main afterwards.

## Commits (local jj, not pushed)
- `fix(agentvcs): keep shared trees on cleanup and show the original in diffs`
- `fix(desktop): show the native context menu on macOS inputs`

Related: [[pando/fixes/webui-native-dialogs-wails-macos.md]]
