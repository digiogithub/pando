---
id: PANDO-T-0014
type: task
title: Warn when a watcher registers more than 10,000 paths
status: in_review
priority: low
parent: PANDO-US-0125
author: mcp
labels: [watcher, observability]
created: 2026-10-09T12:25:37Z
updated: 2026-10-09T12:36:04Z
started: 2026-10-09T12:36:04Z
---

## Description
Count watched dirs (+ files on darwin/kqueue estimate) per watcher; log a single Warn above 10,000 suggesting watchExclude / .gitignore.
## Acceptance Criteria
Warning emitted once per watcher; test.
