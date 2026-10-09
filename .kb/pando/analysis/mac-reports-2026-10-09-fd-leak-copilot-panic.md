---
created_at: 2026-10-09T12:18:34.02684588Z
updated_at: 2026-10-09T12:18:34.02684588Z
tags:
    - analysis
---
# Analysis: macOS user reports 2026-10-09 (FD leak + agent.Run panics)

Status: analysis only, no code changed. Pending José's approval for fixes.

## 1. Panic "agent.Run" (debugID 5197-1123-6710-9997, v1.2.13 darwin, Copilot + Haiku)
Stack (Better Stack source 2751484): `runtime error: index out of range [0] with length 0` at
`internal/llm/provider/copilot.go:468` (`copilotResponse.Choices[0]` in non-streaming `send`),
called from `agent.generateTitle` (agent.go:971) in title goroutine (agent.go:1279-1283).
Copilot returned HTTP 200 with empty `choices`. Non-fatal: recovered; only the session title is lost.
The RecoverPanic label "agent.Run" for the title goroutine is misleading.
Same unguarded pattern: copilot.go:473 and streaming copilot.go:613/626/627; openai.go:296/305 (non-stream).
Fix: guard `len(Choices)==0` -> retryable error (or empty response), same in stream path; rename label to "agent.generateTitle".
Secondary: logging.RecoverPanic writes `pando-panic-*.log` with relative os.Create (logger.go:108) -> lands in project CWD; write under logs/data dir.

## 2. FD leak on macOS (Xcode/iOS project)
fsnotify v1.9 kqueue backend opens 1 FD per watched dir AND per file inside each watched dir.
Recursive watchers over the whole workspace, each its own fsnotify instance:
- LSP workspace watcher per LSP client (internal/lsp/watcher/watcher.go:356-385), started in app/lsp.go:396-411
- LSP bootstrap watcher (app/lsp_bootstrap.go:55, opt-in activateOn=workspace)
- remembrances code watcher (app/remembrances_watch.go:174-209)
FDs ~= files x number of watchers. No Xcode exclusions anywhere (Pods, DerivedData, xcuserdata, Carthage, *.xcassets); hardcoded lists differ per watcher (watcher.go:760, lsp_bootstrap.go:19, remembrances_watch.go:191); .gitignore not honored; not configurable ("TODO: make configurable").
Leak amplifiers:
- restartLSPClient (app/lsp.go:453-466) only sends LSP `shutdown`, never `exit`/Client.Close(): old process + 3 pipe FDs + goroutines leak per restart; old watchCtx cancel never called.
- WatchWorkspace ignores fsnotify.NewWatcher error (watcher.go:337-340) -> nil watcher -> panic on Add -> RecoverPanic triggers restartLSPClient -> more leaks. Under EMFILE this cascades.
- watcher.go:~444 `return` on os.Stat error for Create events kills the LSP watcher silently (Xcode temp files).
Fix: shared exclusion list (+Pods, DerivedData, xcuserdata, Carthage, .build, *.xcarchive) + user config `watchExclude` + .gitignore via internal/search/ignore.go; single shared workspace watcher fan-out to LSP clients; Close() old client in restart; handle NewWatcher error; `continue` instead of `return`. Longer-term: FSEvents backend on darwin (1 FD per tree).
Workaround users: disable LSP for project / move DerivedData to default ~/Library location, remove in-project build dirs.
