---
created_at: 2026-10-13T10:24:43.885600515Z
updated_at: 2026-10-13T11:04:08.749357769Z
tags:
    - investigation
    - telemetry
    - darwin
    - copilot
---
# Investigation: two macOS users — Copilot panic + `pando app` not starting (2026-10-13)

Telemetry: Better Stack source 2751484 (`t596690_pando_logs` hot, `t596690_pando_s3` cold). Related: [[fix_macos_fd_watchers_copilot_empty_choices]], [[remote_telemetry_betterstack_summary]], [[sqlite-multiwriter-engine-v1.3.0]].

CORRECTION: the first version of this doc mixed the two users. The first darwin query grouped by debug_id over S3 only, which ended at 09:49, so the 10:11+ hot rows looked like Nazareth's. Always group by `debug_id` across both the hot and S3 legs.

## Nazareth — debug_id 5197-1123-6710-9997 (Pando Desktop)
- 08:12:13 v1.2.13 desktop runs for 8 s. From 08:12:38 it is v1.3.0 desktop, which runs until 10:44 in `~/GIT/app_ios_ivooxnew`, with 0 errors.
- v1.2.13 never appears again after v1.3.0 starts, so this is an upgrade, not two installs.
- The 71 `reindex file failed: code: get project: context canceled` warnings at 09:53:49 are shutdown noise.
- Only open issue: the code-index watcher estimates about 11.3k FDs (above the 10k threshold).

## Colleague — debug_id 7402-2673-1745-4679 (`pando app`, project ~/Documents/Xcode/iVoox/redesign/two/app_ios_ivooxnew)
- 10:11:53 v1.2.13 app. 10:12:04 `Panic in agent.Run: runtime error: index out of range [0] with length 0` + `panic while generating title`. This is the Copilot `choices: []` bug, fixed in v1.2.14. The turn still ended `end_turn`. Local file: `pando-panic-agent.Run-20261013-121205.log`.
- 10:12:38 and 10:44:57 v1.3.0 app starts. Both stop logging right after `IPC bus wired to session service`. There are no session, agent, or error lines after it. v1.2.13 does not appear again, so this was an upgrade.
- Code (`cmd/app.go`, `cmd/ipc_wiring.go`): on success nothing more is logged at info after `IPC bus wired`. "listening on" goes to stdout via fmt.Printf. A `server.Start()` error (e.g. web port in use) is returned to the terminal and never reaches telemetry. `StartBusWithRetry` failures would log Error, and there are none.
- So telemetry cannot tell three cases apart: (a) the web server failed (terminal error), (b) the UI/browser did not load, (c) the app was idle. Need the terminal output of `pando app` and the browser state.
- Watcher: estimated about 11.2k FDs in this project too.

## Gap worth fixing
`pando app`/`serve` should log the listen address at Info and log `server.Start()` errors through `logging.Error`, so startup failures reach telemetry.
