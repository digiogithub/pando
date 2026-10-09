---
id: PANDO-T-0010
type: task
title: "Shared watch exclusion matcher: defaults + Xcode, .gitignore, config watchExclude; apply in code-index, LSP and bootstrap watchers"
status: in_review
priority: high
parent: PANDO-US-0125
author: mcp
labels: [watcher, config, macos]
created: 2026-10-09T12:25:37Z
updated: 2026-10-09T12:29:17Z
started: 2026-10-09T12:29:17Z
---

## Description
New package (e.g. internal/fswatch) merging existing lists (lsp/watcher/watcher.go:760, app/lsp_bootstrap.go:19, app/remembrances_watch.go:191) plus Xcode/iOS defaults (DerivedData, Pods, Carthage, xcuserdata, .build, *.xcarchive, *.dSYM). Reuse internal/search/ignore.go for .gitignore. New config `watchExclude []string`. Applied before fsnotify Add, including dirs added on Create events.
## Acceptance Criteria
Unit tests for matcher; config field documented; watchers use it.
