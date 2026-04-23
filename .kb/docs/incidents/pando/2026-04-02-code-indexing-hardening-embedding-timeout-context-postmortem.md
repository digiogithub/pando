# Postmortem: Code Indexing Hardening After KB Chunking Incident

**Project/User:** pando  
**Date:** 2026-04-02  
**Status:** Preventive hardening applied

## Summary
After resolving the primary KB markdown indexing stall (non-advancing chunking loop), we audited the code indexing pipeline to verify whether the same failure mode could occur there.

Result: code indexing does **not** use the problematic text chunking path, but we identified a related operational risk in the symbol-embedding phase and applied a preventive safeguard.

## Symptom / Concern
- User requested validation that the same incident could affect code indexing.
- Risk scenario: embedding requests in code indexing could remain blocked too long without a per-batch timeout, causing indexing jobs to appear stuck.

## Root-Cause Analysis
- Code indexing path is implemented in `internal/rag/code/indexer.go`.
- It indexes files, extracts symbols via tree-sitter, and embeds symbol text in batches.
- Unlike KB document indexing, this path does not call `ChunkText` and therefore does not share the exact non-advancing overlap/split loop bug.
- However, embedding batches previously had no explicit timeout boundary in `embedSymbols`, creating a resilience gap under provider/network hangs.

## Fix Applied
In `internal/rag/code/indexer.go`:
- Added `codeEmbeddingsTimeout = 90 * time.Second`.
- Hardened `embedSymbols(...)` by:
  - Ensuring `ctx` is never nil (`context.Background()` fallback).
  - Wrapping each embedding batch call with `context.WithTimeout`.

In tests:
- Added `internal/rag/code/indexer_test.go`.
- Added regression-style test `TestEmbedSymbols_NilContextUsesBackground` to ensure nil context does not propagate into embedder calls and embedding write path remains functional.

## Validation
- Ran focused tests after patch:
  - `go test ./internal/rag/code ./internal/rag/embeddings`
- Result: both packages pass.

## Files Involved
- `internal/rag/code/indexer.go`
- `internal/rag/code/indexer_test.go`

## Why It Matters
This is a high-value defensive stability improvement:
- Reduces risk of apparent stalls in code indexing.
- Adds bounded execution behavior for embedding batches.
- Complements the primary KB incident fix by hardening adjacent indexing flows.
