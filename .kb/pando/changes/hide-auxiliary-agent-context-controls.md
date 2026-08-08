---
created_at: 2026-08-08T14:36:24.427112739Z
updated_at: 2026-08-08T14:36:24.427112739Z
tags:
    - change
    - config
    - tui
    - webui
    - agents
---
# Hide token-budget / auto-compaction controls for auxiliary agents (2026-08-08)

## What changed

The per-agent `MaxTokens`, `AutoCompact` and `AutoCompactThreshold` knobs are no longer shown in
the TUI settings page nor the web-UI Agents settings for the auxiliary built-in agents
(`summarizer`, `task`, `title`, `cli-assist`, `persona-selector`, `context-enricher`). Only the
`coder` agent — the one driving the long agent loop where the context window actually fills up —
still exposes them. Model, Reasoning Effort and Thinking Mode remain visible for every agent.

The fields are only hidden from the UIs: they still exist in the config struct and can be set by
hand in the TOML config as an advanced override, and `ResolveAgentMaxTokens` /
`AutoBudgetByRole` keep resolving the automatic budget when `MaxTokens == 0`.

## Files / symbols touched

- `internal/config/config.go`: new `AgentExposesContextControls(name AgentName) bool` (true only
  for `AgentCoder`), documented next to `AutoBudgetByRole` / `roleCeilingTokens`.
- `internal/tui/page/settings.go` (`buildAgentsSection`): Model/ReasoningEffort/ThinkingMode are
  always appended; MaxTokens/AutoCompact/CompactThreshold appended only when
  `config.AgentExposesContextControls(agentName)`.
- `internal/api/handlers_config.go`:
  - `AgentConfigItem` gains `contextControls bool` (JSON `contextControls`), populated on GET.
  - `handlePutConfigAgents` now preserves `ContextWindowOverride` for every agent (the API never
    carries it, so each save was resetting it), and preserves
    `MaxTokens`/`AutoCompact`/`AutoCompactThreshold` for agents where the controls are hidden, so
    a UI save cannot wipe a manual TOML override.
- `web-ui/src/types/index.ts`: `AgentConfigItem.contextControls?: boolean`.
- `web-ui/src/components/settings/AgentsSettings.tsx`: `showContextControls` derived from
  `agent.contextControls` (fallback: name === 'coder'); Max Tokens field and the whole
  auto-compact block are rendered conditionally, and the field grid drops to 2 columns when the
  Max Tokens field is hidden.

## Reason

Users were confused by knobs that had no visible effect for short, single-shot agents (title
generation, summarization, persona selection, context enrichment): their output budget is already
resolved automatically per role and auto-compaction never triggers for them in practice. Related
background: `pando/plans/auto-max-tokens-agent-budgets-proposal.md`.

## Verification

- `go build ./internal/...` — clean.
- `go test ./internal/api ./internal/config ./internal/tui/page` — all ok.
- `npx tsc --noEmit` in `web-ui` — clean.

[[auto-max-tokens-agent-budgets-proposal]]
