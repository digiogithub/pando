# Self-Improvement System for Pando — Implementation Plan

**Date:** 2026-03-17
**Status:** Planned
**Priority:** High

## Overview

Autonomous self-improvement loop that evaluates session quality after each conversation, scores prompt templates using UCB (Multi-Armed Bandit), and evolves a Skill Library via LLM-as-Judge using a cheap/fast configurable model.

**Core Loop:** Reward → Evaluate → Evolve

### Key References
- Reflexion (NeurIPS 2023, Shinn et al.) — verbal reinforcement without weight updates
- SAGE (Dec 2025, Amazon) — Skill Library with RL-based self-evolution
- MASPOB (Mar 2026) — bandit-based prompt optimization with UCB
- LLM-as-a-Judge (2024-2025) — scalable automated evaluation

## Architecture Summary

```
Session End
    │
    ▼
RewardCalculator (R = α·S_success + β·S_tokens)
    │
    ├── S_success: 1.0 if no user corrections detected (regex patterns)
    └── S_tokens:  normalized efficiency vs. rolling baseline
    │
    ▼
session_scores table ──→ DB trigger ──→ prompt_ucb_stats (auto-update)
    │
    ├── if reward > 0.5 → LLM Judge (cheap model, async)
    │       │
    │       └── JudgeOutput: reasoning, key_points, new_skill
    │               │
    │               └── if confidence > 0.7 → skill_library table
    │
    ▼
Next Session Start
    │
    └── UCB Selector: UCB_i = μ_i + c·√(ln(N)/n_i)
            │
            ├── if N < minSessionsForUCB → use default template
            └── else → pick template with highest UCB score
                        + inject top skills from skill_library
```

## 6 Implementation Phases

### Phase 1: Database Schema
**File:** `internal/db/migrations/20260318000001_add_self_improvement.sql`
**Detail:** `.kb/plans/self-improvement/phase1-db.md`

Tables:
- `prompt_templates` — versioned template variants with UCB tracking
- `session_scores` — per-session reward decomposition
- `prompt_ucb_stats` — UCB state (auto-updated by trigger)
- `skill_library` — evolved prompt rules from LLM judge

### Phase 2: Config Extension
**File:** `internal/config/config.go`
**Detail:** `.kb/plans/self-improvement/phase2-config.md`

New `[evaluator]` section:
```toml
[evaluator]
enabled = true
model = "claude-haiku-4-5-20251001"  # cheap judge model
provider = "anthropic"
alphaWeight = 0.8     # S_success importance
betaWeight = 0.2      # S_tokens importance
explorationC = 1.41   # UCB exploration factor
minSessionsForUCB = 5
maxSkills = 100
async = true
```

### Phase 3: Core Evaluator Service
**Package:** `internal/evaluator/`
**Detail:** `.kb/plans/self-improvement/phase3-evaluator.md`

Files:
- `service.go` — main Service interface + EvaluatorService
- `reward.go` — R = α·S_success + β·S_tokens calculation
- `ucb.go` — UCB1 algorithm + template selection
- `judge.go` — LLM-as-Judge with cheap provider
- `skill.go` — Skill Library CRUD + injection

### Phase 4: Integration
**Files:** `session/session.go`, `llm/prompt/builder.go`, `app/app.go`, `luaengine/types.go`
**Detail:** `.kb/plans/self-improvement/phase4-integration.md`

Integration points:
1. `session.EndSession()` → trigger async `EvaluateSession()`
2. `prompt/builder.go:BuildPrompt()` → UCB template selection per section
3. `prompt/builder.go:buildSkillsSection()` → inject Skill Library rules
4. New `HookType: hook_evaluation_complete` in luaengine
5. `app.go` → initialize EvaluatorService, seed default templates

### Phase 5: SQLC Queries
**File:** `internal/db/sql/self_improvement.sql`
**Detail:** `.kb/plans/self-improvement/phase5-sqlc.md`

Key queries: InsertSessionScore, ListActiveTemplatesBySection (UCB-sorted),
GetTokenBaseline, CountSessionScores, InsertSkill, DeactivateLowestSkill,
ListUCBRanking (for TUI), ListActiveSkillsByType

### Phase 6: TUI Evaluator Page
**Files:** `internal/tui/page/evaluator.go`, `internal/tui/components/evaluator/`
**Detail:** `.kb/plans/self-improvement/phase6-tui.md`

3-panel page:
- Header: total evaluations, avg reward, best template, skill count
- Template UCB Rankings table (rank, name, section, avg_reward, ucb_score)
- Skill Library list (task type, success_rate, usage, content)

Access: `Ctrl+E` keybinding

## Key Design Decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| Algorithm | UCB1 (Multi-Armed Bandit) | Stateless, SQLite-native, no ML infra needed |
| Evaluation model | Configurable cheap model | Separates expensive main agent from cheap judge |
| Evaluation timing | Async goroutine after session end | Never blocks user |
| Judge invocation | Only if reward > 0.5 | Avoids wasting tokens on failed sessions |
| Skill threshold | LLM confidence > 0.7 | Prevents low-quality rule accumulation |
| Default behavior | disabled = false | Opt-in only, no impact on existing users |
| Corrections detection | Regex on user messages | No external dependencies, multilingual |
| Template seeding | On first run from registry | UCB always has baseline to compare against |

## Files to Create/Modify

| File | Action | Phase |
|------|--------|-------|
| `internal/db/migrations/20260318000001_add_self_improvement.sql` | CREATE | 1 |
| `internal/config/config.go` | MODIFY (add EvaluatorConfig) | 2 |
| `internal/evaluator/service.go` | CREATE | 3 |
| `internal/evaluator/reward.go` | CREATE | 3 |
| `internal/evaluator/ucb.go` | CREATE | 3 |
| `internal/evaluator/judge.go` | CREATE | 3 |
| `internal/evaluator/skill.go` | CREATE | 3 |
| `internal/evaluator/types.go` | CREATE | 3 |
| `internal/evaluator/repository.go` | CREATE | 5 |
| `internal/db/sql/self_improvement.sql` | CREATE | 5 |
| `internal/session/session.go` | MODIFY (add evaluator hook) | 4 |
| `internal/llm/prompt/builder.go` | MODIFY (UCB selection + skills) | 4 |
| `internal/luaengine/types.go` | MODIFY (new hook type) | 4 |
| `internal/app/app.go` | MODIFY (init evaluator) | 4 |
| `internal/tui/page/evaluator.go` | CREATE | 6 |
| `internal/tui/components/evaluator/table.go` | CREATE | 6 |
| `internal/tui/components/evaluator/skills.go` | CREATE | 6 |
| `internal/tui/components/evaluator/metrics.go` | CREATE | 6 |
| `internal/tui/page/page.go` | MODIFY (register page) | 6 |
| `internal/tui/keys.go` | MODIFY (evaluator keybindings) | 6 |
