---
created_at: 2026-10-13T11:22:20.71899993Z
updated_at: 2026-10-13T11:22:20.71899993Z
tags:
    - investigation
    - darwin
    - sqlite
    - multiwriter
    - bug
---
# macOS: every pando.db write hangs in v1.3.0 (multiwriter flock + fcntl on the same file) — 2026-10-13

Users: Nazareth `5197-1123-6710-9997` (desktop/TUI) and a colleague `7402-2673-1745-4679` (`pando app`/TUI), both darwin. Earlier notes: [[pando/investigations/2026-10-13-nazareth-darwin-copilot-startup.md]]. Engine: [[sqlite-multiwriter-engine-v1.3.0]].

## Symptoms
- TUI freezes after startup, and Ctrl-C does nothing, so the user has to kill it (no `App shutdown started` even with debug on).
- `pando app` "does not start": it logs nothing after `IPC bus wired`.
- Desktop opens and lists sessions (reads) but never completes a write.

## Telemetry evidence (Better Stack 2751484)
- On darwin, v1.3.0 has zero `Result` and zero `agentvcs: commit created` lines. v1.2.13 on the same Macs has both, and linux v1.3.0 has 150 `Result`.
- Nazareth's desktop runs 08:12, 09:06 and 09:14 needed no KB write (`161 unchanged`) and worked.
- From 09:49 on, KB doc `ivoox-ios/changes/agents-md-worktree-alignment-and-skill-catalog.md` changed. In all 8 later startups (09:49, 10:29, 10:34, 10:44, 11:11, 11:13, 11:14, 11:15 tui, 11:17), `kb sync: indexing document` never reaches `kb sync: completed`.
- v1.2.14 to v1.3.0 contains only commit 839540537 (multiwriter engine).

## Root cause (code)
go-sqlite-multiwriter v0.1.1 uses ONE file (`lockF`, `pando.db-mwlock`) for two lock kinds:
- membership: `flock(LOCK_SH)`, held for the whole process lifetime (`mp.go:117/171`, `mp_unix.go flockTry/flockWait`);
- publication of every commit: `fcntl F_SETLKW` F_WRLCK on byte 1 (`mp.go:608`, `mp_unix.go byteLock`); prefill (byte 3) and compact (byte 2) use the same file.

On Linux, flock and POSIX locks are independent. On macOS/BSD (XNU), flock is implemented via VNOP_ADVLOCK/F_FLOCK on the same per-vnode lock list as POSIX locks, with a different owner (open file vs process). The process's own shared flock therefore conflicts with its own F_WRLCK, and F_SETLKW blocks forever. Reads are unaffected. Linux check: `flock(SH)` then `lockf(EX|NB,1,1)` succeeds. The same test on a Mac should fail with EAGAIN; this still needs to be confirmed on a Mac.

## Workaround
`PANDO_DB_ENGINE=sqlite` for EVERY process, including the Desktop app (GUI apps need `launchctl setenv PANDO_DB_ENGINE sqlite`). A stock opener refuses the DB while `pando.db-mw` exists; killed processes can leave it behind.

## Fix options
1. Library v0.1.2: move membership to its own file (e.g. `-mwmember`), or implement membership as an fcntl F_RDLCK on byte 0 instead of flock, so the two lock kinds never share a file. Add a darwin CI test that commits with MultiProcess.
2. Stopgap in pando: default to EngineSQLite on darwin, handling existing `-mw` files.
Then release v1.3.1. Local cross builds are not possible (CI builds darwin), so verification needs a Mac.
