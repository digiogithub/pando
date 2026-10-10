---
id: PANDO-US-0130
type: story
title: pando db compact under the engine
status: done
priority: high
parent: PANDO-EP-0022
labels: [db]
created: 2026-10-10T20:30:30Z
updated: 2026-10-10T21:49:24Z
closed: 2026-10-10T21:49:24Z
---

## Description
Compaction needs the exclusive maintenance lock (no other process open); converts auto_vacuum to NONE (engine refuses incremental). Clear message when instances are running.

## Acceptance Criteria
- Test: compact refused while another process holds the DB, succeeds otherwise.
