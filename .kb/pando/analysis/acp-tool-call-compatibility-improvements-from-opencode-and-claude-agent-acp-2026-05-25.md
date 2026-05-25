# ACP tool-call compatibility improvements for Pando from OpenCode and claude-agent-acp

## Summary
This analysis compares how OpenCode and `claude-agent-acp` emit ACP tool-call notifications, with special attention to Zed compatibility. The main conclusion is that Zed-visible tool input depends primarily on `rawInput`, not on custom `_meta`. Pando already sends more structured and richer ACP payloads than OpenCode in several places, but `claude-agent-acp` still follows some compatibility patterns that are likely safer for Zed:

1. emit a canonical `tool_call` early and keep the lifecycle monotonic,
2. make `rawInput` available on the first visible tool card whenever possible,
3. repeat `rawInput` on follow-up updates,
4. split terminal transport metadata into dedicated updates and gate it by client capability,
5. keep hook-specific metadata separate from terminal metadata,
6. keep live and replay payload shapes as identical as possible.

## Codebases inspected
- Pando: `/www/MCP/Pando/pando/internal/mesnada/acp/`
- OpenCode: `/www/MCP/Pando/opencode/packages/opencode/src/acp/agent.ts` and `src/session/processor.ts`
- claude-agent-acp: `/www/MCP/Pando/claude-agent-acp/src/acp-agent.ts` and `src/tools.ts`

## OpenCode behavior

### Tool start
OpenCode always emits a `tool_call` first from `toolStart()`:
- `sessionUpdate: "tool_call"`
- `status: "pending"`
- `title: part.tool`
- `kind`
- `locations: []`
- `rawInput: {}`

This creates a stable card immediately, but the first payload is usually not enriched.

### Running updates
When the internal tool part reaches `running`, OpenCode emits `tool_call_update` with:
- `status: "in_progress"`
- `kind`
- `title`
- `locations`
- `rawInput: part.state.input`
- optional `content`

### Completion and failure
OpenCode emits `tool_call_update` with:
- `status: "completed"` or `"failed"`
- `rawInput`
- `rawOutput`
- final content

### Plan behavior
For `todowrite`, OpenCode emits a `plan` update with the full `entries` array.

### Strengths and weaknesses
Strengths:
- simple monotonic lifecycle,
- predictable `tool_call` → `tool_call_update` sequence,
- same essential fields are present on running and final updates.

Weaknesses:
- the initial tool card is often created with empty `rawInput`,
- little ACP `_meta` richness,
- terminal behavior is simpler than claude-agent-acp.

## claude-agent-acp behavior

## Core pattern
`claude-agent-acp` converts Claude SDK tool-use/tool-result messages into ACP notifications mostly through helpers in `src/tools.ts` and dispatch in `src/acp-agent.ts`.

It keeps a `toolUseCache` so that later result updates can recover the original tool input and preserve lifecycle consistency in both live and replay paths.

## Tool start
On `tool_use`, it computes presentation info through `toolInfoFromToolUse()`:
- human-friendly `title` derived from structured input,
- `kind`,
- `content`,
- `locations`,
- bash terminal reference when terminal mode is supported.

Important detail: for Bash the title is the actual command when available:
- `title: input.command ? input.command : "Terminal"`

This is more informative than a generic `bash` title.

The initial ACP tool card includes the actual structured input from the tool-use event, not a later reconstruction. This is the most relevant compatibility detail for Zed.

## Tool result updates
For tool results, `claude-agent-acp` repeats relevant fields and includes `rawOutput`. Tests confirm this for string, array, JSON-like and bash result shapes.

For `TodoWrite`, the wrapper suppresses the normal tool result update and emits `plan` instead.

## Terminal behavior
This wrapper is especially disciplined for terminal tools:

### Capability gating
It only emits terminal `_meta` when the client advertises:
- `clientCapabilities._meta.terminal_output === true`

If the capability is absent, it falls back to normal text content instead of terminal metadata.

### Split notifications
When terminal output is supported, it emits separate notifications:
1. initial `tool_call` with `_meta.terminal_info`,
2. `tool_call_update` with `_meta.terminal_output` only,
3. final `tool_call_update` with `_meta.terminal_exit` plus completion status/content.

Tests explicitly verify that:
- `terminal_output` and `terminal_exit` are split into separate notifications,
- the completion notification does not redundantly include `terminal_info` or `terminal_output`,
- fallback content is used when terminal capability is unavailable.

### Hook metadata separation
Post-tool-use hooks add `_meta.claudeCode`, but terminal metadata is kept out of hook updates when terminal output was already sent separately. This avoids mixed concerns in a single update.

## Why claude-agent-acp is likely perceived as better in Zed
Likely reasons:
1. the first visible tool card is usually better enriched,
2. titles are derived from meaningful input early,
3. terminal events are capability-aware and split cleanly,
4. the notification lifecycle is highly test-driven,
5. replay and live conversion both rely on the same notification helpers and cache model.

## Pando current behavior

## What Pando already does well
Pando already implements several advanced ACP behaviors in `prompt_handler.go` and `session_state.go`:
- reconstructs meaningful titles from tool input,
- sends `rawInput` and `rawOutput`,
- includes locations and rich content blocks,
- handles bash with terminal references plus terminal `_meta`,
- suppresses `TodoWrite` tool rendering in favor of `plan`,
- tries to avoid creating a permanently empty tool card in Zed,
- synthesizes missing starts when updates/results arrive before a start was observed.

In some ways this is richer than OpenCode.

## Current Pando risk areas

### 1. First visible card may still be ambiguous for Zed
Pando tries to avoid empty starts and sometimes delays `StartToolCall` until input is useful. This is rational, but some ACP clients behave best when they see a canonical `tool_call` very early. If Zed internally anchors rendering to the first tool event, any ambiguity in the start/update sequence can make later `rawInput` enrichment less reliable visually.

### 2. Terminal metadata is not capability-gated
Pando currently emits terminal metadata unconditionally for bash-style tools:
- `terminal_info`
- `terminal_output`
- `terminal_exit`

`claude-agent-acp` gates this on client capability. Pando should do the same to align better with ACP expectations and avoid client-specific rendering quirks.

### 3. Terminal completion updates may carry too much combined state
Pando follows the same general three-step lifecycle as claude-agent-acp, but should be reviewed to ensure the completion notification only carries `terminal_exit` and not redundant terminal metadata from earlier steps.

### 4. Hook-like or corrective updates should avoid mixing unrelated metadata
When Pando sends corrective/enrichment updates, metadata should be scoped to the purpose of that notification. If an update exists only to enrich title/input/locations, it should not also redundantly carry terminal transport fields unless needed.

### 5. Live and replay parity should be kept exact
Pando has both live handling in `prompt_handler.go` and replay in `session_state.go`. The payload fields are close, but this area should be kept mechanically aligned so that Zed receives the same shape whether the session is live or restored.

## Recommended improvements for Pando

### High priority
1. **Guarantee a canonical early `tool_call` event for every visible tool call.**
   - Keep the invariant that no `tool_call_update` or result update is ever the first event a client sees for a tool call.
   - If input is already parseable, include structured `rawInput` immediately on that first event.
   - If input is not yet parseable, still prefer a stable early card, then send an immediate enrichment update once useful input appears.

2. **Treat `rawInput` as the primary Zed compatibility field.**
   - Always repeat `rawInput` on enrichment, running, completed, and failed updates.
   - Ensure `rawInput` is a structured object whenever possible, not a string blob.

3. **Unify live and replay builders.**
   - Extract shared helper(s) for building ACP tool start and update payloads so `prompt_handler.go` and `session_state.go` cannot drift.
   - Use one shared strategy for title, kind, content, locations, rawInput, rawOutput and `_meta`.

4. **Add explicit Zed-focused lifecycle tests.**
   - Assert first-event ordering: `tool_call` must precede any `tool_call_update` for the same ID.
   - Assert initial-card enrichment behavior when input starts empty then becomes parseable.
   - Assert `rawInput` presence on first visible event when available.
   - Assert identical payload shape in live vs replay.

### Medium priority
5. **Gate terminal `_meta` by advertised client capability.**
   - Match `claude-agent-acp`: only emit terminal transport metadata when `clientCapabilities._meta.terminal_output === true`.
   - Otherwise emit normal text content fallback.

6. **Split terminal metadata into minimal-purpose notifications.**
   - Initial `tool_call`: `terminal_info` only.
   - Streaming output update: `terminal_output` only.
   - Final completion update: `terminal_exit` only plus status/final content.
   - Avoid repeating prior terminal metadata on the completion update.

7. **Keep corrective enrichment updates narrowly scoped.**
   - If the goal is to repair title/rawInput/locations for Zed, only include those fields plus required status changes.
   - Avoid attaching unrelated `_meta` unless the client needs it for that moment.

8. **Prefer more meaningful titles for execute tools.**
   - For bash-style tools, consider using the command as the title when safe and available, mirroring claude-agent-acp.
   - This can improve Zed readability even when rawInput is not rendered prominently.

### Lower priority
9. **Document the ACP compatibility contract in KB and code comments.**
   - State clearly that Zed-visible tool arguments depend on `rawInput`.
   - State that plan tools should emit `sessionUpdate: "plan"` with full entry arrays.
   - State the terminal capability gating rule.

10. **Add snapshot tests for ACP notifications.**
   - Capture full JSON payloads for common tools: view, write, edit, bash, browser, TodoWrite.
   - Compare live and replay snapshots.

## Suggested concrete implementation areas in Pando
- `internal/mesnada/acp/prompt_handler.go`
  - review `AgentEventTypeToolCall` start/delta behavior,
  - centralize first-event strategy,
  - add capability-aware terminal metadata emission.

- `internal/mesnada/acp/session_state.go`
  - reuse the same payload-building helpers as live mode,
  - ensure replay emits the same `rawInput`, title, locations and `_meta` policy.

- `internal/mesnada/acp/tool_render.go`
  - add explicit capability-aware terminal fallback policy if not already centralized.

- ACP tests
  - extend `internal/mesnada/acp/agent_pando_test.go` and related tests with Zed-oriented lifecycle assertions.

## Practical Pando checklist
- [ ] first visible tool event is always `tool_call`
- [ ] `rawInput` is structured and present as early as possible
- [ ] `rawInput` is repeated on later updates
- [ ] live and replay payloads match
- [ ] bash terminal `_meta` is capability-gated
- [ ] terminal output and terminal exit are separate notifications
- [ ] `TodoWrite` emits only `plan` updates where appropriate
- [ ] titles are derived from useful input when available

## Bottom line
OpenCode proves the basic ACP lifecycle pattern. `claude-agent-acp` demonstrates the most Zed-friendly discipline: early stable tool cards, strong use of structured input, capability-aware terminal transport, split terminal notifications, and extensive tests around those behaviors. Pando should preserve its richer semantics, but simplify and standardize the lifecycle around those compatibility rules so Zed reliably shows tool input metadata and terminal state.
