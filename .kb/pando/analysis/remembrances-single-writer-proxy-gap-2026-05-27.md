# Remembrances single-writer proxy gap analysis

Date: 2026-05-27
Project: pando

## Summary

Pando already implements a primary/secondary single-writer SQLite model for the main `db.Querier` write path:

- the primary instance acquires the IPC lock and opens SQLite read-write;
- secondary instances open SQLite in read-only mode;
- secondary writes that go through `db.Querier` are forwarded to the primary through `db.write` using `internal/ipc/dbproxy`.

This is correctly wired for sessions, messages, several file/project writes, and some self-improvement operations.

However, remembrances mutating operations do **not** currently follow that contract completely. KB, events, and code indexing stores use `*sql.DB` directly and perform `BeginTx` / `ExecContext` writes locally. In a secondary instance that means:

- reads work against the local read-only connection;
- writes do **not** get redirected to the primary;
- writes will fail on the read-only SQLite connection instead of using the single-writer proxy path.

## Confirmed current architecture

### Single-writer path that already works

Secondary bootstrap uses:

- `internal/ipc/runtime/runtime.go`
  - primary: `db.Connect()`
  - secondary: `db.ConnectReadOnly()`
  - secondary querier: `dbproxy.New(db.New(roConn), ipcClient, rpcAddr)`

Primary registers `db.write` handler through:

- `cmd/root.go`
- `cmd/serve.go`
- `internal/ipc/dbproxy/handlers.go`
- `internal/ipc/writecoordinator/coordinator.go`

Current `db.write` coverage is centered on methods implemented by `db.Querier`.

### Remembrances path that bypasses proxy

`internal/rag/service.go` constructs remembrances stores using the raw SQLite connection:

- `kb.NewKBStore(db, ...)`
- `events.NewEventStore(db, ...)`
- `code.NewCodeIndexer(db, ...)`

These stores use direct SQL writes rather than `db.Querier`.

## Exact mutating operations that need proxy-aware handling

### KB operations

#### Direct mutating methods

- `internal/rag/kb/kb.go`
  - `KBStore.AddDocument`
  - `KBStore.UpdateDocument`
  - `KBStore.DeleteDocument`
  - `KBStore.RebuildFTS` (not urgent for current tool flow, but mutating)

#### Indirect callers that trigger those writes

- `internal/llm/tools/remembrances_kb.go`
  - `KBAddDocumentTool.Run`
  - `KBImportPathTool.Run`
  - `KBDeleteDocumentTool.Run`
- `internal/app/remembrances.go`
  - `App.initRemembrancesKBSync` → `SyncDirectoryWithStats`
- `internal/rag/kb/sync.go`
  - `KBStore.SyncDirectoryWithStats` → add/update/delete
- `internal/rag/kb/watcher.go`
  - `KBStore.handleWatchEvent` → add/update/delete

#### Observed SQL behaviors to preserve

- insert/update/delete in `kb_documents`
- insert/delete in `kb_chunks`
- FTS maintenance in `kb_fts`
- chunking and embedding generation must still happen before committing the write payload, or be moved server-side in a controlled way

### Event operations

#### Direct mutating methods

- `internal/rag/events/events.go`
  - `EventStore.SaveEvent`
  - `EventStore.DeleteEvent` (not currently a commonly exposed tool path, but mutating)

#### Indirect callers

- `internal/llm/tools/remembrances_events.go`
  - `SaveEventTool.Run`
- `internal/app/remembrances_indexer.go`
  - `App.indexSessionConversation` → `svc.Events.SaveEvent(...)`

#### Observed SQL behaviors to preserve

- insert/delete in `events`
- FTS maintenance in `events_fts`
- embedding generation currently happens client-side before the DB transaction

### Code indexing operations

#### Direct mutating methods

- `internal/rag/code/indexer.go`
  - `CodeIndexer.IndexProject`
    - upsert project row in `code_projects`
    - update indexing status to failed/completed from background goroutine
  - `CodeIndexer.indexFile`
    - insert/update `code_files`
    - delete old `code_symbols`
    - insert new `code_symbols`
  - `CodeIndexer.embedSymbols`
    - update `code_symbols.embedding`
  - `CodeIndexer.ReindexFile`
    - reuses `indexFile`
  - `CodeIndexer.DeleteFile`
  - `CodeIndexer.DeleteProject`
  - `CodeIndexer.updateLanguageStats` (called after indexing/deletions, mutating)

#### Indirect callers

- `internal/llm/tools/remembrances_code.go`
  - `CodeIndexProjectTool.Run`
  - `CodeReindexFileTool.Run`
  - `CodeDeleteProjectTool.Run`
- `internal/app/remembrances.go`
  - automatic project indexing setup
- API / Mesnada remembrances endpoints also delegate into the same store

#### Observed SQL behaviors to preserve

- project lifecycle/status rows in `code_projects`
- per-file upsert and symbol refresh in `code_files` / `code_symbols`
- later embedding updates on symbols
- background job map (`jobs`) is in-memory and should remain local to the running instance even if writes are proxied

## Why this is not a small `db.Querier` fix

The current proxy contract is tied to `db.Querier`, but remembrances writes are custom SQL workflows, not sqlc-generated querier methods. That means the fix is architectural, not just wiring.

## Recommended implementation direction

The safest path is to extend `db.write` with explicit remembrances RPC methods.

### Recommended new proxied method family

#### KB

- `KBAddDocument`
- `KBUpdateDocument`
- `KBDeleteDocument`
- optionally `KBRebuildFTS`

Suggested payload for add/update:

- `file_path`
- `content`
- `metadata`
- precomputed `chunks`
- precomputed `embeddings`

This avoids recomputing embeddings on the primary and keeps the write deterministic.

#### Events

- `SaveEvent`
- optionally `DeleteEvent`

Suggested payload:

- `subject`
- `content`
- `metadata`
- precomputed `embedding`

#### Code indexing

Likely required methods:

- `CodeUpsertProject`
- `CodeSetProjectStatus`
- `CodeIndexFile`
- `CodeDeleteFile`
- `CodeDeleteProject`
- `CodeUpdateLanguageStats`
- optionally `CodeSetSymbolEmbeddings`

For file indexing the payload should contain already parsed symbol data rather than raw source only, because parsing happens outside SQLite and already occurs in the caller before SQL insertion.

## Critical design constraints for the future implementation

1. **Secondary instances must remain read-only at the SQLite layer**
   - no fallback local write path should remain active when proxy is configured.

2. **Reads should remain local where possible**
   - KB search, KB get, event search, code search, project stats, symbol overview, etc. can continue reading from the local read-only DB.

3. **Heavy computation should probably stay on the caller**
   - chunking
   - embedding generation
   - tree-sitter parsing
   - symbol extraction

   Then the primary only serializes the final database mutation.

4. **In-memory indexing job state is local process state**
   - proxying DB writes does not automatically solve cross-instance `job_id` visibility.
   - if a secondary launches indexing and writes through the primary, `code_index_status` semantics need explicit design:
     - either job tracking stays local to the launching instance,
     - or job state is also made primary-owned / persisted.

5. **Background remembrances tasks need review**
   - KB watcher/import and session auto-indexing can run inside secondary processes today.
   - after proxying writes, this may be acceptable, but may still duplicate expensive work across instances.
   - a later design decision may be to run some background indexing only on the primary.

## Risks discovered during the attempted implementation session

A partial implementation attempt was started but intentionally not completed because it was not yet safe:

- remembrances service construction was being adapted to optionally receive a `dbproxy.DBProxy`;
- stores were being extended with `SetWriteProxy`-style hooks;
- helper methods were being added in `internal/ipc/dbproxy/proxy.go` for generic non-`db.Querier` proxied writes.

But the full round-trip was **not** completed:

- RPC handlers for the new remembrances methods were not fully implemented;
- tests were not updated end-to-end;
- code indexing payload design still needed careful review;
- build/test verification had not completed.

Because of that, the implementation should be restarted carefully in a fresh session from a clean, reviewed state.

## Suggested next steps for a new session

1. **Start by inspecting current git state**
   - identify and either revert or complete any partial changes in:
     - `internal/rag/service.go`
     - `internal/app/app.go`
     - `internal/rag/kb/kb.go`
     - `internal/rag/events/events.go`
     - `internal/rag/code/indexer.go`
     - `internal/ipc/dbproxy/proxy.go`

2. **Implement a minimal generic proxy helper layer first**
   - add exported helpers in `internal/ipc/dbproxy/proxy.go` for:
     - void writes with timeout/retry
     - typed result writes
   - keep this narrow and tested before changing stores.

3. **Extend `internal/ipc/dbproxy/handlers.go`**
   - add explicit `switch` cases for:
     - KB add/update/delete
     - event save/delete
     - code project/file/indexing status operations
   - keep request structs local and explicit.

4. **Refactor stores to be proxy-aware but read-local**
   - KBStore:
     - add optional proxy field
     - route add/update/delete through proxy when present
   - EventStore:
     - route save/delete through proxy when present
   - CodeIndexer:
     - route all mutating SQL operations through proxy when present
     - preserve parsing/embedding work on caller side

5. **Review job-status semantics for code indexing before finalizing**
   - define whether `code_index_status` must work for jobs initiated from secondaries.
   - if yes, add explicit cross-instance job tracking design.

6. **Add focused tests before broad integration testing**
   - `internal/ipc/dbproxy/handlers_test.go`
     - one test per new remembrances method family
   - store-level tests verifying:
     - local direct write path still works without proxy
     - proxy path calls the right RPC method when proxy is present

7. **Run verification commands**
   Suggested minimum:
   - `go test ./internal/ipc/dbproxy ./internal/rag/... ./internal/app`
   - any remembrances-specific API/tool tests available
   - then broader targeted commands from project notes as needed

8. **After the implementation is complete and verified, store a second KB document**
   - describe the final architecture
   - document exact new RPC methods
   - document any remaining limitations, especially around code indexing job visibility

## Final conclusion

The current codebase already enforces a single-writer model for `db.Querier`-based writes, but remembrances mutating operations are still outside that contract. A proper fix requires extending the proxy/RPC layer with explicit remembrances write operations and making KB, events, and code indexing stores proxy-aware for writes while preserving local reads.
