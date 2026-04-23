# Anthropic Provider Fix Plan - Pando

## Context

Analysis of pando anthropic.go vs crush (charmbracelet/anthropic-sdk-go fork + catwalk) and opencode (@ai-sdk/anthropic v5). Six issues found.

## SDK Versions

| Project | SDK | Notes |
|---------|-----|-------|
| pando | anthropics/anthropic-sdk-go v1.4.0 | Official SDK |
| crush | charmbracelet/anthropic-sdk-go fork (2026-02-23) | Custom fork with catwalk |
| opencode | @ai-sdk/anthropic (Vercel AI SDK v5) | TypeScript, abstracts API |

## Root Causes Found

1. Missing interleaved-thinking-2025-05-14 and fine-grained-tool-streaming-2025-05-14 beta headers
2. No adaptive thinking support for claude-sonnet-4-6 and claude-opus-4-6
3. ReasoningEffort from agent config not wired into Anthropic thinking budget
4. Tool InputSchema missing Required fields (has TODO comment)
5. Thinking activation is keyword-based (user must say think) instead of model/config-based

## Implementation Phases

### Phase 1 - Add missing beta headers
Fact key: anthropic_fix_phase1_beta_headers

File: internal/llm/provider/anthropic.go newAnthropicClient()

Pando only sends claude-code-20250219. Missing:
- interleaved-thinking-2025-05-14 (required for streaming extended thinking in Claude 4.x)
- fine-grained-tool-streaming-2025-05-14 (better tool streaming)

New header: claude-code-20250219,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14

opencode always sends both. crush adds interleaved-thinking only when thinking is enabled.

### Phase 2 - Adaptive thinking for Claude 4.6
Fact key: anthropic_fix_phase2_adaptive_thinking

File: internal/llm/provider/anthropic.go preparedMessages()

claude-sonnet-4-6 and claude-opus-4-6 support thinking type=adaptive which auto-adjusts budget.
Pando always uses ThinkingConfigParamOfEnabled(maxTokens*0.8) for all CanReason models.

Fix: detect adaptive models by API model name, use ThinkingConfigParamOfAdaptive() for them.

opencode reference: transform.ts:368 isAnthropicAdaptive check.

### Phase 3 - Wire ReasoningEffort into thinking budget
Fact key: anthropic_fix_phase3_reasoning_effort

Files: internal/llm/provider/anthropic.go, internal/llm/agent/agent.go

ReasoningEffort from agent config is only wired for OpenAI, not Anthropic.
Anthropic always uses fixed 80% of maxTokens as thinking budget.

Fix: add WithAnthropicReasoningEffort option, map effort levels to budget fractions:
- low: 20%, medium: 40%, high: 80%, max: maxTokens-1

### Phase 4 - Tool schema Required fields
Fact key: anthropic_fix_phase4_tool_schema_required

File: internal/llm/provider/anthropic.go convertTools()

Line 145-149 has TODO comment: how to pass required fields. info.Required already exists.
Fix: populate InputSchema.Required from info.Required when non-empty.

### Phase 5 - Model-based thinking activation
Fact key: anthropic_fix_phase5_thinking_activation

Files: internal/llm/provider/anthropic.go, internal/llm/agent/agent.go

Thinking only triggers when user message contains think keyword (DefaultShouldThinkFn).
CanReason models like claude-sonnet-4-6 never use thinking unless user explicitly types think.

Fix: when ReasoningEffort != empty OR model.CanReason && agentName==coder, enable thinking unconditionally.
Remove keyword check as the primary gate.

### Phase 6 - Tests and validation
Fact key: anthropic_fix_phase6_tests_validation

- go build ./... passes
- Unit tests for: beta headers, adaptive vs enabled thinking, effort budget mapping, required fields, activation logic
- Smoke tests: claude-3.5-sonnet (no regression), claude-3.7-sonnet thinking, claude-sonnet-4-6 adaptive
- Verify thinking_delta events arrive in stream

## Key File Locations

| File | Role |
|------|------|
| internal/llm/provider/anthropic.go | Provider: headers, thinking, tools (phases 1,2,3,4,5) |
| internal/llm/agent/agent.go | Wiring: effort option to provider (phases 3,5) |

## Reference Implementations

| Feature | crush | opencode |
|---------|-------|---------|
| interleaved beta | coordinator.go:807 conditional | provider.ts:165 always |
| fine-grained-tool beta | not present | provider.ts:165 always |
| adaptive thinking | not present | transform.ts:368 isAnthropicAdaptive |
| effort budget | catwalk ParseOptions | transform.ts:570-583 |
| tool required | catwalk schema | Vercel AI SDK auto |
