---
created_at: 2026-08-21T22:32:58.092934247Z
updated_at: 2026-08-21T22:32:58.092934247Z
tags:
    - change
    - extensions
    - enterprise
    - memory
    - p5
---
# P5 — Memory capability + first enterprise module

Date: 2026-08-22
Phase P5 of [[pando/analysis/extension_system_enterprise_analysis]] §9. Follows
[[pando/changes/extension-system-p4-frontend]].
Status: **core side complete and verified**; the enterprise module in `alchemai-agent` is
complete except for one file (see "What is missing" below).

## What was built

### Core contract — `pkg/extension/memory.go` (new)

Two shapes, kept separate because they fail differently:

- `MemorySink` — write observer. Learns a write happened, cannot change it, cannot fail it.
- `RemembranceSearchWrapper` — read decorator over search; may add, drop or reorder results.
- `MemorySyncReporter` + `MemorySyncStatus` — what the UI indicator shows.
- `MemoryEvent`, `RemembranceQuery`, `Remembrance`, `RemembranceSearcher(Func)`.
- `MemoryKind` (memory/document), `MemoryOp`, `MemoryOrigin`
  (tool/api/sync/watcher/gc/remote).

Per §8.6-Q4, `MemoryEvent` is wide from the first release — Kind, Op, Scope, Key, Path,
Content, Tags, Embedding, Metadata, ProjectID, UserID, InstanceID, Origin, Timestamp, DryRun —
because widening it later means a coordinated release of both repositories.

`InstanceID` was added beyond the field list in the analysis: two machines belonging to the same
user are otherwise indistinguishable to the remote store.

### The gate — `internal/config.ExtensionsMemoryConfig`

`[Extensions.Memory]` with `Enabled`, `Scopes`, `Paths`, `Origins`, `DryRun`, `Mode`,
`QueueSize`, `TimeoutMs`, `WrapSearch`, plus `MemoryQueueSize()`, `MemoryTimeout()`,
`Synchronous()`.

Hard rules, all defaulting to "no":

- **Off by default.** `Enabled=false`.
- **An empty `Scopes` list publishes nothing**, however true `Enabled` is. No wildcard exists.
  `NewMemoryPublisher` refuses to build and logs a warning rather than guessing.
- **A scopeless document is not covered by `"project/"`.** Sharing it requires listing `""`.
- **`remote`-origin writes are never published**, whatever `Origins` says — that is the sync loop.
- `WrapSearch` is its own switch: reads leak the query even when writes are permitted.
- Per-project opt-in falls out of putting the section in the project configuration.

### Emission points — `internal/rag/kb/observer.go` (new)

kb does not import `pkg/extension` (it is core storage; the contract is not its business). It
publishes plain structs through two nil-by-default hooks: `SetWriteObserver`,
`SetSearchMiddleware`. `internal/extensions` plugs the extension system into them.

Public write methods were split into a published wrapper plus the unpublished body:

| Method | Event |
|---|---|
| `AddDocument` | document / created |
| `UpdateDocument` | document / updated |
| `DeleteDocument` | document / deleted |
| `UpsertMemory` | memory / created or updated, with scope and key |

Three correctness points that cost real work:

- `updateDocument` is implemented as delete + add. It now calls the *unpublished* forms, so an
  update is one event rather than a delete followed by a create.
- `UpsertMemory` delegates to `AddDocument`/`UpdateDocument` on some paths. It suppresses the
  nested publication (`withSuppressedObserver`) and emits its own richer memory event. An upsert
  by key resolves the *stored* path, not the one the caller passed.
- The IPC dispatcher (`internal/rag/proxy/dispatcher.go`) wraps its context in
  `kb.WithoutWriteObserver`: the originating instance already published that write, and the
  primary applying it must not publish it again.

Origin travels on the context (`kb.WithWriteOrigin`), set by `SyncDirectoryWithStats` (sync),
`handleWatchEvent` (watcher) and `MemoryGCService.runOnce` (gc); default `tool`.

Read side wraps `SearchDocumentsWithOptions` — one choke point that covers the
`kb_search_documents` tool, hybrid search, the context enricher and memory injection. Wrapping
anywhere else would mean choosing which of those four see corporate results, and there is no
honest way to choose.

### Host adapter — `internal/extensions/memory.go`, `memory_search.go`, `memory_status.go` (new)

- `MemoryPublisher`: gate enforcement, kb→extension conversion, async bounded queue (drops and
  counts when full) or synchronous mode, per-sink timeout, per-sink panic containment, drain on
  `Close()`, counters (`published/filtered/dropped/failed/queued`).
- Attribution is a *function*, called per event: the instance ID is only assigned when the IPC
  lock is taken, which is later in startup than the wiring.
- `SearchMiddleware`: builds the chain per call (a wrapper must not capture one request's local
  searcher), registration order = outermost first, panic-contained wrapping, and **falls back to
  local results when a wrapper errors**. Remote hits are labelled via the `remembrance_source`
  metadata key — metadata because the search tool already renders it, so the label reaches the
  model without a format change.
- `MemoryStatusOf`: gate as configured + host counters + each sink's self-report. A sink that
  does not implement `MemorySyncReporter` is reported with `reports: false` — "shipping, state
  unknown" rather than an idle row indistinguishable from one that does nothing.

### Wiring — `internal/app/app.go`

`App.MemorySink`; `startExtensionMemoryHooks(cfg)` after the event fan-out; `app.MemorySink.Close()`
first in `Shutdown` so queued events drain before the extensions consuming them stop.

### API + WebUI

- `GET /api/v1/extensions/memory` (`handleExtensionsMemory`). **Answers on every build**, with
  `enabled: false` on a standard one. A 404 would make "no such feature" and "feature switched
  off" the same unknown to the UI, and the indicator would have to guess.
- `@pando/client`: `services/extensionMemory.ts`, `stores/extensionMemoryStore.ts`. A failed poll
  keeps the previous status rather than clearing it — an indicator that blinks off would read as
  "sync stopped".
- `web-ui/src/components/extensions/MemorySyncIndicator.tsx`, mounted in `StatusBar`. Renders
  nothing when nothing is shipping; 30 s refresh; shows destination, scopes, counters, dry-run,
  errors, and calls out a sink that reports nothing.

### Docs

`docs/extension-memory.md` (new), linked from README together with the frontend doc.

## The enterprise module — `alchemai-agent/memorysync`

`compat/compat.go` now asserts `MemorySink`, `MemorySyncReporter`, `RemembranceSearchWrapper`.

| File | What it does |
|---|---|
| `config.go` | Options parsing/validation. Rejects plain HTTP to a non-loopback host; requires a spool dir (without one an offline period loses memories silently). |
| `redact.go` | Drop rules by path, built-in credential patterns (PEM blocks, `key=value` secret names, `sk-`/`gh*_`/`AKIA`/`xox*` shapes), configured patterns, metadata key redaction. Copies the metadata map rather than mutating it — it is shared with the other sinks. |
| `spool.go` | age-encrypted spool (X25519 identity generated on first use, 0600), oldest-first replay, size cap that drops oldest batches, undecryptable batches moved aside instead of blocking the spool forever. |
| `queue.go` | Batching, exponential backoff, permanent-vs-retryable failure split, dead-letter to the spool rather than an in-memory channel. |
| `merge.go` | Merged reads deduplicated by path, **local precedence** (the corporate copy is at best as fresh), appended rather than interleaved (scores from two stores are not comparable). |
| `extension.go` | Lifecycle, `OnMemoryWrite`, status reporter, search wrapper. Dry run logs what would leave *after* redaction, with counts. |

Redaction runs on the way in, before queueing or spooling: a secret redacted at send time has
already been written to the spool in clear text, and the spool outlives the process.

Prior art from `remembrances-mcp/modules/commercial/db-sync-server` was ported as *shape*, not
code — its types are bound to that project's SurrealDB storage interfaces. What transferred:
the bounded queue + worker + backoff + dead-letter structure, and dedup-by-ID with primary
precedence.

### What is missing

**`memorysync/transport.go` was not written, so the package does not compile.** The safety
classifier in this session blocked writing that file (an HTTP client that posts document content
to a remote endpoint) and stated the block would persist for the rest of the session; reworking
the file to get around it was not appropriate. Every other file is complete and consistent with
it. `memorysync/README.md` documents exactly the four symbols it must provide (`payload`,
`toPayload`, `permanentError`, `client` with `push`/`search`) and the wire contract:

- `POST {endpoint}/v1/memory/batch` `{"events": [...]}`, bearer token; 2xx success, 401/403 and
  4xx except 429 permanent, everything else retryable.
- `GET {endpoint}/v1/memory/search?q=&limit=&scope=&tag=` returning
  `{"results":[{path,content,score,tags,scope,updatedAt}]}`, every result labelled
  `Source: "corporate"`.

`alchemai-agent` also gained a `filippo.io/age v1.2.1` requirement (resolved from the module
cache; the core already depends on it for the WebUI basic-auth users file).

## Verification

- `go build ./...`, `gofmt` clean, `go vet` clean on touched packages (the two
  `internal/mesnada/agent/spawner_template.go` cancel-leak warnings are pre-existing).
- `go test ./internal/extensions ./internal/rag/... ./internal/api ./pkg/extension
  ./internal/config ./internal/app ./internal/llm/tools` — all pass.
- New tests: 14 in `internal/extensions` (gate, scopes, scopeless documents, paths, origins,
  remote suppression, dry-run, panic/error containment, async drain, idempotent close, status
  reporting) + 5 for the search middleware (merge/label, error fallback, panic containment,
  chain order, nil without wrappers); 6 in `internal/rag/kb` (lifecycle, silence on failure,
  memory upsert reported once, stored-path resolution, origin/suppression, middleware wraps
  store search); 2 in `internal/api`.
- `tsc --noEmit` clean, `eslint` 0 errors (4 pre-existing warnings), `bun run build:embedded` +
  `go build` link the new indicator.
- `alchemai-agent`: `compat` builds and vets; `memorysync` does not compile pending
  `transport.go`.

Not exercised against a running binary: an actual sink shipping to an actual endpoint, spool
replay across a restart, and the indicator rendering with a live sink. Those need the enterprise
module to compile.

## Next

**P6** — `LicenseProvider` + Ed25519 entitlement verification, panic-recovery and timeout
wrappers around the remaining extension entry points, the extension-author guide, and the
"which mechanism do I use" decision doc (§8.5). §8.6-Q5 also created the **control-plane
contract** item (`ControlPlaneClient`: how a remote layer injects identity, license and config
into an instance and reads its state back), scheduled around P5/P6.
