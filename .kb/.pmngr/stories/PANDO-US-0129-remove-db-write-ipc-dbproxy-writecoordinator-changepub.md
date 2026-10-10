---
id: PANDO-US-0129
type: story
title: "Remove DB-write IPC: dbproxy, writecoordinator, changepub, remembrances proxy"
status: done
priority: high
parent: PANDO-EP-0022
labels: [db]
created: 2026-10-10T20:30:29Z
updated: 2026-10-10T21:49:23Z
closed: 2026-10-10T21:49:23Z
---

## Description
Bootstrap opens the DB with db.Connect in every role; role becomes leader election for singleton jobs only. Delete dbproxy, writecoordinator, changepub, rag/proxy dispatcher, SetWriteProxy paths, db.compact forwarding, ConnectRWSecondary/ConnectReadOnly/ConnectForRole, killStalePrimary; simplify PromoteToPrimary (no DB swap).

## Acceptance Criteria
- No db.write RPC; secondaries run full DB features; leader still owns code-index watcher and evaluator background.
