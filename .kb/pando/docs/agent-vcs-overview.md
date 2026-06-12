# Agent-VCS — Mini-jj for Pando

## What it is

Agent-VCS is a lightweight version control system inspired by Jujutsu (jj) that replaces Pando's old snapshot system. It automatically records changes to working directory files throughout each agent session, forming a linear chain of immutable commits per session.

## Goal

Maintain a history or log of file changes per session in the project files. Each agent session (conversation) produces a sequence of commits that allows reviewing what changed, when, and reverting to any point.

## Data model

- **Commit**: immutable, identified by content hash. Contains reference to tree, parent, session ID, description, timestamp, file stats.
- **Tree**: ordered list of TreeEntry (path, hash SHA-256, size, modtime). Stored by hash — if two commits have the same tree, it is deduplicated.
- **TreeEntry**: a file or directory within a Tree.
- **Blob**: gzip-compressed file content, stored by SHA-256 hash (content-addressable). Deduplicated across commits and sessions.
- **SessionLog**: ordered list of commit IDs for a session.
- **DiffEntry**: file-level change between two commits (added/modified/deleted).
- **CommitSummary**: lightweight view of a commit for listings (includes short ID and changed file count).

## Per-session flow

1. **Session Start** → `NewChange(sessionID, description)` — creates the root commit with a complete tree of the working directory.
2. **During the session** (agent loop) → `Record(sessionID, description)` — compares the current tree with the last commit; if the treeID changed, creates a new delta commit. If there are no changes, does nothing.
3. **Session End** → `Record(sessionID, "Session end: ...")` — final commit of the session.

The integration uses an Adapter that implements the `session.SnapshotCreator` interface, mapping "start" to NewChange and the rest to Record.

## Disk storage

```
{data-dir}/agent-vcs/
├── commits/{commitID}.json     # Immutable commit (JSON)
├── trees/{treeID}.json         # Tree snapshot (JSON)
├── blobs/{h[0:2]}/{h[2:4]}/{h} # Content-addressable gzip blobs
└── sessions/{sessionID}.json   # Commit chain per session
```

Stored alongside where snapshots used to be saved, under the configured data directory.

## Ignored file support

- Reads `.gitignore` and `.pandoignore` from the root directory up to the filesystem root.
- Always ignores `.git/` and `.pando/`.
- Applies `excludePatterns` from the snapshots config (e.g., `node_modules/`, `*.log`, `vendor/`).
- Respects the maximum file size limit (`maxFileSize` from config).
- Only scans directories that are recognized projects (with markers like `.git`, `go.mod`, etc.).

## Package files in `internal/agentvcs/`

| File | Role |
|------|------|
| `model.go` | Structs (Commit, Tree, TreeEntry, SessionLog, DiffEntry, CommitSummary), hash functions (computeCommitID, computeTreeID) |
| `storage.go` | Disk persistence: CRUD for commits, trees, blobs (gzip), session logs. Content-addressable with deduplication. |
| `scanner.go` | Working directory scanning with .gitignore/.pandoignore support and config exclude patterns |
| `service.go` | Main service: NewChange, Record, Log, Diff, DiffFromParent, Revert, Cleanup, ListSessions |
| `adapter.go` | Adapter implementing `session.SnapshotCreator` for session lifecycle integration |
| `agentvcs_test.go` | Unit tests: storage roundtrip, tree dedup, blob roundtrip, diff, scanner ignore, cleanup |

## Modified files in the integration

| File | Change |
|------|--------|
| `internal/app/app.go` | `AgentVCS` field replaces `Snapshots`. Initialized with `agentvcs.NewService()`. Cleanup on shutdown. |
| `cmd/root.go` | Pubsub subscriber changed to `app.AgentVCS` |
| `cmd/agentvcs.go` | **New**: 5 CLI subcommands (sessions, log, show, revert, compact) |
| `internal/api/handlers_snapshots.go` | Rewritten to delegate to AgentVCS while maintaining backward-compatible API |
| `internal/api/handlers_agentvcs.go` | **New**: native endpoints (sessions, log, commit, diff) |
| `internal/api/handlers_extras.go` | `/snapshots/count` counts commits via AgentVCS |
| `internal/api/routes.go` | Registers 4 new endpoints under `/api/v1/agentvcs/` |
| `internal/tui/tui.go` | Manual command uses `AgentVCS.Record` |
| `internal/tui/page/snapshots.go` | Loads commits from `AgentVCS.Log()` |
| `internal/tui/components/snapshots/table.go` | Uses `agentvcs.Commit` in pubsub events |
| `internal/tui/components/snapshots/details.go` | Updated for the new model |

## CLI Commands

```
pando agent-vcs sessions              # List sessions with commits
pando agent-vcs log <session-id>      # Commit chain of a session
pando agent-vcs show <commit-id>      # Commit detail + diff
pando agent-vcs revert <commit-id>    # Revert working dir to a commit (with safety backup)
pando agent-vcs compact --keep N      # Compact: keep only the N most recent sessions
```

Available alias: `pando avcs <command>`.

## REST API

Backward-compatible endpoints under `/api/v1/snapshots/` (delegating to AgentVCS) plus native endpoints:

```
GET  /api/v1/agentvcs/sessions              # List sessions
GET  /api/v1/agentvcs/sessions/{id}/log     # Commit log per session
GET  /api/v1/agentvcs/commits/{id}          # Commit detail + diff
GET  /api/v1/agentvcs/commits/{id}/diff     # Commit diff only
```

## Compaction and cleanup

- **Automatic** on app close: uses `cfg.Snapshots.AutoCleanupDays` and `cfg.Snapshots.MaxSnapshots` (default 100 sessions, 30 days).
- **Manual** via CLI: `pando avcs compact --keep 50 --days 30`.
- Sessions are sorted by last update date (newest-first); those exceeding the limit are pruned.
- After pruning sessions, orphan blobs not referenced by any tree are deleted.

## Configuration

Reuses the `snapshots` section from Pando's config:

```toml
[snapshots]
enabled = true
maxSnapshots = 100        # Max sessions to keep
maxFileSize = "10MB"      # Max file size
excludePatterns = ["node_modules/", ".git/", "vendor/", "*.log", "*.tmp"]
autoCleanupDays = 30      # Days before pruning old sessions
```
