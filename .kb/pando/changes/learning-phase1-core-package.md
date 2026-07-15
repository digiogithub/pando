---
created_at: 2026-07-15T14:02:09.078669547Z
updated_at: 2026-07-15T14:02:09.078669547Z
tags:
    - change
    - learning
    - phase1
    - core
---
# Learning mode — Phase 1 (core package) COMPLETE

Implements Phase 1 of [[learning-opt-in-mode-implementation]]. Adds the
dependency-free core of the opt-in `/learning` "learner and documentarian" session
mode, modeled verbatim on [[superpowers-opt-in-mode-implementation]]'s
`internal/superpowers`.

## What changed
- **New `internal/learning/learning.go`** (imports only `strings` + `sync`, keeping it
  free of the `llm/tools → mesnada/acp → learning` import cycle that later phases would
  otherwise create):
  - `State{Enabled bool}` + `var sessions sync.Map`; presence = active, no configured default.
  - `SetEnabled(sessionID, enabled)`, `Enabled(sessionID)`, `normalizeSessionID` (trims).
  - `Instructions()` — the per-turn harness: recover context first (kb_search_documents /
    hybrid_search_remembrances / recall + code-index tools), ASK via the `AskUserQuestion`
    tool instead of guessing, capture non-trivial discoveries with `kb_add_document` +
    `remember`, keep docs honest (update stale, `kb_mark_outdated` the superseded), and an
    explicit clause that output-brevity policies (Caveman/Ponytail) never limit KB doc depth.
  - `FinishPrompt()` — consolidation turn for `/learning-finish`: review learnings, persist to
    KB/memory, reconcile/mark stale docs, summarize; no git side effects.
  - `ActivationMessage(focus)`, `const AlreadyActiveMessage`, `const NotActiveMessage`.
- **New `internal/learning/learning_test.go`**: enable/disable/idempotency, session-ID
  normalization, empty-ID ignored, session isolation, concurrency (race), and content assertions
  that the instructions + finish prompt name the required tools (`kb_search_documents`,
  `AskUserQuestion`, `kb_add_document`, `remember`, `kb_mark_outdated`) and that ActivationMessage
  echoes a trimmed focus.

## Why
First increment of the `/learning` + `/learning-finish` mode. The core is intentionally
import-free so Phases 2-4 (agent bridge, prompt composition, slash commands on all surfaces) can
wire it without an import cycle. `kb_mark_outdated` is referenced by name here but the tool itself
is added in Phase 5 (no MCP tool exposes `KBStore.MarkDocumentOutdated` today).

## Verification
- `go build ./internal/learning/` — clean.
- `go test ./internal/learning/` — ok.
- `go vet ./internal/learning/` — clean.
- `go test -race ./internal/learning/` — ok.

## Next
Phase 2: `internal/llm/agent/learning_session.go` (SetLearningMode / LearningMode /
learningEnabledForContext / RunLearningFinish) + wire into `sessionPolicyActive` and
`sessionPolicyInstructions` in `agent.go`, order superpowers → learning → caveman → ponytail.