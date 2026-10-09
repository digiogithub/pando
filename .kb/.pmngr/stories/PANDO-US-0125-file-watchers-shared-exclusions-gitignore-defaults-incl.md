---
id: PANDO-US-0125
type: story
title: "File watchers: shared exclusions (.gitignore, defaults incl. Xcode, watchExclude), primary-only code watcher, shared LSP watcher, FD hygiene"
status: backlog
priority: high
parent: PANDO-EP-0021
author: mcp
labels: [watcher, macos, bug, lsp, config]
created: 2026-10-09T12:25:14Z
updated: 2026-10-09T12:25:14Z
---

## Description
Reduce FDs held by fsnotify (kqueue on macOS: one FD per file + dir).

## Acceptance Criteria
- One exclusion matcher used by code-index watcher, LSP workspace watcher and LSP bootstrap watcher; applied before `Add`.
- Defaults keep previous lists and add Xcode/iOS: DerivedData, Pods, Carthage, xcuserdata, .build, *.xcarchive, etc.
- `.gitignore` honored; config `watchExclude` (list of names/globs) honored.
- Code-index watcher only on IPC primary.
- Single fsnotify watcher for all LSP clients (fan-out).
- restartLSPClient closes old client; NewWatcher error handled; Create-event stat error does not kill the loop.
- Warning logged when a watcher registers > 10,000 paths.
