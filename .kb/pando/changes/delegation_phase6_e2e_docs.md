---
created_at: 2026-06-21T19:27:20.309744685Z
updated_at: 2026-06-21T19:27:54.579796785Z
tags:
    - change
    - mesnada
    - delegation
    - phase6
    - tests
    - e2e
    - docs
---
# Change: Delegation Phase 6 — End-to-end tests + docs (feature COMPLETE)

Implemented 2026-06-21. Status: DONE, verified. Final phase of plan
`pando/plans/delegated_conclusion_resurrection_plan.md` (Phases 0-6). Marks the
delegated-conclusions + agent-loop-resurrection feature COMPLETE (optional Phase 7
warm-instance reuse deferred). Default-OFF unchanged.

## What changed & why
Phases 0-5 built and wired the feature; Phase 6 adds end-to-end test coverage of
the full pipeline (capture → enrich → broadcast → supervisor re-entry) and the
mandatory user-facing + KB documentation. Earlier phases had focused unit tests but
nothing exercised the REAL multi-component pipeline together. Phase 6 closes that
with two e2e files using production code (real FileStore, real
conclusion.Enrich/FormatForParent, real supervisor logic) — no subprocess; the only
mocked seam is the agent the supervisor calls.

## Files added
- internal/mesnada/orchestrator/delegation_e2e_test.go — genuine
  onTaskComplete → captureConclusion → Enrich → broadcastCompletion on a real temp
  FileStore: block parse+enrich+metadata (resolver project id/name, CapturedAt,
  Synthesized=false) persisted+re-fetchable+delivered on SubscribeCompletions
  (capture-before-broadcast); synthesize fallback; failed-task synthesis;
  no-block+fallback-off → no conclusion but still broadcast; disabled → nothing.
- internal/app/delegation_e2e_test.go — cross-package e2e: task built via
  production conclusion.Enrich, fed to the real supervisor: Case A injected content
  == FormatForParent; Case B resurrected once with framing+summary; depth+budget
  caps respected.

## Files touched
- internal/app/delegation_supervisor_test.go — fakeInjector records injectArgs
  (content passed to InjectConclusion) for exact-payload assertions.
- README.md — new Features bullet for Subagent Delegation (conclusions +
  resurrection, mesnada_await, caps, default-off, TUI/WebUI toggle location).
- KB feature doc pando/features/delegated_conclusions_resurrection.md (new).

## Verification
- go build ./... → clean.
- go test ./internal/mesnada/orchestrator -run TestDelegationE2E → 5/5 pass.
- go test ./internal/app -run TestDelegationE2E → pass (Case A/B + caps).
- go test ./internal/app ./internal/mesnada/... ./internal/llm/agent ./internal/llm/tools → all pass.
- gofmt -l on new/changed test files → clean.

## Notes
- A fully-real app-level wiring test (orchestrator emitting through its own
  completion stream into a Started supervisor) was intentionally NOT added:
  onTaskComplete/broadcastCompletion are unexported, so driving them from package
  app would require adding production API purely for tests. The dispatch goroutine
  is trivial channel-forward glue; the chosen split covers the contract without that
  smell.
- Feature COMPLETE. Only optional Phase 7 (reuse a running per-project child ACP
  instance as a warm delegation target) remains as a future opt-in.
