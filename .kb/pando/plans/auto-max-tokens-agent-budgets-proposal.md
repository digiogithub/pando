# Proposal: automatic max token budgets per agent in Pando

## Summary

This proposal recommends changing Pando so that `agents.<name>.maxTokens` is no longer a required manual tuning knob for normal users. Instead, Pando should support an **automatic output budget mode** that selects a sensible max output token limit based on:

1. the configured agent role,
2. the selected model capabilities,
3. the provider/runtime API shape, and
4. safe context-window guardrails.

Manual numeric `maxTokens` should remain available as an **advanced override**.

## Problem statement

Today, Pando exposes `maxTokens` directly in agent configuration and UI. This has technical value, but poor product ergonomics:

- most users do not know what number is appropriate,
- the meaning is provider/API-specific (`max_tokens`, `max_completion_tokens`, `max_output_tokens`),
- the right value depends more on **agent role** than on user preference,
- current defaults are coarse and inconsistent with task intent,
- current validation (`contextWindow / 2`) is a defensive heuristic, not a user-meaningful policy.

Examples in the current codebase:

- `coder`: `32000`
- `summarizer`: `90000`
- `task`: `16384`
- `title`: `80`

These values are hard for users to reason about and likely over-allocate output budget for several roles, especially `summarizer` and `task`.

## Research basis

### Findings from Pando

Current agent roles in `internal/config/config.go`:

- `coder`
- `summarizer`
- `task`
- `title`
- `cli-assist`
- `persona-selector`

Current behavior:

- `agent.MaxTokens > 0` overrides model defaults.
- invalid or missing values are replaced with `model.DefaultMaxTokens` or a fallback.
- values above `model.ContextWindow / 2` are clamped.
- title is forcibly capped at `80`.
- `cli-assist` currently uses `model.DefaultMaxTokens` directly.

### Findings from provider documentation

#### OpenAI

OpenAI documents output token settings as a way to:

- control cost,
- improve latency,
- avoid overly verbose responses.

OpenAI also emphasizes that token caps are a **hard cutoff**, and that response shape should primarily be guided through prompt instructions, examples, and stop sequences rather than relying on the cap alone.

#### Anthropic

Anthropic distinguishes between:

- output token cap (`max_tokens`),
- reasoning/thinking effort,
- and newer **task budget** concepts for agentic loops.

This suggests that a single manually tuned max output token number is too low-level to be the primary UX control.

#### Gemini

Gemini distinguishes between:

- `maxOutputTokens`, and
- thinking depth (`thinking_level` / thinking config).

Google guidance also frames these controls in terms of task complexity and latency/cost trade-offs rather than as user-facing numbers to tune manually.

## Product direction

### Desired behavior

For standard users:

- Pando should default each agent to **Auto** output budget mode.
- Users should not need to decide a raw token count.
- The UI should show the resolved value for transparency, but not require editing it.

For advanced users:

- Pando should still allow a manual integer override per agent.

### Recommended UX model

Visible setting:

- `Max output tokens`: `Auto` or `Custom`

If `Auto`:

- show helper text such as `Auto currently resolves to 8192 for this model`.

If `Custom`:

- show numeric input with validation.

## Proposed automatic budgets by agent role

These are recommended **starting defaults** for automatic mode before model-based adjustment:

| Agent | Purpose | Proposed auto budget |
|---|---|---:|
| `title` | generate short session title | 80 |
| `persona-selector` | classify best persona / return only a name | 64 |
| `cli-assist` | generate one shell command or a very short command snippet | 256 |
| `task` | break goals into steps / planning output | 2048 |
| `summarizer` | compact conversation into an operational summary | 4096 |
| `coder` | main interactive coding / explanation / tool-using agent | 8192 |

### Optional scaling band

For larger-capability models and workflows where it is beneficial:

- `coder`: allow auto to scale up to `12288` or `16384`
- `summarizer`: allow auto to scale up to `8192`
- `task`: allow auto to scale up to `4096`

This scaling should be automatic and not user-driven.

## Why these values

### Title

A title is intentionally short. Pando already caps this at `80`, which is consistent with the role.

### Persona selector

This agent returns only a persona name or `none`. It should not need a large generation budget.

### CLI assist

This path should produce a single command or very short shell output. A low cap helps keep output terse and reduces the chance of the model returning explanations instead of commands.

### Task

This role is for structured planning. It needs enough room for several steps and rationale, but not long-form essay output.

### Summarizer

The compaction summarizer should produce a concise operational summary replacing prior history. Its main challenge is **large input**, not huge output. A moderate output budget is more appropriate than the current `90000`.

### Coder

The main agent needs the most flexibility because it may explain, plan, produce code, and manage tool-using workflows. It should get the largest automatic budget, but still not default to extreme output unless the model and task justify it.

## Proposed resolution algorithm

Introduce a central runtime resolver for effective max output tokens.

Pseudo-policy:

```text
resolvedMaxTokens = min(
  manualOverrideIfPresentOrAutoBudgetByRole,
  modelOutputLimitOrDefault,
  safeContextGuardrail
)
```

More explicit logic:

1. If the agent has a manual `maxTokens > 0`, use that as the requested budget.
2. Otherwise, use the auto budget for the agent role.
3. Clamp to the model/provider effective output capability.
4. Clamp again to a safe context-derived guardrail.
5. Return the resolved value to all provider constructors.

## Proposed guardrail policy

The current `contextWindow / 2` rule is a useful safety net but too blunt to represent policy.

Recommended replacement:

- keep a hard safety clamp, but
- prefer **role-based contextual fractions** with practical ceilings.

Example conceptual policy:

| Agent | Context-derived upper guardrail |
|---|---|
| `title` | min(128, contextWindow * very_small_fraction) |
| `persona-selector` | min(128, contextWindow * very_small_fraction) |
| `cli-assist` | min(512, contextWindow * small_fraction) |
| `task` | min(4096, contextWindow * medium_fraction) |
| `summarizer` | min(8192, contextWindow * medium_high_fraction) |
| `coder` | min(16384, contextWindow * medium_high_fraction) |

The exact fractions can be tuned, but the design principle is important: output budget should align with agent role, not a single universal fraction.

## Minimal implementation strategy

### Option A: backward-compatible `0 = auto`

This is the lowest-risk path.

Behavior:

- `maxTokens > 0` => manual override
- `maxTokens <= 0` or omitted => Auto

Benefits:

- minimal config format change,
- low migration risk,
- existing users with explicit numbers keep their behavior.

### Option B: explicit mode field

Example:

```toml
[Agents.coder]
Model = "anthropic.claude-sonnet-4-6"
MaxTokensMode = "auto"
MaxTokens = 8192 # only used when mode = custom
```

Benefits:

- clearer semantics,
- cleaner UX,
- easier long-term evolution.

Tradeoff:

- larger config/UI/API change.

## Recommended implementation path

Start with **Option A** (`0 = auto`) to minimize churn.

## Code areas to update

### 1. Config semantics and validation

Files:

- `internal/config/config.go`
- `internal/config/init.go`
- `cmd/init.go`

Changes:

- stop treating `maxTokens <= 0` as invalid for all agents;
- reinterpret it as Auto mode;
- update generated default config templates to use Auto semantics instead of large hardcoded numbers.

### 2. Effective budget resolver

Introduce a shared function, for example:

```go
func ResolveAgentMaxTokens(agentName AgentName, agentCfg Agent, model models.Model) int64
```

Suggested responsibilities:

- determine whether manual override is active,
- choose auto budget by role,
- apply model default/output clamp,
- apply context guardrail,
- return final effective max tokens.

### 3. Provider creation paths

Replace local ad hoc logic in places that currently do:

```go
maxTokens := model.DefaultMaxTokens
if agentConfig.MaxTokens > 0 {
    maxTokens = agentConfig.MaxTokens
}
```

Use the shared resolver instead.

Key files:

- `internal/llm/agent/agent.go`
- `internal/cliassist/llm.go`
- any API or session paths that construct providers directly from model defaults

### 4. UI / API exposure

Files:

- `internal/tui/page/settings.go`
- `web-ui/src/components/settings/AgentsSettings.tsx`
- `internal/api/handlers_config.go`

Changes:

- allow Auto semantics,
- show resolved effective value,
- move raw numeric editing into an advanced/custom mode.

### 5. Tests

Add or update tests for:

- auto resolution by role,
- manual override precedence,
- model default clamp,
- context guardrail clamp,
- migration/backward compatibility,
- title special-case handling,
- cli-assist using the same resolution logic.

## Suggested first automatic defaults for config templates

### Current style

```toml
[Agents.coder]
MaxTokens = 32000

[Agents.summarizer]
MaxTokens = 90000

[Agents.task]
MaxTokens = 16384
```

### Proposed style

```toml
[Agents.coder]
MaxTokens = 0 # Auto

[Agents.summarizer]
MaxTokens = 0 # Auto

[Agents.task]
MaxTokens = 0 # Auto
```

If a comment-friendly template is preferred:

```toml
[Agents.coder]
MaxTokens = 0 # Auto (resolved by agent role and model)
```

## Migration considerations

### Existing configs

Keep existing explicit numeric values unchanged.

### New configs

Use Auto by default.

### Existing validation behavior

Current validation rewrites non-positive values to defaults. This must change so that `0` is a stable Auto state, not an invalid config.

## Risks and tradeoffs

### Pros

- much better UX for most users,
- lower need for manual tuning,
- budgets better aligned with actual agent purpose,
- lower default cost/latency for short-form agents,
- fewer confusing provider-specific implications.

### Risks

- some current users may rely on very large hardcoded defaults,
- summarizer/coder output could become too short if initial auto budgets are too conservative,
- provider-specific quirks may require follow-up tuning.

### Mitigations

- preserve manual override support,
- log resolved max token values in debug mode,
- expose resolved values in UI,
- start with conservative but not tiny defaults,
- add targeted tests around long compaction and coding flows.

## Recommended rollout plan

### Phase 1

- Implement `0 = auto`
- Add central resolver
- Switch provider construction to use resolver
- Update init templates to Auto
- Preserve current title cap

### Phase 2

- Update TUI/web UI to show `Auto` and resolved effective value
- Move manual integer entry behind advanced/custom mode

### Phase 3

- Tune role budgets based on real-world usage and telemetry/log observation
- Consider explicit `MaxTokensMode` if the team wants cleaner config semantics later

## Final recommendation

Pando should move from **manual max token counts as the default user experience** to **automatic per-agent output budgeting with optional manual override**.

The most practical first step is:

- interpret `maxTokens = 0` as Auto,
- use role-based defaults (`title=80`, `persona-selector=64`, `cli-assist=256`, `task=2048`, `summarizer=4096`, `coder=8192`),
- clamp by model capability and safe context rules,
- and surface the resolved value in the UI for transparency.

This change aligns better with how modern providers frame output limits, thinking depth, and agentic budgeting, while giving users a much more understandable configuration model.