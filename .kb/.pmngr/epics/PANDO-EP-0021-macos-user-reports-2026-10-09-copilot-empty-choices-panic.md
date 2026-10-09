---
id: PANDO-EP-0021
type: epic
title: "macOS user reports 2026-10-09: Copilot empty-choices panic and file-watcher FD exhaustion"
status: backlog
priority: high
author: mcp
labels: [macos, bug, watcher, copilot]
created: 2026-10-09T12:25:05Z
updated: 2026-10-09T12:25:05Z
---

## Description
Two issues reported by macOS users on 2026-10-09.

1. `Panic in agent.Run: index out of range [0] with length 0` at `internal/llm/provider/copilot.go:468` in title generation (Copilot + Haiku returned HTTP 200 with empty `choices`). debugID 5197-1123-6710-9997, v1.2.13.
2. File descriptor exhaustion with `pando mcp-server` on a Capacitor/iOS repo: fsnotify kqueue opens one FD per watched file and directory; DerivedData/Pods are watched; each instance watches independently (~32k FDs per instance, 4 sessions filled `kern.maxfiles`).

Analysis: KB `pando/analysis/mac-reports-2026-10-09-fd-leak-copilot-panic.md`.

## Acceptance Criteria
- Empty `choices` from Copilot/OpenAI-compatible providers never panics.
- Watchers honor `.gitignore`, built-in defaults (incl. Xcode) and a `watchExclude` config option before adding paths.
- Only the IPC primary instance runs the code-index watcher.
- LSP clients share one workspace watcher.
- LSP restart closes the old client; watcher errors handled.
- Warning when watched paths exceed 10,000.
