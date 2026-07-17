---
created_at: 2026-07-17T07:25:14.033131762Z
updated_at: 2026-07-17T07:32:04.579455445Z
tags:
    - fix
    - tui
    - webui
    - filetree
    - symlink
    - tools
    - git
---
# Fix: file tree auto-refresh (TUI + WebUI), git-free operation, and symlink traversal in Pando tools

Date: 2026-07-17

## Problems reported

1. **Stale file tree**: files created/removed by the agent were not reflected in the Editor
   mode file tree, neither in the TUI nor in the Web UI. The tree only loaded once (TUI `Init`,
   web `useEffect` on mount) and refreshed only on explicit user action (TUI `r` key, web
   `onRefresh` after create/rename/delete). Web `TreeNode` cached children with a
   `childrenLoaded` guard, so an expanded directory never re-read its contents.
2. **Symlinks invisible to Pando tools**: `ls`, `grep` and `glob` could not see the content of a
   path that is a Unix symlink, while plain bash could. Root cause: `filepath.Walk`
   (`internal/llm/tools/ls.go`) and `filepath.WalkDir` (`internal/search/walker.go`) use `Lstat`
   and never follow symlinks — not even when the walk root itself is a link (the walk then
   yields only the link entry, no children). `os.ReadDir` entries also report `IsDir() == false`
   for a symlink pointing at a directory, so `internal/api/handlers_files.go` and the TUI
   filetree loader listed linked directories as plain files.
   `view` was never affected: it uses `os.Stat`, which resolves links.
3. **Git dependency in the TUI tree** (raised by the user as "if the path is not a git repo that
   would error"). Measured: a *non-repo directory* already worked (git exits 128, handled), but a
   machine with **no `git` binary on PATH** made `readDirectory` return
   `git check-ignore: exec: "git": executable file not found in $PATH`, so the whole tree failed
   to load — nothing was listed at all. The locale-dependent `strings.Contains(out, "not a git
   repository")` check was also useless under a non-English git (observed:
   `fatal: no es un repositorio git`); only the exit-code check was doing the work.

## Changes

### Symlink traversal
- **New `internal/fileutil/walk.go`**:
  - `WalkFollowSymlinks(root, fn fs.WalkDirFunc)` — `filepath.WalkDir` semantics but follows
    symlinks that resolve to directories, including a symlinked root. Reported paths stay
    *logical* (the link name is kept, not rewritten to the target). Cycles are prevented with a
    visited set of `filepath.EvalSymlinks` real paths plus a `maxSymlinkWalkDepth = 64` cap.
    Broken links and links to files are reported as entries, never descended into.
    `fs.SkipDir` / `fs.SkipAll` are honored.
  - `IsDirEntry(dir, entry)` — `entry.IsDir()` resolving symlinks via `os.Stat`.
- `internal/llm/tools/ls.go`: `filepath.Walk` → `fileutil.WalkFollowSymlinks` (callback signature
  moved from `os.FileInfo` to `fs.DirEntry`).
- `internal/search/walker.go` (`SearchFiles`, backing **grep** and **glob**): `filepath.WalkDir`
  → `fileutil.WalkFollowSymlinks`.
- `internal/api/handlers_files.go` (`handleListFiles`): `isDir` now via `fileutil.IsDirEntry`.
- `internal/tui/components/filetree/loader.go`: `readDirectory` uses `fileutil.IsDirEntry`;
  `walkFiles` (fuzzy-search listing) uses `fileutil.WalkFollowSymlinks`.

### Git-free operation (TUI loader)
- `ignoredPaths` no longer returns an error: signature is now
  `ignoredPaths(projectPath, candidates) map[string]bool`. Any git failure (exit 1 = nothing
  ignored, exit 128 = not a repository, missing binary, anything else) yields an empty map, so
  the directory is still listed, just without applying `.gitignore`. Partial stdout is honored.
  Locale-dependent string matching dropped; stderr goes to `io.Discard`.
- `loadGitStatuses` likewise returns `map[string]GitFileStatus` with no error: status is
  decoration, its absence only means no status colors. `LoadGitStatus` no longer sets
  `GitStatusUpdateMsg.Err`.

### File tree auto-refresh — TUI
- `internal/tui/components/filetree/loader.go`:
  - `LoadFileTreeExpanded(projectPath, opts, expanded, statuses)` + `readDirectoryExpanded` —
    reload that re-reads the children of every expanded directory, so a refresh does not collapse
    the tree. `cloneExpanded` helper.
- `internal/tui/components/filetree/filetree.go`:
  - `autoRefreshInterval = 3s`, `autoRefreshTickMsg{seq}`, `scheduleAutoRefresh()`. The `seq`
    guard means only the newest tick chain is honored — a duplicated tick cannot fork into two
    self-perpetuating chains.
  - `Init` starts the chain; the tick reloads only when `rendered && !filterMode && !creatingFile`.
    `rendered` is set in `View()` and cleared on each reload, so a hidden tree (ChatOnly layout,
    another page) never hits the filesystem or git. The chain is alive because `ChatPageModel.Init`
    calls `p.sidebar.Init()`, which reaches the filetree through its container.
  - `reload()` now preserves expansion (`expandedPaths()`); the `r` key and `newFileCreatedMsg`
    use it too.
  - `FileTreeRefreshMsg` preserves the cursor by path (`currentNodePath` + `restoreCursor`).

### File tree auto-refresh — Web UI
- `web-ui/src/components/editor/CodeEditorView.tsx`: `TREE_REFRESH_INTERVAL_MS = 3000`; polls
  `fetchFiles` while the explorer is open and `document.hidden` is false; also refreshes on
  `visibilitychange` / `focus`. `fetchFiles` bumps a `treeVersion` counter passed down.
- `web-ui/src/components/editor/FileExplorer.tsx`: `treeVersion` prop threaded to `TreeNode`; the
  children-loading effect depends on it (the `childrenLoaded` early-return guard was removed, it
  is now only used to render "Empty folder") and is cancellation-safe.
  The web tree never depended on git (`handleListFiles` is pure `os.ReadDir`).

## Verification
- `go build ./...`, `go vet` on touched packages — clean.
- `go test ./internal/fileutil ./internal/search ./internal/llm/tools ./internal/api ./internal/tui/components/filetree ./internal/llm/agent` — pass.
- `npx tsc --noEmit` in `web-ui` — clean.
- New tests:
  - `internal/fileutil/walk_test.go` — linked dirs descended, symlinked root walked, cycle
    (proj → linkdir → real → loopback → proj) stops, broken link reported, `IsDirEntry`.
  - `internal/search/walker_symlink_test.go` — grep/glob find a match only reachable through a
    link, reported under the logical `linkdir` path.
  - `internal/llm/tools/ls_symlink_test.go` — ls descends into a linked dir and lists a linked root.
  - `internal/tui/components/filetree/loader_test.go` — expansion preserved across reload, new
    file appears on refresh, symlinked dir listed as a directory and traversable, and
    `TestLoadFileTreeWithoutGit` (empty `PATH`, so no git binary) proves the tree still lists
    everything and returns no error.
  - `internal/tui/components/filetree/filetree_refresh_test.go` — tick always reschedules, only
    reloads when `rendered`, clears `rendered`, drops stale `seq` (no forked chains), and skips
    the reload while the filter input is active.

## Notes / trade-offs
- TUI refresh runs `git status --porcelain` + `git check-ignore` every 3s while the tree is
  visible; the `rendered` gate keeps that cost off hidden trees. Both are now best-effort.
- `fileutil.GlobWithDoublestar` (glob fallback when native search fails) still does not follow
  symlinks.
- `gitTrackedAndUntrackedFiles` (fuzzy-search listing fast path) does not list files inside
  symlinked directories, because `git ls-files` does not traverse links; the `walkFiles` fallback
  (used when git is unavailable) does.

Related: [[feature_filetree_hidden_files_toggle]], [[pando/plans/webui_settings_complete_plan.md]]
