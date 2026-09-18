---
id: PANDO-T-0007
type: task
title: Fix the latent channel race in the savings ledger recorder
status: backlog
priority: low
labels: [bug]
created: 2026-09-18T11:01:15Z
updated: 2026-09-18T11:01:15Z
---

## Description

`internal/savings/ledger.go` `recorder.run()` ranges over the shared field `r.ch` while `Close()` can nil it concurrently, the same pattern that deadlocked `internal/sandbox/events.go` during PANDO-US-0048 (fixed there by passing the channel as a `run(ch)` parameter). Not exercised today because no test calls `Close()` right after `Record()`.

## Acceptance Criteria

- [ ] `recorder.run` receives its channel as a parameter captured at launch.
- [ ] A test that records and immediately closes passes under `-race` without hanging.
