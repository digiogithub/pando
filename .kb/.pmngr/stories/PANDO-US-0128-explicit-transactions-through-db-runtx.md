---
id: PANDO-US-0128
type: story
title: Explicit transactions through db.RunTx
status: done
priority: high
parent: PANDO-EP-0022
labels: [db]
created: 2026-10-10T20:30:28Z
updated: 2026-10-10T21:49:22Z
closed: 2026-10-10T21:49:22Z
---

## Description
Convert the 19 BeginTx/Begin sites (rag chunk, kb, events, code indexer, backfill, design store, history) to whole-transaction retry.

## Acceptance Criteria
- No direct BeginTx outside internal/db; closures free of side effects outside the tx.
