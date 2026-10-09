---
id: PANDO-T-0013
type: task
title: Single shared fsnotify workspace watcher fanned out to all LSP clients
status: in_review
priority: medium
parent: PANDO-US-0125
author: mcp
labels: [lsp, watcher]
created: 2026-10-09T12:25:37Z
updated: 2026-10-09T12:33:30Z
started: 2026-10-09T12:33:30Z
---

## Description
Today each LSP client creates its own recursive fsnotify watcher (app/lsp.go:396-411, lsp/watcher/watcher.go:316+). Replace with one app-level watcher whose events are dispatched to each client's WorkspaceWatcher (registration matching per client). Clients added/removed dynamically.
## Acceptance Criteria
One fsnotify instance regardless of LSP client count; events still reach each client; tests.
