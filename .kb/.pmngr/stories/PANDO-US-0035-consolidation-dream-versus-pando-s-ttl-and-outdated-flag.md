---
id: PANDO-US-0035
type: story
title: Consolidation (Dream) versus Pando's TTL and outdated-flag lifecycle
status: backlog
priority: medium
parent: PANDO-EP-0008
labels: [analysis, memory, grok-build]
estimate: 5
created: 2026-09-18T08:35:08Z
updated: 2026-09-18T08:35:08Z
---

## Description

As a Pando maintainer, I want to compare Grok Build's "Dream" consolidation with Pando's TTL + hits + `outdated` lifecycle, so that we know whether Pando should consolidate memories instead of only expiring them.

Study Dream gating (legacy 24 h + 5 sessions; v2 20 pending or 24 h), the typed topic operations (create / update / delete / rename / merge / split) with cited evidence, contradiction handling, archive and retention.

Questions to answer:

- Could the KB (`pando/...` documents plus `[[links]]`) serve as Grok's "topics" layer, with memories acting as the "inbox"?
- Which gates and budgets fit Pando?
- How should consolidations be audited and made reversible, given the versioned `.kb/` and jj?
- Should TTL still apply to curated knowledge, which Grok exempts from decay?

Files to study — Grok: `xai-grok-memory/src/{v2_consolidation,dream,dream_lock,v2_maintenance,archive}.rs`, `xai-grok-shell/src/session/acp_session_impl/v2_memory_dream.rs`. Pando: `internal/rag/kb/{memory.go,memory_gc.go,links.go,graph.go,repair.go}`.

## Acceptance Criteria

- [ ] `pando/analysis/grok-build-memory-consolidation.md` with a lifecycle comparison table and candidate consolidation designs framed as questions, not implementations.
- [ ] No Pando code is changed.
