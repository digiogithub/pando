# Postmortem: KB Markdown Indexing Stuck Due to Non-Advancing Chunking Loop

**Project/User:** pando  
**Date:** 2026-04-02  
**Status:** Resolved

## Summary
A high-impact indexing issue caused KB markdown imports to stall after the first `indexing document` log line while CPU and RAM usage continued to grow. The issue is resolved.

## Symptom
- KB markdown indexing appeared stuck after the first `indexing document` log.
- Process showed sustained/high CPU and memory growth.
- Operational effect: stalled imports and resource pressure.

## Root Cause
Potential non-advancing loop in text chunking:
- In sentence-boundary split handling, some iterations could produce a boundary where `splitIdx <= overlap`.
- This could prevent effective forward movement of the chunk start offset.
- Result: repeated/degenerate iteration behavior with runaway resource use.

## Fix Implemented
Forward-progress guarantees were added to chunking iteration:
1. Enforce forward progress even when boundary calculations are invalid.
2. Apply fallback split logic when sentence-boundary split is not usable.
3. Force `start` advancement if computed next window does not move forward.
4. Add a defensive maximum-chunks guard to prevent pathological unbounded loops.

## Validation
- Indexing is now unblocked and progresses normally.
- Regression test added to cover forward-progress / non-stuck behavior.

## Files Involved
- `internal/rag/embeddings/chunking.go`
- `internal/rag/embeddings/embeddings_test.go`

## Why This Matters
This is a high-impact operational stability fix that prevents:
- runaway CPU/RAM consumption,
- stalled KB imports,
- and degraded reliability during document ingestion.

It hardens chunking logic against edge-case boundaries and improves resilience of the indexing pipeline.