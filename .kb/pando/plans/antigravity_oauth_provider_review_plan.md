# Antigravity OAuth Provider Complete Review Plan

## Objective

Perform a full review of the current Antigravity OAuth provider implementation that was produced across the sequential subagent phases, compare it against the intended implementation plan, identify architectural and functional gaps, and define the concrete follow-up work required to make the provider production-ready.

This review is not limited to verifying that code exists. It must validate that the implementation actually matches the intended runtime behavior:
- Google OAuth with PKCE
- secure multi-account persistence
- token lifecycle management
- Antigravity-specific request handling
- scheduler-based account rotation
- failover semantics
- logical account-agnostic model selection
- usable API and TUI flows
- reliable automated tests

---

## Review Goals

1. Confirm what was truly implemented versus what was only approximated.
2. Distinguish between:
   - fully implemented behavior
   - partially implemented behavior
   - design substitutions that diverge from the original plan
   - missing functionality
3. Evaluate architectural quality, not just feature presence.
4. Produce a follow-up backlog ordered by impact and dependency.
5. Reduce the risk of assuming Antigravity is “done” when important pieces are still indirect or incomplete.

---

## Current Review Hypothesis

The current implementation appears to provide a broad functional base, but likely remains incomplete in several critical areas:

- provider account persistence and secret handling are largely implemented
- OAuth handlers and core token exchange flow exist
- logical Antigravity models exist
- scheduler and failover foundations exist
- token refresh logic exists
- TUI/API integration exists at a basic usability level
- tests were expanded substantially

However, the major likely gap is that the implementation seems to route Antigravity through the existing Gemini provider path instead of introducing a dedicated Antigravity transport/provider client. This may mean the implementation is operationally useful but not fully faithful to the design.

This hypothesis must be verified carefully during the review.

---

## Scope of the Review

### In scope

- provider account and config model
- encryption and secret handling
- OAuth package and endpoints
- request-time provider construction and transport routing
- Antigravity scheduler behavior
- token refresh and persistence lifecycle
- failover/error classification logic
- logical model registration and model listing
- agent/provider integration
- API and TUI management flows
- automated tests and current testability constraints
- design consistency with the original implementation plan

### Out of scope

- unrelated pre-existing repository failures unless they block Antigravity verification
- redesigning unrelated provider systems
- production deployment work
- UI polish not directly related to Antigravity management behavior

---

## Review Method

### Phase A - Establish the expected behavior baseline

Use the implementation plan document as the source of truth:
- `.kb/pando/plans/antigravity_oauth_provider_plan.md`

For each plan phase, extract:
- intended deliverable
- intended runtime semantics
- dependencies on previous phases
- explicit design constraints

Deliverable of this phase:
- a normalized checklist of expected behavior per phase

---

### Phase B - Inventory the implemented code

Review all touched Antigravity-related files, including at least:

- `internal/oauth/antigravity/antigravity.go`
- `internal/api/handlers_antigravity_oauth.go`
- `internal/api/handlers_provider_accounts.go`
- `internal/api/handlers_models.go`
- `internal/api/routes.go`
- `internal/llm/models/antigravity.go`
- `internal/llm/models/registry.go`
- `internal/llm/provider/provider.go`
- `internal/llm/provider/antigravity_oauth.go`
- `internal/llm/provider/antigravity_scheduler.go`
- `internal/llm/provider/antigravity_failover.go`
- `internal/llm/agent/agent.go`
- `internal/tui/components/dialog/add_provider.go`
- `internal/tui/page/settings.go`
- all new or modified Antigravity tests

For each file, record:
- purpose
- newly added responsibilities
- coupling points
- any evidence of workaround-style implementation

Deliverable of this phase:
- implementation inventory by file and subsystem

---

### Phase C - Phase-by-phase compliance review

For each original implementation phase, assess:
- implemented as designed
- implemented with substitutions
- partially implemented
- not implemented

Required review dimensions per phase:
1. feature presence
2. runtime correctness
3. architectural alignment
4. operational safety
5. test coverage

Special attention must be paid to:
- whether a dedicated Antigravity provider client actually exists
- whether runtime routing truly uses Antigravity semantics or only Gemini reuse
- whether Gemini CLI fallback is modeled only in scheduler state or is truly executable
- whether logical models are cleanly account-agnostic in all relevant call paths
- whether OAuth state storage is appropriately modeled

Deliverable of this phase:
- a compliance matrix mapping each planned phase to actual implementation status

---

### Phase D - Architecture review

Evaluate the design quality of the resulting implementation.

Questions to answer:
- Is Antigravity represented as a real provider, or only as a scheduling/auth layer over Gemini?
- Is `provider.go` taking on too much Antigravity-specific orchestration logic?
- Is `ExtraHeaders` being used for concerns outside transport headers?
- Are responsibilities cleanly separated between:
  - OAuth package
  - API handlers
  - scheduler
  - provider runtime
  - model registry
- Are persistence side effects located in the right places?
- Are failover actions deterministic and isolated enough for testing?

Deliverable of this phase:
- an architecture assessment with strengths, weaknesses, and technical debt items

---

### Phase E - Test and verification review

Review the quality of validation, not just quantity of tests.

Check:
- which packages have direct passing tests
- which tests are blocked by pre-existing fixture/config/environment issues
- which important runtime paths still lack stable tests
- whether unit tests cover behavior or only surface helpers
- whether critical end-to-end scenarios remain unverified

Minimum scenarios to explicitly assess:
1. start OAuth flow for a new account
2. complete callback and persist tokens
3. refresh expired token automatically
4. use a logical Antigravity model with runtime account selection
5. fail over after quota exhaustion
6. mark account unusable on invalid grant
7. preserve gateway availability while disabling Gemini CLI fallback when project access fails

Deliverable of this phase:
- a testing adequacy report and a prioritized test gap list

---

### Phase F - Follow-up remediation plan

Based on the review, define a concrete backlog of remediation work.

Expected categories:
1. correctness fixes
2. architecture cleanup
3. missing feature completion
4. test harness improvements
5. UX/API cleanup

The backlog should explicitly prioritize likely high-value fixes such as:
- introducing a dedicated Antigravity provider client if still missing
- separating OAuth session metadata from `ExtraHeaders`
- clarifying or implementing real Gemini CLI fallback execution
- reducing Antigravity-specific orchestration complexity in `provider.go`
- stabilizing config-dependent API tests

Deliverable of this phase:
- an ordered implementation backlog with rationale and dependency notes

---

## Review Deliverables

The full review should produce one consolidated review report containing:

1. Executive summary
2. Phase-by-phase compliance matrix
3. Architecture assessment
4. Runtime behavior assessment
5. Security and persistence assessment
6. Testing assessment
7. Confirmed gaps and risks
8. Prioritized follow-up backlog
9. Recommendation on current maturity level:
   - experimental
   - partially usable
   - usable with caveats
   - production-ready

---

## Success Criteria

The review is complete when:
- every planned phase has been classified precisely
- every major Antigravity subsystem has been inspected directly
- architectural deviations from the plan are documented explicitly
- missing or indirect behavior is clearly separated from fully working behavior
- a concrete remediation backlog exists
- the team can make an informed decision about whether Antigravity is actually ready for normal use

---

## Initial Expected Findings to Validate

These are hypotheses, not final conclusions:

1. OAuth support appears substantially implemented.
2. Secret sanitization and persistence appear mostly correct.
3. Scheduler logic appears present but may be only partially connected to a true Antigravity transport.
4. A dedicated Antigravity provider client may still be missing.
5. Gemini CLI fallback may be modeled but not fully executable.
6. `ExtraHeaders` may currently carry too many unrelated responsibilities.
7. API test instability may hide real regressions in persistence-heavy flows.

These points should be confirmed or disproven by the review.

---

## Recommended Execution Order

1. Normalize the original plan into a review checklist.
2. Inventory all Antigravity-related implementation files.
3. Perform the phase compliance review.
4. Perform the architecture review.
5. Review tests and verification quality.
6. Write the remediation backlog.
7. Publish the final review report into KB.

---

## Final Note

The main purpose of this review is to prevent false closure. The current implementation may already be valuable, but the system should not treat the Antigravity provider as complete until the review verifies both behavioral correctness and architectural fidelity to the intended design.
