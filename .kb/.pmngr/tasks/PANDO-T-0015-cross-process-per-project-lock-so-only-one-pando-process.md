---
id: PANDO-T-0015
type: task
title: Cross-process per-project lock so only one Pando process (incl. mcp-server) runs the code-index watcher
status: in_review
priority: high
parent: PANDO-US-0125
author: mcp
labels: [watcher, mcp-server, macos]
created: 2026-10-09T12:28:44Z
updated: 2026-10-09T12:30:18Z
started: 2026-10-09T12:30:18Z
---

## Description
T-0011 gates on IPC primary, but `pando mcp-server` (cmd/mcp_server.go:125) never joins IPC, so every mcp-server process still watches (the reported case: 4 Claude Code sessions). Add a non-blocking flock (like internal/ipc/lock_unix.go / lock_windows.go) on a per-project lock file; holder runs startup index + watcher; others retry periodically and take over when the holder exits.
## Acceptance Criteria
N concurrent processes on same project => exactly one code-index watcher; takeover after holder exit; tests.
