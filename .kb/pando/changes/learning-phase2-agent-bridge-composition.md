---
created_at: 2026-07-15T14:18:03.950485544Z
updated_at: 2026-07-15T14:18:03.950485544Z
tags:
    - change
    - learning
    - phase2
    - agent
    - prompt-injection
---
# Learning mode — Phase 2 (agent bridge + prompt composition) COMPLETE

Implements Phase 2 of [[learning-opt-in-mode-implementation]]. Continues
[[learning-phase1-core-package]]. Wires the dependency-free `internal/learning` core into the
agent's per-turn prompt injection, mirroring the Superpowers bridge
([[superpowers-opt-in-mode-implementation]]).

## What changed
- **New `internal/llm/agent/learning_session.go`**:
  - `SetLearningMode(sessionID, enabled)` / `LearningMode(sessionID)` delegate to
    `learning.SetEnabled` / `learning.Enabled`.
  - `learningEnabledForContext(ctx)` resolves the session id via the agent's existing
    `sessionIDFromContext` (context keys live in llm/prompt + llm/tools, so resolution stays out
    of the import-free core to avoid the `tools -> mesnada/acp -> learning` cycle).
  - `RunLearningFinish(ctx, svc, sessionID)` — clone of `RunSuperpowersFinish`: runs the
    consolidation turn as a normal `svc.Run(learning.FinishPrompt())`, clears the mode ONLY on a
    successful terminal `AgentEventTypeResponse{Done, Error==nil}`; cancel/error/`Run` error retain
    it. `ErrLearningNotActive` returned when inactive.
- **`internal/llm/agent/agent.go`**:
  - Added `internal/learning` import (alphabetical, after `config`).
  - `sessionPolicyActive(ctx)` now ORs `learningEnabledForContext(ctx)` so the prepareProvider
    fast path is gated when learning is on.
  - `sessionPolicyInstructions(ctx)` injects
    `prompt.InjectSkillInstructions("learning", learning.Instructions())` immediately AFTER the
    superpowers block. Final composition order: **superpowers -> learning -> caveman -> ponytail**
    (superpowers+learning govern how work is approached; caveman/ponytail govern the write-up/amount).
    Header comment updated to match.
- **New `internal/llm/agent/learning_session_test.go`** (reuses package helpers `superpowersCtx`,
  `fakeFinishService`, `drain`, `ErrRequestCancelled`, `ErrSessionBusy`): set/get, ctx resolution,
  `sessionPolicyActive` gating, single-ruleset injection, three-way order
  superpowers->learning->ponytail, and `RunLearningFinish` inactive->error, success-clears,
  cancel-retains, run-error-propagates-and-retains.

## Verification
- `go build ./internal/llm/agent/` — clean.
- `go vet ./internal/llm/agent/` — clean.
- `go test ./internal/llm/agent/ -run 'Learning|SessionPolicy'` — ok.
- `go test -race ./internal/llm/agent/ -run 'Learning|SessionPolicy'` — ok. (Pre-existing HEAD
  races in goal_runner_test / persona_selector avoided by the targeted run — see [[pando_repo_pitfalls]].)

## Next
Phase 3: register `learning` / `learning-finish` in `internal/commands/registry.go` (+ test counts).
Then Phase 4: slash commands on ACP (specs/parser/handler/AgentService + both adapters), Web UI SSE,
and TUI. Phase 5: new `kb_mark_outdated` MCP tool.