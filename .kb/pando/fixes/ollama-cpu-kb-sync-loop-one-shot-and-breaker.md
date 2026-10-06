---
created_at: 2026-10-07T13:44:07.381633601Z
updated_at: 2026-10-07T13:44:07.381633601Z
tags:
    - fix
    - ollama
    - embeddings
    - kb
    - mesnada
    - performance
---
# Fix: Ollama pinned at 100% CPU by KB sync loop (one-shot skip, sync breaker, no fallback fan-out)

Diagnosis: [[pando/analysis/ollama-cpu-kb-sync-loop-darwin-2026-10-07.md]]. macOS v1.2.9 telemetry showed every mesnada `pando-cli` subagent re-running the full KB sync at startup; each doc timed out (45s `kbEmbeddingsTimeout`), nothing was stored, so every next start repeated it, while the Ollama embedder multiplied requests on failure.

## Changes
1. **One-shot runs skip startup indexing** — new `AppOptions.SkipStartupIndexing` (internal/app/app.go), set in `cmd/root.go` when `-p/--prompt` or `--goal` is given (covers mesnada pando-cli subagents).
   - `initRemembrancesProjectIndexing(..., skipStartupIndexing)` (internal/app/remembrances_code.go): still defaults `ContextEnrichmentCodeProject`, then returns before the startup scan/fs watcher.
   - `initRemembrancesKBSync(..., skipStartupIndexing)` (internal/app/remembrances.go): keeps `ConfigureFilesystemMirror` + converter (child KB writes still land on disk), skips auto-import and KB watcher.
2. **KB sync circuit breaker** (internal/rag/kb/sync.go): `kbSyncBackendFailureLimit = 2` consecutive add/update failures classified as backend-unavailable abort the sync (cancel walker/loaders, drain results) and return wrapped `ErrSyncEmbeddingBackendUnavailable`. Success or non-backend error resets the counter.
3. **No Ollama request fan-out on timeouts** (internal/rag/embeddings):
   - `embedder.go`: `ErrBackendUnavailable`, `IsBackendUnavailable(err)` (DeadlineExceeded, net timeout, ECONNREFUSED/ECONNRESET, 503). `context.Canceled` is not counted.
   - `ollama.go`: `ollamaHTTPError` keeps status (503 unwraps to `ErrBackendUnavailable`). Batch failure falls back to per-text only when not `shouldStop` (ctx done / backend unavailable); per-text loop stops at first backend failure; legacy `/api/embeddings` used only when `/api/embed` answers 404/405/501.

## Verification
- `go build ./...`, `go vet` on touched packages.
- New tests: `TestSyncStopsWhenEmbeddingBackendUnavailable` (internal/rag/kb/sync_backend_breaker_test.go: 10 docs, exactly 2 embed calls), `TestOllamaEmbedDocuments_TimeoutDoesNotFanOut`, `_ServerBusyStops`, `_LegacyFallbackOnMissingEndpoint` (internal/rag/embeddings/embeddings_test.go).
- `go test ./internal/rag/embeddings ./internal/rag/kb ./internal/app` pass, `-race` on the new tests passes. `./internal/rag` `TestBuildMemoryBlockWithResultAgainstRealStore` failed once in a full run and passed 3/3 isolated: unrelated flake.

## Not done (follow-ups from the analysis)
- Global `ContextEnrichmentCodeProject` applied to a different working dir.
- dbproxy primary lacks `ReplaceSessionEvents` (session indexing from secondaries fails).
- Persisted per-doc failure memo (the breaker already caps a failing start to 2 requests).
