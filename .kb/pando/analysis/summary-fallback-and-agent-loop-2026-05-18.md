# Summary fallback and agent loop analysis

Date: 2026-05-18
Project: pando

## User request

Analyze the current summarization system and propose an optimal solution for:

1. Automatic fallback to the coder model when the configured summary model/provider fails.
2. A more robust agent loop based on Crush and OpenCode behavior, especially for context compaction and continuation.

## Current implementation in Pando

### Relevant code paths

- `internal/llm/agent/agent.go:139-195` — `NewAgent`
- `internal/llm/agent/agent.go:408-576` — `processGeneration`
- `internal/llm/agent/agent.go:970-1203` — `Summarize`
- `internal/llm/agent/agent.go:1206-1234` — `shouldCompact`
- `internal/llm/agent/agent.go:1242-1293` — summarizer fallback helpers for compaction
- `internal/llm/agent/agent.go:1295-1378` — `compactContext`
- `internal/llm/agent/agent.go:1654-1714` — trimming helpers
- `internal/config/init.go:178-189` — default agent max token settings
- `internal/config/config.go:76-85` — agent config schema

### What is already good

1. Pando already truncates history from `SummaryMessageID` forward in `processGeneration`, which matches the core continuation pattern used by Crush and OpenCode.
2. Pando already has a compaction fallback path for `compactContext`:
   - `NewAgent` initializes:
     - `summarizeProvider` from `AgentSummarizer`
     - `summarizeFallbackProvider` from coder `agentProvider`
   - `sendCompactionSummary` retries with the coder provider if the primary summarizer fails.
3. `compactContext` already trims the source conversation text to the summarizer context window and, on fallback, re-trims to the coder context window.
4. `processGeneration` already resumes after compaction by reloading DB messages and continuing the same tool loop.

## Main current problems

### 1. Manual/background `Summarize()` does not use fallback

`agent.Summarize()` still hard-fails if `a.summarizeProvider == nil` or if `a.summarizeProvider.SendMessages(...)` fails.

So the requested behavior is only partially implemented today: compaction has fallback, full summarization does not.

Impact:
- Manual summarize can fail even when coder would work.
- TUI/ACP users can see summarize errors for provider/API issues that should be recoverable.

### 2. Two separate summarization implementations diverge

There are effectively two flows:

- `Summarize()`
- `compactContext()`

They use different prompts, different retry logic, different persistence flow, and different continuation behavior.

Impact:
- Behavior drift and duplicate bugs.
- One path supports fallback, the other does not.
- One path restarts generation internally; the other only compacts.

### 3. `Summarize()` resumes generation in a goroutine and can get stuck invisibly

After persisting the summary, `Summarize()` builds a new provider and enters its own loop calling `streamAndHandleEvents(...)` with `eventCh=nil`.

Problems:
- If continuation after summary fails, the user only gets a summarize error event, but the original generation flow is not structurally resumed.
- It creates a second continuation loop inside the summarizer path instead of using a single canonical request loop.
- It is harder to reason about cancellation, event ordering, and busy state.
- It differs from Crush’s cleaner “persist summary, requeue/restart main loop” model.

### 4. Auto-compact trigger is based only on `sess.PromptTokens`

`processGeneration()` calls:

- `a.shouldCompact(sess.PromptTokens)`

But both Crush and the user’s problem statement point to failures caused by:
- summary model context read limits
- provider/API failures
- output/max-write token limits

And `shouldCompact()` currently ignores:
- completion tokens already accumulated in the session
- remaining budget for the active model’s max output
- summarizer output budget / summarizer-specific limits

Impact:
- Trigger can come too late or too early.
- It does not robustly protect against generation hitting output ceilings.

### 5. Default summarizer max tokens is too large

Defaults in `internal/config/init.go`:

- coder: `32000`
- summarizer: `90000`

This is risky because the user explicitly reports failures due to maximum writable output, not just input context.

A summarizer should generally have:
- large enough input allowance
- moderate output allowance

`90000` is unusually high for a compaction summary and can cause provider-side max output failures or pathological delays.

### 6. Current compaction source format is lossy and token estimation is rough

`compactContext()` converts the whole conversation into plain text with `User:` / `Assistant:` prefixes and then trims by `chars/4` heuristics.

This works, but it loses structure:
- tool calls / tool results
- finish reasons
- explicit session interruption state

This is weaker than Crush’s approach of summarizing actual session messages with a purpose-built prompt.

## Comparison against Crush and OpenCode

### Crush strengths relevant here

1. Single canonical loop with stop condition based on remaining context.
2. On compaction:
   - summarize
   - persist summary
   - if interrupted mid-task, requeue original request with an interruption prefix
   - restart the main loop
3. Summary becomes the first effective user message on next run.
4. Summary prompt is operational and takeover-oriented.

### OpenCode strengths relevant here

1. Summary is a dedicated provider, but continuation path remains centered on the main generation loop.
2. History truncation from `SummaryMessageID` is simple and effective.
3. Auto-compact threshold is easy to reason about.

### Best combined direction for Pando

Use Crush’s loop ownership and restart semantics, plus OpenCode/Pando’s simple `SummaryMessageID`-based truncation.

## Optimal solution proposal

## A. Unify summarization behind one internal primitive

Create a single internal function, conceptually something like:

- `generateAndPersistSummary(ctx, sessionID, mode)`

Responsibilities:
1. Load session messages.
2. Build a structured summarization prompt.
3. Select provider with fallback order:
   - configured summarizer
   - coder provider
4. Fit input to the chosen provider’s real context window.
5. Enforce a bounded summary output budget.
6. Persist summary message.
7. Update `SummaryMessageID`, token counters, and cost.
8. Return metadata:
   - summary text
   - used provider/model
   - whether fallback was used

Then both:
- `Summarize()`
- `compactContext()`

should call that same primitive.

This removes the current divergence.

## B. Implement fallback for all summarization paths

Required behavior:

1. If the summarizer provider is unavailable at creation time, use coder immediately.
2. If the summarizer provider returns an error at request time, retry automatically with coder.
3. If the summarizer fails due to context/input size, rebuild the request trimmed to coder’s window and retry.
4. If the summarizer fails due to output/max token limits, retry with coder using a lower explicit output budget.

### Recommended fallback policy

Attempt order:
1. Configured summarizer model/provider
2. Coder model/provider

Retry only once per provider class to avoid loops.

Log clearly:
- summarizer model id
- fallback model id
- failure reason category
- whether request was retrimmed

## C. Stop resuming generation from inside `Summarize()`

This is the biggest loop design change.

### Current behavior
`Summarize()` persists the summary and then starts another generation loop in its own goroutine.

### Proposed behavior
`Summarize()` should only:
1. generate summary
2. persist summary
3. emit summarize-complete event
4. return

No hidden continuation loop.

### Why
- keeps one owner of the agent loop
- avoids concurrency surprises
- matches Crush’s simpler control flow
- makes failure handling explicit

If a summary is triggered during active generation, the active generation path should decide how to continue.

## D. Make compaction continuation owned by the main generation loop

For auto-compact during `processGeneration()`:

1. Detect compaction need before the next provider call.
2. Persist summary through the unified summarization primitive.
3. Rebuild `msgHistory` from `SummaryMessageID` onward.
4. If the previous assistant turn had pending tool work, append an interruption/resume instruction to the next user-visible turn or inject a synthetic continuation marker.
5. Continue in the same outer loop.

This keeps the current good part of Pando, but with shared summary generation logic.

## E. Add explicit interruption semantics like Crush

Pando already sanitizes unmatched tool calls, which is good. But it should also explicitly preserve interruption context when compaction occurs mid-loop.

Recommended behavior when compacting after a tool-use turn:

Add a short synthetic user message after the summary, for example:

- "The previous agent iteration was interrupted by automatic context compaction. Continue from the latest tool results and complete the original task without restarting completed work."

Why this helps:
- improves continuity for models after compaction
- reduces repeated work
- mirrors Crush’s requeue intent without needing recursive `Run()`

This should be injected only when compaction happens during an unfinished tool-driven workflow, not for every manual summarize.

## F. Trigger compaction on remaining budget, not just prompt tokens

Replace current `shouldCompact(sess.PromptTokens)` with a calculation based on total session usage and remaining budget.

### Recommended trigger inputs
For the active coder model:
- `used = session.PromptTokens + session.CompletionTokens`
- `contextWindow = override or provider.Model().ContextWindow`
- `reservedOutput = min(agent.MaxTokens, safe cap)`
- `reservedToolMargin = fixed safety buffer`
- `remaining = contextWindow - used`

Trigger compaction when:
- `remaining <= max(fixedBuffer, reservedOutput + reservedToolMargin)`

This is closer to Crush’s “remaining context threshold” approach.

### Suggested thresholds
- For very large windows: fixed buffer like 20k–32k.
- For normal windows: 15%–20% of context window.
- Also require a minimum reserve for one more assistant turn plus tool protocol overhead.

This is more robust than checking only prompt tokens.

## G. Bound summarizer output aggressively

The summarizer should not have `MaxTokens=90000` by default.

Recommended practical defaults:
- summarizer default output budget: `2048` to `4096`
- coder default output budget: keep larger

Even if the summarizer model can output more, compaction summaries should remain operationally concise.

Recommendation:
- lower default summarizer `MaxTokens` from `90000` to `4096`
- optionally scale up only for extremely large context windows or explicit user config

This directly addresses the user’s “maximum write” issue.

## H. Improve summarization prompt and input shape

Use a stronger operational prompt modeled more closely on Crush:

Required sections:
- Current task/request
- Constraints and non-goals
- Work completed
- Files touched / important code locations
- Pending tool results or interrupted actions
- Exact next steps

Also include:
- commands that succeeded/failed
- unresolved errors
- assumptions/blockers

This prompt should be shared by both manual summarize and auto-compact.

## I. Prefer structured message slicing over whole-conversation flattening when possible

Better than converting everything to one flat text blob:

1. First try to summarize the real message list after trimming message history to the summarizer budget.
2. Only fall back to flattened text if provider/message-shape constraints make that necessary.

Benefits:
- keeps tool-result structure longer
- better preserves recency and role boundaries
- more faithful continuation context

If staying with flattened text for now, at least include explicit markers for:
- tool calls
- tool results
- interruptions
- summary boundaries

## J. Failure policy for compaction inside the agent loop

If auto-compaction fails:

1. First retry with coder fallback if not already tried.
2. If both fail:
   - emit warning/error event
   - continue the existing loop only if remaining budget is still safe enough for one final turn
   - otherwise stop cleanly with an actionable error, not a silent stall

Recommended stop message:
- compacting failed on both summarizer and coder fallback; session is near context exhaustion; please change model or compact manually after reducing context.

This avoids the current “sometimes it does not answer and the process stops” failure mode.

## K. Recommended implementation plan

### Phase 1 — low-risk fixes
1. Make `Summarize()` use coder fallback exactly like `compactContext()`.
2. Lower summarizer default `MaxTokens` from `90000` to `4096`.
3. Improve logging and event messages to indicate fallback use.
4. Make `shouldCompact()` evaluate total used tokens, not only prompt tokens.

### Phase 2 — loop hardening
5. Extract shared `generateAndPersistSummary(...)` primitive.
6. Refactor `compactContext()` and `Summarize()` to use it.
7. Remove hidden continuation loop from `Summarize()`.
8. Add explicit interruption continuation marker for mid-tool compaction.

### Phase 3 — quality improvements
9. Replace the simple summarization prompt with a structured takeover prompt.
10. Improve budget calculation to reserve both output and tool overhead.
11. Consider message-structured summarization before plain-text flattening.

## Concrete recommendations

### Must do
- Apply automatic coder fallback in `Summarize()` as well as compaction.
- Reduce summarizer max output budget sharply.
- Remove internal continuation loop from `Summarize()`.
- Base compaction trigger on remaining context, not just prompt tokens.

### Strongly recommended
- Unify summarize/compact code paths.
- Add interruption marker when compaction happens mid-tool workflow.
- Standardize one high-quality summary prompt.

### Nice to have
- Categorize provider failures to decide whether retrimming is needed.
- Track metrics for compaction attempts, fallback usage, and failures.
- Add tests covering:
  - summarizer unavailable -> coder fallback
  - summarizer API failure -> coder fallback
  - summarizer smaller context than coder
  - summarizer max-output failure
  - auto-compaction during tool loop continuation

## Bottom line

Pando already contains part of the requested solution in `compactContext()`, but the system is inconsistent because manual/background `Summarize()` still lacks fallback and because the summarize path owns its own continuation loop.

The optimal design is:
- one summary generation primitive
- summarizer -> coder automatic fallback everywhere
- bounded summarizer output
- compaction trigger based on remaining context budget
- continuation owned only by the main agent loop, with explicit interruption markers when compaction happens mid-task

That gives Pando the strongest parts of Crush and OpenCode while preserving its current `SummaryMessageID` continuation model.