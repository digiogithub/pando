---
id: PANDO-T-0011
type: task
title: Run code-index filesystem watcher only on the IPC primary instance
status: in_review
priority: high
parent: PANDO-US-0125
author: mcp
labels: [watcher, ipc]
created: 2026-10-09T12:25:37Z
updated: 2026-10-09T12:29:17Z
started: 2026-10-09T12:29:17Z
---

## Description
app/remembrances_code.go starts watchIndexedProject in every instance (each mcp-server). Gate on IPC primary role; secondaries rely on primary. On promotion (failover) start the watcher.
## Acceptance Criteria
Secondary instances hold no code-index watcher; promoted secondary starts it; tests.
