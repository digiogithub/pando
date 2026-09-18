---
id: PANDO-US-0037
type: story
title: Memory scoping, storage location, forgetting and safety
status: backlog
priority: medium
parent: PANDO-EP-0008
labels: [analysis, memory, grok-build]
estimate: 3
created: 2026-09-18T08:35:09Z
updated: 2026-09-18T08:35:09Z
---

## Description

As a Pando maintainer, I want to compare memory scoping, storage location, forgetting and safety, so that personal and project memories are stored and deleted correctly.

Compare Grok's workspace identity (hash of git origin, stored outside the repository under `~/.grok/`) with Pando's in-repo `.kb/` mirror, and Grok's hash-preconditioned tombstone forget with Pando's hard delete. Cover access policy, prompt-injection defenses, secrets and redaction, telemetry privacy, and the enterprise memory-sink implications.

Questions to answer:

- Do `user/` memories actually cross projects in Pando, given the global `memory_key` index and per-repository database?
- Should personal memories be kept out of the team-shared, versioned `.kb/`?
- Is a tombstone or audit ledger needed so deleted memories do not resurrect through the mirror, the watcher or enterprise sinks?
- Should upserts use content-hash optimistic concurrency?

Files to study — Grok: `xai-grok-memory/src/{storage.rs:700-760,v2_access.rs,v2_maintenance.rs}`, `acp_session_impl/memory_forget.rs`. Pando: `internal/rag/kb/{filesystem,sync,watcher,selfwrite}.go`, `internal/llm/tools/remembrances_memory.go:233-285`, `internal/db/migrations/20260611000001_add_kb_memory.sql`, `internal/redact/`, the extensions memory sink.

## Acceptance Criteria

- [ ] `pando/analysis/grok-build-memory-scoping-forgetting.md`.
- [ ] No Pando code is changed.
