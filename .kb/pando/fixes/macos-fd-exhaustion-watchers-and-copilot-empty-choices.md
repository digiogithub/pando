---
created_at: 2026-10-09T12:36:04.647578656Z
updated_at: 2026-10-09T12:36:04.647578656Z
tags:
    - fix
    - watcher
    - macos
    - copilot
---
# Fix: macOS FD exhaustion by file watchers + Copilot empty-choices panic (EP-0021)

Date: 2026-10-09. Status: implemented, in_review, NOT committed. gintrack PANDO-EP-0021 (US-0124, US-0125, T-0009..T-0015). Analysis: [[pando/analysis/mac-reports-2026-10-09-fd-leak-copilot-panic.md]].

## Copilot panic (T-0009)
- copilot.go send: `len(Choices)==0` -> retry with `emptyChoicesBackoff` (500ms*attempt, cap 3s) up to retryLimit, then error. Stream: guard Choices[0] (complete with FinishReasonUnknown). openai.go send: same guard.
- agent.go title goroutine RecoverPanic label "agent.generateTitle".
- Tests: internal/llm/provider/empty_choices_test.go.

## Watchers (T-0010..T-0015)
- New package internal/fswatch:
  - exclude.go `Excluder` (NewExcluder(root, extra), ShouldSkipDir): dot dirs, defaults (union of old lists + DerivedData, Pods, Carthage, xcuserdata, .build, .swiftpm, .gradle, .next, .nuxt, .dart_tool, .venv, venv, *.xcarchive, *.dSYM, *.xcresult), config `WatchExclude` (doublestar, bare name = any depth), .gitignore/.pandoignore (root + ancestors only up to the enclosing git root; nested loaded lazily). `search.IgnoreMatcher.AddDir` added.
  - hub.go `Hub`: one shared fsnotify watcher for all LSP clients + LSP bootstrap watcher, fan-out subscriptions (buffer 1024, non-blocking), PathCount; closed in App.Shutdown.
  - lock*.go: `TryLock`/`TryLockDir`, `<workdir>/.pando/code-watch.lock` (flock / LockFileEx).
  - budget.go: `WatchBudget`, warn once when estimated FDs > 10,000 (darwin/BSD: dirs+files; else dirs).
- Config: `Config.WatchExclude []string` (toml `WatchExclude`), commented example in init.go template.
- remembrances_code.go: code index + watcher skipped on IPC secondaries (deferred, started on PromoteToPrimary) and guarded by cross-process lock (retry every 30s, takeover) — covers mcp-server which does not join IPC.
- LSP: per-client file-watch registration handler on lsp.Client (`SetFileWatchHandler`, early registrations queued); restartLSPClient disposes old client (shutdown+exit+Close), cancels its watcher, restart limiter 3/5min then markLSPUnavailable; NewWatcher error no longer panics; Create stat error continues.

## Verification
go build ./...; go vet (linux all, darwin fswatch+lsp, windows fswatch); go test fswatch (incl -race), app, lsp/..., llm/provider, llm/agent, api, config, search, ipc/... all ok. Real multi-process smoke test on Mac pending (needs embeddings config).

## Follow-ups
- FSEvents backend on darwin (not done).
- Config reload does not rebuild excluders (restart needed).
