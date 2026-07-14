---
created_at: 2026-07-14T15:37:25.338620883Z
updated_at: 2026-07-14T15:37:25.338620883Z
tags:
    - caveman
    - documentation
    - readme
    - token-optimization
    - complete
---
# Caveman output-brevity mode — Phase 7 (docs + quality gates) COMPLETE

Closes [[caveman-persistent-output-brevity-mode]]. The feature is now complete
end to end (P1–P7). Continues [[caveman_phase5_6_settings_ui]].

## What changed

Documentation only — no behaviour change, no Go/TS source touched in this phase.

**`README.md`** (the config reference lives in the shipped TOML template, which
Phase 2 already documented in `internal/config/init.go`):

1. **Features list** — new bullet for *Caveman Output Brevity*, placed just above
   the Superpowers bullet: what it does, `/caveman` + `/caveman-finish` +
   `[Caveman] DefaultMode`, off by default, "reduces output tokens only", linking
   to the new section.
2. **Built-in Slash Commands table** — two new rows,
   `/caveman [lite|full|ultra|wenyan]` and `/caveman-finish`, next to `/ponytail`.
3. **New section `### Caveman output brevity (opt-in)`**, before the Superpowers
   section, covering everything Phase 7 requires:
   - What is removed (greetings, restatement, transitions, redundant summaries).
   - **What is never touched**: code, commands, paths, URLs, JSON/YAML/TOML, error
     text, API signatures, test output, security warnings, approval questions —
     and it may not skip root-cause evidence, test commands/results or safety
     caveats to be shorter. A direct request for detail always wins.
   - **Level table** (`lite`, `full`, `ultra`, `wenyan`) whose wording was checked
     against the actual `caveman.Instructions()` strings rather than paraphrased
     from the plan. Replies stay in the user's language except under `wenyan`.
   - **Config snippet** `[Caveman] DefaultMode = ''` + where the setting lives in
     the UI (`Token Optimization → Caveman Output Brevity`).
   - **Precedence chain**, stated explicitly: direct user instruction / project
     rules → explicit session choice (`/caveman <level>` or `/caveman-finish` as
     explicit off) → `Caveman.DefaultMode` → off. Notes that the session override
     is in-memory (does not survive a restart) while the TOML default does.
   - **Measurement honesty** (plan §7.2/7.3): input and reasoning tokens are NOT
     reduced; the injected policy has its own input cost, so on already-terse work
     total savings can be small or negative; savings depend on model and task, so
     **Pando ships no percentage claim**. No telemetry/stats were added.
   - Upstream credit: style levels follow juliusbrussee/caveman (MIT),
     reimplemented natively as a prompt policy — no hooks, telemetry, stats or MCP
     middleware.

## Verification (full quality gate)

- `go build ./...` — clean.
- `go test ./...` — **entire suite passes (exit 0)**, no failures to record.
  (The `internal/mcpgateway` TOML failures previously noted as pre-existing did
  not reproduce.)
- `go test -race ./internal/caveman ./internal/llm/agent ./internal/config ./internal/api`
  — all ok, including the agent package.
- Web UI: `npx tsc --noEmit` clean; `npx eslint .` → 0 errors (2 pre-existing
  `react-refresh/only-export-components` warnings in `KeyValueEditor.tsx`, an
  untouched file); `npm run build` (tsc -b + vite + PWA) succeeds.
- `git status` shows no stray build artifacts (`web-ui/dist` is ignored).

## Feature status

All 7 phases done: core package + session resolver, TOML config + `UpdateCaveman`,
per-turn injection (skills → superpowers → caveman → ponytail), slash commands on
TUI/WebUI/ACP, TUI + Web UI settings for the global default, and now docs.
