---
id: PANDO-US-0131
type: story
title: Multi-process corruption and concurrency test suite
status: done
priority: high
parent: PANDO-EP-0022
labels: [db]
created: 2026-10-10T20:30:31Z
updated: 2026-10-10T21:49:25Z
closed: 2026-10-10T21:49:25Z
---

## Description
Re-exec test binary as 3-5 writer processes on the real Pando schema (sessions, messages, KB+FTS, events, code index), kill-mid-write recovery, concurrent first-start migrations, auto_vacuum conversion, mixed-mode refusal. Checks: integrity_check, FTS integrity-check, row-count invariants.

## Acceptance Criteria
- Suite green under go test and -race on the engine package.
