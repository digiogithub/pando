---
created_at: 2026-09-11T23:07:02.123996247Z
updated_at: 2026-09-11T23:07:02.123996247Z
tags:
    - change
    - ipc
    - tests
    - failover
    - docs
---
# Change: P6 — multi-process tests, the G7 kill policy, and the stale-doc cleanup (2026-09-12)

Implements phase **P6** of [[pando/plans/mcp_server_ipc_bootstrap.md]] (§4 G7, §7 P6, §8, §9), the last phase of that plan. With it, **every phase (P0-P6) and every gap (G1-G7) is implemented.** Builds on [[pando/fixes/ipc_failover_p0_inplace_promotion.md]], [[pando/changes/ipc_wiring_p1_shared_wireipc.md]], [[pando/changes/mcp_server_ipc_bootstrap_p2.md]], [[pando/changes/ipc_role_aware_services_p3.md]], [[pando/changes/ipc_other_entrypoints_p4.md]], [[pando/changes/ipc_direct_writers_p5.md]] and [[pando/fixes/config_update_zero_value_overwrite.md]].

## 1. Multi-process tests (`tests/test_ipc_multiprocess.py`, new)

The first tests that exercise the topology the way users actually run it: several real processes, one shared project database, and one of them dying. Unit tests cannot reach this — an in-process test can fake a promotion, but not an OS-level flock release, a SIGKILLed primary, or a stdio MCP client closing its pipe.

### Structure
- Python `unittest` (the repo convention: tests live in `tests/`, pytest is not installed, and `python3 -m unittest` works), following `tests/test_telemetry_cli.py` and `tests/test_cronjob_cli.py`: build the binary once in `setUpModule`, drive the real CLI over subprocesses, skip rather than fail when the environment cannot support the test.
- The binary is built into a **temp directory** (`tempfile.mkdtemp`), not the repo, and removed in `tearDownModule`.
- **Isolation is mandatory and structural.** A namespace has to be entered by a whole process, so each test re-executes the test file itself inside `unshare -Urmn` (`--scenario <name>`). Inside, before anything runs: `ip link set lo up`, then a **tmpfs over `/tmp/pando-instances`**. `HOME`, `XDG_CONFIG_HOME` and the project directory are throwaway paths, and the scratch `.pando.toml` sets `[Data] Directory` explicitly (a config rewrite blanks absent keys — [[pando/fixes/config_update_zero_value_overwrite.md]]).
  - The private **network** namespace is the important part: the deterministic ports `ipc.PortsForPath` derives cannot reach, or be reached by, a real Pando instance on the host. Combined with the tmpfs registry and the scratch HOME, the suite cannot read, write, lock, kill or signal anything it did not create.
- The child prints one `PASS`/`FAIL` line per assertion and exits non-zero if any failed; the parent test method asserts exit 0 and, on failure, reports the child's whole output, so a failure reads normally in unittest output.
- A `finally` block kills any surviving process matching the scratch binary path and asserts none was left.

### Scenarios and results (7/7 pass, 141 assertions, 23.1s total)

| Scenario | What it proves | Result |
|---|---|---|
| `join` | `pando mcp-server --no-http` starts first and is primary (real `initialize` over stdio), an ACP secondary joins. The lock-file PID, `pando ipc status` ("Known instances for this path: 2", `Mode: mcp`) and the registry agree; exactly one entry is `is_primary`, with modes `mcp` and `acp`. | PASS |
| `stop_eof` | The mcp-server primary is stopped by closing its stdin. | Promoted, exit 0 |
| `stop_term` | ...by SIGTERM. | Promoted **0.11s** after the stop, exit 0 |
| `stop_kill` | ...by SIGKILL (no defers run; the kernel frees the flock). | Promoted **1.47s** after the kill — much faster than the 15s heartbeat timeout, because the secondary's SUB socket sees the disconnect |
| `concurrent` | 3 concurrent `mcp-server`s plus an ACP instance. | Exactly one primary across 4 registry entries, exactly **one** process logged the primary-only services, and all 4 writes landed |
| `cronjob` | `cronjob run` next to a primary; then a cron edit through a `serve` secondary's REST API. | One-shot exits 0 in **0.48s**, starts nothing, never promotes, primary untouched; the edit reached the primary over `cronjob.reload` in **0.023s** and was scheduled exactly once |
| `handover_write` | A KB write issued while the primary gracefully hands over. | The write succeeded on the **new** primary and the row was gone from the DB (P5's handover retry) |

Each of the three `stop_*` scenarios additionally asserts, with two ACP secondaries running:
- exactly **one** of them is promoted, never both;
- the promoted one starts the primary-only services with `trigger=promotion`, and the one that was not promoted still logs **none** (P3's role gating, across a real promotion);
- the lock file then names the promoted process;
- a `session/new` on the new primary writes a real row;
- a **freshly started** `mcp-server` joins as a secondary to the new primary and its KB write (`forget`) is applied — i.e. the new topology accepts new members and forwards their writes.

Writes are deliberately chosen to need no embedding provider (the namespace has no route to one): rows are seeded with plain SQL and deleted through the `forget` tool, which takes the same always-forwarded remembrances path. Session writes use ACP `session/new`.

### Running them
```
python3 -m unittest tests.test_ipc_multiprocess -v
python3 -m unittest tests.test_ipc_multiprocess.IPCMultiProcessTest.test_join
```
**They do not run in normal CI.** This repository has no `tests/` runner and no CI job for that directory (no pytest configuration; the Go workflow runs only `go test`), so they are run on demand by whoever touches the IPC code. They skip cleanly when `go` is missing, when `unshare` is missing, or when the kernel refuses unprivileged user namespaces.

## 2. G7 — the kill policy (`internal/ipc/runtime/`)

**Decision: never kill a suspended primary; keep killing an unresponsive but running one.**

A secondary that gets no answer to `ipc.ping` used to SIGKILL the lock holder. A process stopped by job control (`^Z`, `kill -STOP`) or halted in a debugger cannot answer that probe **by definition**, yet it is healthy and its user intends to resume it. Killing it destroys real work, and an editor-spawned `mcp-server` is exactly the kind of process that would do so to someone's TUI.

- New `processState(pid)` reads field 3 of `/proc/<pid>/stat` (parsed from the **last** `)`, since `comm` may contain spaces and parentheses). `procstate_linux.go` implements it; `procstate_other.go` returns "unknown" behind `//go:build !linux`.
- `killStalePrimary` now refuses to kill a primary in state `T` (job-control stop) or `t` (ptrace stop), logging the state, the reason, and the exact command to resume it (`kill -CONT <pid>`) or stop it deliberately. It still kills `R`/`S`/`D`, zombies, and processes that are already gone.
- **Portability:** only Linux can read the state. Elsewhere `procStateSupported` is false, `processState` reports "unknown", and the long-standing behaviour is kept — an honest fallback rather than a guess. macOS would need `sysctl(KERN_PROC)`; Windows a different model entirely.
- The narrowing is real but small: `mcp-server` and `cronjob run` already pass `AllowKillStalePrimary: false` (P2/P4), so this only affects TUI/ACP/serve/desktop/app, which are the entrypoints allowed to kill at all.

Tests (`internal/ipc/runtime/procstate_test.go`): the state-letter classification; a real child observed running, then SIGSTOPped, then resumed; an unreadable PID reporting "unknown"; `killStalePrimary` leaving a **SIGSTOPped** lock holder alive and still suspended; and the control case, where a running but unresponsive holder is still killed. The suspended test must not reap the child — a stopped process never exits, so `Wait()` would block forever; the helper's own cleanup SIGKILLs it first.

## 3. De-flaking `TestCronJobReloadRPCReachesPrimary` (`cmd/`)

**Root cause (two parts, neither a product bug):**
1. `ipc.PortsForPath` maps a path into **40000-60000**, which overlaps this platform's ephemeral port range (`ip_local_port_range` is 32768-60999 by default). Any unrelated process on the machine can transiently hold the port a given temp directory hashes to.
2. When that happens, `wirePrimary` does not fail: by design it logs `"failed to start bus, continuing without IPC"` and carries on (matching Bootstrap's own fallback). The test had no way to notice, so the forwarded RPC failed later with a bare `dial ROUTER endpoint ... connection refused`.

**Fixes**, both in the test layer, since the product behaviour (degrade rather than refuse to start) is deliberate:
- `projectWithFreeIPCPorts` picks a temp project directory whose two derived ports are bindable right now, retrying with a different directory (a different hash) up to 20 times, and skipping with a clear message if the machine really has no free range.
- `waitForPrimaryBus` polls `ipc.ping` for up to 10s, dropping the cached DEALER between attempts, and fails with a message that names the actual cause. `pingOverRPC` now routes through it, which also removes a second latent race: a **promoted** primary binds with a retry loop of its own (the old primary's sockets linger ~100ms), so the previous single 2s probe could race that window.
- Both RPC-driving siblings — `TestCronJobReloadRPCReachesPrimary` and `TestKBRelinkForwardsToPrimaryElseRunsLocally` — now gate on the bus answering before forwarding anything.

**Proof:** `go test -count=20 -run TestCronJobReloadRPCReachesPrimary ./cmd/` passes 20/20.

## 4. A second flake found and fixed: `TestHandoverRefusalsClassifyAsUnavailable`

Running the full `-race` sweep surfaced an unrelated failure in `internal/ipc/writecoordinator` (~8 of 20 runs, 0 of 1 without `-race`). It is **not timing**: `Submit` on a shut-down coordinator has two exits, and the enqueue `select` has both `<-c.done` and the buffered `c.jobs <- job` ready at once, so Go picks one at random.

- Refused **before** queueing → `"coordinator is shut down"` → `ErrCodeUnavailable` (retryable: the write provably never ran).
- Enqueued, then caught by `<-c.done` → `"coordinator shut down while waiting for result"` → `ErrCodeTimeout` (ambiguous: the job may have been dequeued and executed).

The test asserted `Unavailable` for both. The classification is correct as designed (P5 chose `TIMEOUT` for the ambiguous case so a typed create is not re-sent), so the **test** was fixed to pin each text to its own class. 20/20 under `-race` afterwards.

## 5. Files

- **New:** `tests/test_ipc_multiprocess.py`, `internal/ipc/runtime/procstate_linux.go`, `internal/ipc/runtime/procstate_other.go`, `internal/ipc/runtime/procstate_test.go`.
- `internal/ipc/runtime/runtime.go`: the suspended-primary guard in `killStalePrimary`, plus `isSuspendedState` and `suspendedStateReason`.
- `cmd/ipc_wiring_test.go`: `projectWithFreeIPCPorts`, `portFree`, `waitForPrimaryBus`; `isolatedIPCProject` and `pingOverRPC` now use them.
- `cmd/ipc_p4_test.go`: both RPC tests gate on the bus.
- `internal/ipc/writecoordinator/handover_text_test.go`: the classification fix.
- Docs: `pando/plans/mcp_server_ipc_bootstrap.md` (status, §3, §4 G7, §7 P6, §9), `pando/plans/unified_single_writer_master_plan.md` and all seven `unified_single_writer_phase*.md`.

## 6. Docs corrected

The `unified_single_writer_*` documents all still said "not started" while the code had implemented almost all of them. They were **patched, not rewritten**: each status line now reflects reality and each carries a short "as of 2026-09-12" note linking this plan and the P0-P5 change docs. Statuses now read: phase 1 bootstrap **implemented** (and extended to mcp-server/agui-serve/cronjob run); phase 2 write contract **implemented** (plus `ErrCodeUnavailable` and the ~20s handover wait); phase 3 writecoordinator **implemented**; phase 4 changepub **implemented** (consumers still only log); phase 5 failover **implemented, but correct only since 2026-09-11**; phase 6 observability **partial**; phase 7 multi-process tests **implemented 2026-09-12** by this phase. Two design statements that the master plan still asserted are now flagged: the entrypoint list grew, and secondaries are not read-only. The historical §1 gap table and "known limitations" list are marked as such rather than deleted.

## 7. Verification

- `go build ./...`, `go vet ./internal/ipc/runtime/ ./cmd/`, `gofmt -l` on every touched file: clean.
- `go test -race -count=1 ./internal/ipc/... ./cmd/... ./internal/app/...`: **all ok**, no races.
- `go test -count=20 -run TestCronJobReloadRPCReachesPrimary ./cmd/`: 20/20.
- `go test -race -count=20 ./internal/ipc/writecoordinator/`: ok.
- `python3 -m unittest tests.test_ipc_multiprocess -v`: **7/7, 141 assertions, no failures, 23.1s**, run inside the sandbox described above. The real `/www/MCP/Pando/pando/.pando/` and the host's `/tmp/pando-instances` were never touched, and no process outside the sandbox was signalled.

## 8. Follow-ups and residual risks

- **A write enqueued into an already-shut-down coordinator is reported ambiguously** (see §4). It provably never runs, but `Submit` cannot distinguish "queued and abandoned" from "dequeued and executing", so a typed write is not re-sent even though it is safe to do so. Narrow; fixing it means tracking per-job dequeue state. Documented, not fixed.
- **The port derivation overlaps the ephemeral range** (§3). The tests now work around it, but a *real* instance can hit the same collision and silently run without IPC. Worth considering: fail loudly, retry a nearby port, or move the range above 61000.
- **`stop_kill` promoted in 1.47s**, far faster than the documented ~15s heartbeat path, because the SUB socket notices the disconnect. The 15s figure quoted in the plan and earlier docs is therefore a worst case, not the normal one.
- The suite is opt-in and not in CI (§1). If it is ever wired into CI, it needs a runner for `tests/` and a kernel that allows unprivileged user namespaces.
- Non-Linux platforms keep the old kill behaviour for suspended primaries (§2).

Links: [[pando/plans/mcp_server_ipc_bootstrap.md]], [[pando/fixes/ipc_failover_p0_inplace_promotion.md]], [[pando/changes/ipc_wiring_p1_shared_wireipc.md]], [[pando/changes/mcp_server_ipc_bootstrap_p2.md]], [[pando/changes/ipc_role_aware_services_p3.md]], [[pando/changes/ipc_other_entrypoints_p4.md]], [[pando/changes/ipc_direct_writers_p5.md]], [[pando/fixes/config_update_zero_value_overwrite.md]], [[pando/plans/unified_single_writer_master_plan.md]], [[pando/plans/unified_single_writer_phase7_integration_tests.md]]
