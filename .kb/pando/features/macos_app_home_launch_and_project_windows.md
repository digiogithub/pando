---
created_at: 2026-09-29T22:26:15.905904564Z
updated_at: 2026-09-29T22:26:15.905904564Z
tags:
    - feature
    - fix
    - macos
    - desktop
    - projects
    - webui
    - pando
---
# macOS app icon opens Pando in home + Projects open their own desktop window

## Date
2026-09-30

## Problem
1. Clicking the Pando.app icon (Finder/Dock/Launchpad) did nothing. The bundle's
   CFBundleExecutable shell wrapper ran `pando-bin desktop` with LaunchServices'
   defaults: cwd `/` (read-only on macOS, config + `.pando/data` IPC bootstrap fail)
   and launchd's minimal PATH (`/usr/bin:/bin:/usr/sbin:/sbin`).
2. Projects view in the desktop app only toggled the background ACP child; user
   wanted a click on a project to open a separate Pando desktop instance for it.

## Changes
- `internal/desktop/workdir.go` (new):
  - `LaunchedFromApp()` — env `PANDO_LAUNCHED_FROM_APP=1` or (darwin && PPID==1).
  - `DefaultWorkingDir()` — cwd unless GUI launch or cwd == `/`, then `$HOME`
    (home = general project-less workspace; `config.IsHomeDirectory` already skips
    code indexing etc.).
  - `ImportLoginShellPath()` — darwin + GUI launch only: `$SHELL -l -i -c` printf with
    marker, 5s timeout, prepends login PATH (merge, dedup).
  - `SpawnInstance(dir)` — `os.Executable() desktop --cwd dir`, own process group
    (`procgroup.Ensure`), drops `PANDO_LAUNCHED_FROM_APP` from child env.
- `cmd/desktop.go` — no `--cwd`: `desktop.DefaultWorkingDir()` + `os.Chdir`; always
  `desktop.ImportLoginShellPath()`.
- `scripts/build-macos-app` — bundle wrapper now `cd "$HOME"`, exports
  `PANDO_LAUNCHED_FROM_APP=1`, `exec pando-bin desktop --cwd "$HOME"`. Old bundles are
  still covered by the Go fallback (cwd `/` / PPID 1).
- `internal/api/handlers_projects_desktop.go` (new) + route
  `POST /api/v1/projects/{id}/open-desktop`: desktop StartupMode only (409 otherwise),
  404 unknown project / missing folder, `current` when this window already uses the
  path, `already_open` when instanceregistry has a live ModeDesktop entry for it,
  else spawns → `opened`. `spawnDesktopInstance` / `liveDesktopForPath` are vars for tests.
- WebUI: `projectStore.openProjectDesktop(id)` (toasts per status);
  `ProjectsView` — when `isDesktop`, row click opens the project window, extra
  ExternalLink "Open in new window" button; Play/Stop keeps toggling the ACP child.
  Non-desktop (browser) behaviour unchanged. Adding a folder = existing "Add Project".

## Verification
- `go test ./internal/desktop/ ./internal/api/` pass (new: workdir_test.go,
  handlers_projects_desktop_test.go).
- `GOOS=darwin|windows go vet ./internal/desktop/`, `bash -n scripts/build-macos-app`,
  web-ui `tsc --noEmit` + eslint clean on touched files.
- NOT verified on a real Mac: needs rebuild of .app/.pkg (`xc release-osx`) and a
  Finder launch test.

Related: [[pando/fixes/macos_desktop_signing_fix.md]], [[pando/fixes/macos_pkg_bundle_relocation.md]]
