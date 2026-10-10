---
id: PANDO-US-0126
type: story
title: "Engine layer: pando-sqlite driver on modernc + multiwriter VFS"
status: done
priority: high
parent: PANDO-EP-0022
labels: [db]
created: 2026-10-10T20:30:26Z
updated: 2026-10-10T21:49:20Z
closed: 2026-10-10T21:49:20Z
---

## Description
New `internal/db` engine: driver `pando-sqlite` wrapping modernc.org/sqlite; multiwriter multi-process mode on unix, stock WAL + busy_timeout + _txlock=immediate on Windows; per-connection pragmas via DSN; transparent retry of autocommit writes on SQLITE_BUSY_SNAPSHOT (write queries buffered); `db.RunTx`; lifetime shared lock + exclusive maintenance lock; migration lock around goose; one-time auto_vacuum=NONE conversion; `PANDO_DB_ENGINE=sqlite` emergency switch with mixed-mode guard.

## Acceptance Criteria
- Unit tests for retry driver, write classification, RunTx, locks, conversion.
