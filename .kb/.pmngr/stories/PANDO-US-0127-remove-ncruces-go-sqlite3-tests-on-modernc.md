---
id: PANDO-US-0127
type: story
title: Remove ncruces/go-sqlite3; tests on modernc
status: done
priority: high
parent: PANDO-EP-0022
labels: [db]
created: 2026-10-10T20:30:27Z
updated: 2026-10-10T21:49:21Z
closed: 2026-10-10T21:49:21Z
---

## Description
Drop ncruces driver/embed, port tests to the engine/modernc driver, check DATETIME scanning compatibility.

## Acceptance Criteria
- No ncruces import; go.mod clean; go test ./... green.
