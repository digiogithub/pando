---
created_at: 2026-07-17T07:40:57.571534762Z
updated_at: 2026-07-17T07:40:57.571534762Z
tags:
    - fix
    - tui
    - filetree
    - symlink
    - git
    - gitignore
---
# Fix: git check-ignore batch aborted by symlinked directories (TUI file tree)

Date: 2026-07-17

Follow-up to [[pando/fixes/filetree_autorefresh_and_symlink_traversal.md]]. Two regressions
introduced by that same change, found in `/www/Dreamplace/dreamplacemeets`.

## Symptom

In the TUI editor tree the `frontend` symlink was listed with its contents, while the `backend`
symlink (same shape, same repo layout) was missing entirely.

## Root cause (chained, both self-inflicted)

`.gitignore` in that project ignores **both**:

```
frontend
backend
```

1. `fileutil.IsDirEntry` (added in the previous fix) started reporting a symlink-to-directory as a
   directory. `ignoredPaths` appends a trailing-slash variant for directories (needed to match
   directory-only patterns such as `reports/`), so the batch fed to `git check-ignore --stdin`
   gained a `backend/` line. Git refuses that:
   `fatal: pathspec 'backend/' is beyond a symbolic link` (observed in Spanish:
   `fatal: el patrón de ruta 'backend/' está detrás de un enlace simbólico`), exit 128, and it
   **aborts the whole batch**.
2. The previous fix also made `ignoredPaths` honor partial stdout on error. So the entries printed
   before the fatal (`backend`, alphabetically first) were hidden, and everything after it
   (`frontend`) was never checked and stayed visible. The tree's contents depended on listing
   order.

Before the previous session both were hidden (exit 128 → empty ignore map). Seeing `frontend` was
the bug, not the correct behavior.

## Decision

Asked the user. Chosen: **strict .gitignore** — a gitignored symlink is hidden like any other
ignored entry. Rejected alternatives: always showing symlinked dirs, and a
`tui.showIgnoredFiles` setting.

## Changes (`internal/tui/components/filetree/loader.go`)

- `loadCandidate` gains `isSymlink`, set in `readDirectory` from
  `entry.Type()&fs.ModeSymlink != 0`.
- `ignoredPaths` sends the trailing-slash variant only for **real** directories
  (`candidate.isDir && !candidate.isSymlink`), so no fatal, so no aborted batch. The plain path is
  still sent, and it is what matches a bare `backend` pattern.
- `ignoredPaths` no longer honors partial output: **only a clean exit (0) is trustworthy**. Any
  error → empty map → the tree lists everything rather than applying a half-computed .gitignore.
  This reverts the partial-output tweak from the previous fix, which is what made the result
  order-dependent.

## Verification
- `go build ./...`; `go test ./internal/fileutil ./internal/search ./internal/llm/tools ./internal/api ./internal/tui/components/filetree ./internal/llm/agent` — pass.
- New `TestReadDirectoryAppliesGitignoreToSymlinksRegardlessOfOrder` in
  `internal/tui/components/filetree/loader_test.go`: real `git init` fixture with two symlinks
  (`a_ignored` sorting before `z_kept`, only the first gitignored) plus `kept.txt`. Asserts the
  ignored symlink is hidden while the later symlink and the regular file stay visible.
  **Confirmed it fails when the `!candidate.isSymlink` guard is removed**
  ("a gitignored symlink must be hidden like any other ignored entry") and passes with it.
- Probed the real directory: the tree now lists `docs`, `pencil`, `scripts`, `AGENTS.md`, … and
  hides both `frontend` and `backend`, deterministically.

## Lesson
`git check-ignore --stdin` is all-or-nothing: one fatal pathspec kills the rest of the batch. Never
trust its output on a non-zero exit, and never hand it a path that traverses a symlink.
