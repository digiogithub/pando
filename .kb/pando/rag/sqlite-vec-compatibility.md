# sqlite-vec Compatibility Research for Pando

## Problem Statement

Pando uses `github.com/ncruces/go-sqlite3 v0.25.0` (no CGO, WASM-based SQLite).
We evaluated `github.com/asg017/sqlite-vec-go-bindings` for vector search via `vec0` virtual tables.

## Compatibility Matrix

| sqlite-vec-go-bindings | ncruces required | Pando ncruces | Compatible? |
|---|---|---|---|
| v0.1.6 (latest stable) | v0.17.1 | v0.25.0 | ❌ |
| v0.1.7-alpha.2 (latest) | v0.17.1 | v0.25.0 | ❌ |
| All versions ≤ v0.1.7-alpha.2 | v0.17.1 | v0.25.0 | ❌ |

**All available versions of sqlite-vec-go-bindings require ncruces v0.17.1.**

## Error Messages Encountered

### Error 1: Atomics/Threads
```
invalid function[11] export["sqlite3_soft_heap_limit64"]:
i32.atomic.store invalid as feature "" is disabled
```
**Cause**: sqlite-vec WASM requires the WebAssembly threads proposal (atomic instructions).
wazero is configured by ncruces with `api.CoreFeaturesV2` only.

**Fix attempted**: Set `sqlite3.RuntimeConfig` with `experimental.CoreFeaturesThreads`:
```go
import (
    sqlite3 "github.com/ncruces/go-sqlite3"
    "github.com/tetratelabs/wazero"
    "github.com/tetratelabs/wazero/api"
    "github.com/tetratelabs/wazero/experimental"
)
func init() {
    cfg := wazero.NewRuntimeConfig()
    cfg = cfg.WithMemoryLimitPages(4096) // 256MB on 64-bit
    cfg = cfg.WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesThreads)
    sqlite3.RuntimeConfig = cfg
}
```
This must be set BEFORE the first connection (ncruces compiles WASM lazily via `sync.Once`).

### Error 2: Module API mismatch
```
failed to connect to database: "go_final" is not exported in module "env"
```
**Cause**: Even with atomics enabled, the sqlite-vec WASM (compiled for ncruces v0.17.1) imports
a host function `go_final` from the `env` module that was removed/renamed in ncruces v0.25.0.

**This is a hard incompatibility** — cannot be fixed without recompiling the sqlite-vec WASM.

## How sqlite-vec-go-bindings/ncruces Works

The package is NOT a Go wrapper that calls sqlite-vec at runtime.
It provides a **custom WASM binary** (SQLite + sqlite-vec compiled together):

```go
// ncruces/init.go in the package:
//go:embed sqlite3.wasm
var wasmBinary []byte

func init() {
    sqlite3.Binary = wasmBinary  // replaces the standard ncruces WASM
}
```

This replaces `_ "github.com/ncruces/go-sqlite3/embed"` — you must NOT import both.

## When to Check Again

Monitor `github.com/asg017/sqlite-vec-go-bindings` for releases that:
- Update `require github.com/ncruces/go-sqlite3` to v0.25.0 or later
- Or explicitly state compatibility with ncruces v0.25.x

Until then, the pure Go cosine similarity approach in `internal/rag/search.go` is the solution.

## Alternative: Downgrade ncruces

Possible but NOT recommended — ncruces v0.25.0 has significant improvements over v0.17.1.
Also, downgrading might break other parts of Pando that rely on v0.25.0 behaviour.

## Alternative: modernc.org/sqlite

Another pure-Go SQLite driver that `viant/sqlite-vec` uses. Replacing the entire DB driver
would be a much larger change and is not worth it just for vector search.

## Future Migration Path (when compatible version exists)

1. Remove `_ "github.com/ncruces/go-sqlite3/embed"` from `connect.go`
2. Add `_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"` 
3. Add vec_init.go to enable threads in wazero RuntimeConfig
4. Add migration to create `rag_vec` virtual table:
   ```sql
   CREATE VIRTUAL TABLE rag_vec USING vec0(embedding FLOAT[1536]);
   ```
5. Populate `rag_vec` from existing BLOBs in `rag_chunks.embedding`:
   ```sql
   INSERT INTO rag_vec(rowid, embedding)
   SELECT id, embedding FROM rag_chunks WHERE embedding IS NOT NULL;
   ```
6. Update `SearchVector` to use SQL MATCH instead of Go cosine loop.

The BLOB format is already compatible (little-endian float32).
