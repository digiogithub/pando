---
id: PANDO-US-0074
type: story
title: "Learned skills as reviewable proposals: file-backed, approved-only injection, frozen per session, success_rate fed by rewards"
status: done
priority: medium
parent: PANDO-EP-0014
author: mcp
labels: [evaluator, skills, prompt-cache]
estimate: 8
created: 2026-09-29T20:33:19Z
updated: 2026-09-29T21:39:22Z
started: 2026-09-29T21:29:01Z
closed: 2026-09-29T21:39:22Z
---

## Description

Skills extracted by the judge go straight into `skill_library` and are injected into every prompt (`builder.go:188-211`) with no human review. Their ranking cannot work: no query updates `success_rate`, so `DeactivateLowestSkill` evicts arbitrarily; titles are `"<task_type> skill"`; `GetActiveSkills` increments `usage_count` on every prompt build (every turn); and a skill saved mid-session changes the system prompt, invalidating the provider prompt cache (same class of problem fixed for memories in PANDO-US-0036).

Make skills a reviewable, versioned artefact:

- Judge proposals are written to `.pando/skills/learned/<slug>.md` with front matter: status (`pending` | `approved` | `rejected`), task type, confidence, source session, judge model, created date, and a generated title. The DB keeps only status/stats mirrors.
- Only `approved` skills are injected. Optional `evaluator.skills.autoApproveAfter = N` approves a pending skill once N later sessions of its task type scored above threshold while it was *shadow*-attached (off by default).
- The injected "Learned Optimization Rules" block is built once per session and frozen (reuse the pattern of `sessionMemoryBlock`, invalidated after compaction); `usage_count` increments once per session.
- `success_rate` becomes the mean reward of sessions where the skill was injected (join through a `session_skill_injections` table), updated during evaluation; eviction and ordering use it.
- Review surfaces: `pando skills list|approve|reject <id>`, TUI evaluator page actions, WebUI skills tab with approve/reject, and the existing `pando_evaluator_skills` tool gains a `status` filter.
- Dedupe stays (word overlap) but also compares against approved and rejected files, so a rejected rule is not proposed again.

## Acceptance Criteria

- [ ] A judge output with confidence ≥ 0.7 creates a pending file and no prompt change; approving it injects it on the next new session only.
- [ ] Prompt bytes stay identical across turns after a skill is approved mid-session.
- [ ] After three evaluated sessions with the skill injected, `success_rate` equals their mean reward.
- [ ] A rule textually similar to a rejected file is not re-proposed.
- [ ] `go test -race ./internal/evaluator ./internal/llm/prompt ./internal/llm/agent` pass; KB change document written.

## Notes

Consistent with PANDO-EP-0008 decisions: the repo is the source of truth, humans review, the KB/git history is the audit trail.
