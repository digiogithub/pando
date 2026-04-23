# Gemini Provider Fix Plan - Pando

## Context

The Gemini provider in pando fails with retry errors when using gemini-3.1-pro-preview-customtools (Gemini 3.x). Root causes identified by comparing with crush (charm.land/catwalk + genai v1.51.0) and opencode (@ai-sdk/google).

## Root Causes

1. genai SDK outdated - pando uses v1.3.0, crush uses v1.51.0. v1.3.0 lacks ThinkingLevel field in ThinkingConfig.
2. ThinkingConfig not model-aware - stream() hardcodes {IncludeThoughts: true} with no budget/level. send() has no ThinkingConfig at all.
3. stream() infinite loop risk - if finalResp == nil after stream loop completes, outer for{} retries indefinitely.
4. Thought parts not filtered - when IncludeThoughts=true, thought parts bleed into visible response content.
5. Gemini 3.x models not statically defined - no constants for gemini-3.x, relying on dynamic discovery.

## Implementation Phases

### Phase 1 - Update genai SDK
Fact key: gemini_fix_phase1_sdk_update

Upgrade google.golang.org/genai from v1.3.0 to v1.51.0.
- go get google.golang.org/genai@v1.51.0 && go mod tidy
- Unlocks ThinkingLevel field in ThinkingConfig struct
- No breaking changes expected

### Phase 2 - Model-aware ThinkingConfig helper
Fact key: gemini_fix_phase2_thinking_config

Add buildThinkingConfig() *genai.ThinkingConfig to geminiClient in internal/llm/provider/gemini.go:
- gemini-3.x => ThinkingLevel: HIGH, IncludeThoughts: true
- gemini-2.5.x => ThinkingBudget: ptr(int32(2000)), IncludeThoughts: true
- older models => nil

### Phase 3 - Fix send() and harden stream()
Fact key: gemini_fix_phase3_send_stream_fix

1. send() line ~192: add config.ThinkingConfig = g.buildThinkingConfig()
2. stream() line ~295: replace hardcoded ThinkingConfig with g.buildThinkingConfig()
3. stream() after inner loop: guard finalResp == nil to avoid infinite outer loop

### Phase 4 - Filter thought parts from response
Fact key: gemini_fix_phase4_thinking_parts

Check part.Thought == true before adding to content. In both send() and stream() loops.

### Phase 5 - Static Gemini 3.x model definitions
Fact key: gemini_fix_phase5_model_definitions

Add to internal/llm/models/gemini.go:
- Gemini31ProPreview = gemini.gemini-3.1-pro-preview-customtools (CanReason=true)
- Gemini30Flash = gemini.gemini-3-flash (CanReason=true)

### Phase 6 - Tests and validation
Fact key: gemini_fix_phase6_tests_validation

- go build ./... must pass
- Unit test buildThinkingConfig() for gemini-3.x, gemini-2.5.x, gemini-2.0.x
- Smoke test gemini-2.5-pro (no regression)
- Smoke test gemini-3.1-pro-preview-customtools (originally failing)
- Verify thought content does not leak into chat output

## Key Files

- internal/llm/provider/gemini.go (phases 2,3,4)
- internal/llm/models/gemini.go (phase 5)
- go.mod (phase 1)

## Reference Implementations

- crush: internal/agent/coordinator.go:340 - detects gemini-2 vs gemini-3 for ThinkingConfig
- opencode: packages/opencode/src/provider/transform.ts:789-797 - same via @ai-sdk/google
- genai v1.51.0 ThinkingConfig.ThinkingLevel enum: LOW/MEDIUM/HIGH/MINIMAL
