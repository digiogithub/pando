---
id: PANDO-T-0012
type: task
title: "LSP hygiene: restartLSPClient closes old client, handle fsnotify.NewWatcher error, Create-stat error must not exit watcher loop"
status: in_review
priority: high
parent: PANDO-US-0125
author: mcp
labels: [lsp, watcher, bug]
created: 2026-10-09T12:25:37Z
updated: 2026-10-09T12:30:41Z
started: 2026-10-09T12:30:41Z
---

## Description
app/lsp.go:453-466 only sends `shutdown` (process + pipes leak). lsp/watcher/watcher.go:337 ignores NewWatcher error (nil watcher -> panic -> restart loop). watcher.go ~:444 `return` on os.Stat error kills watcher.
## Acceptance Criteria
Old client Close()d (shutdown+exit+Close), old watch ctx cancelled; no nil watcher use; `continue` instead of return; tests where feasible.
