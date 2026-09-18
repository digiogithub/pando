---
created_at: 2026-09-18T08:36:59.935790826Z
updated_at: 2026-09-18T08:36:59.935790826Z
tags:
    - backlog
    - sandbox
    - memory
    - grok-build
---
# Backlog: Grok Build-inspired sandbox and memory epics (2026-09-18)

## What changed
Backlog-only change (no source code). Two epics filed in the PANDO gintrack backlog (`.kb/.pmngr/`) via `gintrack item new -w pando`, after comparing Pando with Grok Build (`/www/MCP/Pando/grok-build`, code index `grok-build`), motivated by the user's external DeepSeek analysis of Grok Build features (share link was not fetchable: 403).

- **PANDO-EP-0009 — Host command sandbox, on by default and container-free** (priority high, 52 pts). Stories PANDO-US-0040..0049: core policy/config (0040), Linux Landlock+seccomp helper re-exec with bwrap (0041), macOS Seatbelt via sandbox-exec (0042), Windows honest degradation + Job Object (0043), bash/persistent shell integration (0044), denial detection + escalation approval (0045), settings toggle TUI/WebUI/API (0046), other spawn sites (0047), observability + `pando sandbox status` (0048), tests/CI/docs (0049). Key design choice: per-spawn wrapping (Grok confines its whole process once, off by default, no escalation).
- **PANDO-EP-0008 — Analysis: Grok Build memory system** (analysis only, 29 pts). Stories PANDO-US-0033..0039: architecture reference, auto-capture, Dream consolidation, injection/prompt-cache, scoping/forgetting, rollout/UX, synthesis. Surfaced suspected defects to verify and file separately: dead `MemoryAutoCapture` config, `BuildMemoryBlock(ctx, "")` never searches, possible prompt-cache invalidation, `forget` hard delete.

## Files
- `.kb/.pmngr/epics/PANDO-EP-0008-*.md`, `PANDO-EP-0009-*.md`, `.kb/.pmngr/stories/PANDO-US-0033..0049-*.md`
- Research reports stored: [[pando/analysis/grok-build-sandbox-research.md]], [[pando/analysis/grok-build-memory-survey.md]]

## How produced / verified
Two parallel read-only research subagents (sandbox, memory) using pando code tools on projects `pando` and `grok-build`; items created through the gintrack CLI (IDs allocated by the tool, dependency references checked against allocated IDs); KB docs verified indexed via `kb_search_documents`.
