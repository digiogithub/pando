---
created_at: 2026-08-19T11:29:11.652940996Z
updated_at: 2026-08-19T11:29:11.652940996Z
tags:
    - fix
    - agentvcs
    - snapshots
    - webui
    - tui
---
# Fix: agent-VCS reported 100% of the project as changed on every session

Date: 2026-08-19

## Symptom

In the WebUI (`AgentVcsView`) and the TUI snapshots page, **every session** showed the
same fixed, maximal numbers: ~1933 files changed / ~138 MB. The change set never
reflected what the session actually modified.

## Root causes

Three independent defects, all confirmed against the on-disk store
(`.pando/data/agent-vcs`), where every session log contained exactly **one**
commit: `Session start: …` with `changed_file_count == file_count`.

1. **The session root commit was treated as a change set.**
   `service.createCommitFromEntries` computed `changedFiles = diffTrees(Tree{}, tree)`
   when `parentID == ""`, so the whole working tree was reported as "added".
   `DiffFromParent` did the same for root commits, so the detail panel listed
   every file in the project.

2. **No delta commit was ever recorded.** `session.EndSession` (the only caller of
   the `"end"` snapshot hook) is dead code — nothing in the repo calls it. The
   agent loop never recorded anything either. Only `session.Create` fired a
   `"start"` snapshot, hence exactly one commit per session.

3. **Both UIs displayed whole-tree stats.** The TUI (`page/snapshots.go`,
   `components/snapshots/table.go`, `details.go`) and the legacy snapshots API
   (`commitToResponse`) surfaced `FileCount` / `TotalSize` (the full tree)
   instead of the per-commit change stats.

Secondary problem: `scanner.Scan` re-hashed the entire working tree (138 MB) on
every commit, which made per-turn recording impractical.

## Changes

### `internal/agentvcs`

- `model.go`: added `Commit.IsBaseline` / `CommitSummary.IsBaseline`; added
  `Commit.normalize()`, which back-fills the flag and zeroes the bogus change
  stats for commits written by older versions (parentless commit == baseline).
- `storage.go`: `LoadCommit` calls `normalize()`, so legacy data is corrected on
  read without a migration.
- `service.go`:
  - the root commit of a session is now an explicit **baseline**: no change set,
    `ChangedFileCount = 0`, `ChangedTotalSize = 0`, `IsBaseline = true`. Its blobs
    are still stored for the *full* tree so revert and later diffs have a reference.
  - `DiffFromParent` returns an empty slice for a baseline.
  - new `SessionDiff(ctx, sessionID)` — aggregate baseline to HEAD diff, i.e. what
    this session changed.
  - `Record` passes the parent tree to the scanner as a hash cache, and drops
    cache entries whose `ModTime >= parentCommit.CreatedAt-1` (second-resolution
    modtimes could otherwise hide a same-second rewrite of identical size).
- `scanner.go`: new `ScanWithBaseline(rootDir, baseline)`; reuses a previous
  entry's hash when size and modtime match. `Scan` delegates with `nil`.

### Delta recording (the actual missing behaviour)

- `internal/session/session.go`: new exported `RecordSnapshot(sessionID, description)`
  helper — async, non-blocking, no-op without a snapshot creator.
- `internal/llm/agent/agent.go`: `runInternal` calls `session.RecordSnapshot` after
  every completed turn, guarded by `a.agentName == config.AgentCoder` so sub-agent /
  task sessions sharing the working directory do not duplicate deltas. New
  `turnSnapshotDescription(content)` builds the commit label from the prompt.

### API

- `handlers_agentvcs.go`: `is_baseline` in the log payload; new
  `GET /api/v1/agentvcs/sessions/{id}/diff` (`handleAgentVCSSessionDiff`); the
  sessions listing now carries `changed_files` / `changed_total_size` aggregates.
- `handlers_snapshots.go`: `SnapshotResponse.Size`/`FilesCount` now carry the
  *changed* amounts; whole-tree totals moved to new `tree_size` / `tree_files`;
  added `is_baseline`; commit type `"start"` renamed to `"baseline"`.
- `routes.go`: registered the session diff route.

### UI

- TUI: `CommitRow` gained `ChangedFiles` / `ChangedSize` / `IsBaseline`; the commits
  table shows the changed count/size (column `Files` renamed `Chg`), `typeIcon` renders
  a base marker, and the detail panel shows "Changed files/size" for deltas and a
  "session start state" line for the baseline.
- WebUI: `agentVcsStore.CommitSummary.is_baseline`; `AgentVcsView` badge START becomes
  BASELINE, baseline rows read "session start state - no changes", and the empty
  diff message no longer claims "all files were added".

## Verification

- `go build ./...`, `go vet` on all touched packages - clean.
- `go test ./internal/agentvcs ./internal/api ./internal/session ./internal/llm/agent ./internal/tui/...` - pass.
- `TestCreateCommitTracksChangedStats` rewritten for the new semantics (baseline
  has zero changes and an empty `DiffFromParent`; delta reports 1 file;
  `SessionDiff` returns exactly `main.go`).
- New `TestScanWithBaselineReusesHashes` proves unchanged files reuse the cached
  hash and modified files are re-hashed.
- `npx tsc --noEmit` in `web-ui` - clean.

## Related

[[agent-vcs-overview]]
