# Self-Improvement System — Complete Architecture Analysis

**Date:** 2026-05-30  
**Status:** Current implementation  
**Scope:** `internal/evaluator/`, `internal/llm/prompt/`, `internal/session/`, `internal/app/`, `internal/llm/evaluatortools/`

---

## Overview

Pando's self-improvement system is an autonomous evaluation loop that:
1. Scores every conversation using a reward function after session end
2. Selects the best prompt template variant per section using UCB1 (Multi-Armed Bandit)
3. Extracts reusable behavioral rules ("skills") from high-quality sessions via an LLM judge
4. Injects learned skills into future system prompts

The system is **opt-in** (`evaluator.enabled = false` by default) and designed for **zero failure propagation** — all evaluator errors are logged as warnings and never break normal operation.

---

## Package Map

```
internal/evaluator/
├── types.go         — Data types: PromptTemplate, Skill, Stats, RewardResult, JudgeOutput
├── service.go       — EvaluatorService: main orchestrator, implements Service interface
├── reward.go        — Reward function R = α*S_success + β*S_tokens
├── ucb.go           — UCB1 score: avg_reward + c * sqrt(ln(N)/n_i)
└── judge.go         — LLM-as-Judge: renders prompt, calls cheap model, parses JSON output

internal/llm/prompt/
├── builder.go       — PromptBuilder.Build(): integrates evaluator for template selection + skill injection
└── prompt.go        — globalEvaluator singleton, SetGlobalEvaluator()

internal/session/session.go
  — EndSession() calls globalEvaluator.EvaluateSession() (non-blocking)

internal/app/app.go
  — Wires everything: creates EvaluatorService, evaluatorPromptAdapter, seeds DB templates

internal/llm/evaluatortools/evaluator_tool.go
  — MCP tools: pando_evaluator_stats, pando_evaluator_skills, pando_evaluator_evaluate

internal/db/self_improvement.sql.go
  — SQLC queries: InsertSessionScore, ListActiveTemplatesBySection, InsertSkill, etc.

internal/db/migrations/
  — Tables: prompt_templates, session_scores, prompt_ucb_stats, skill_library
```

---

## Data Flow

### 1. Session Start — Prompt Construction

```
app.go → agent creates PromptBuilder
PromptBuilder.Build(ctx)
  └─ per section (base/identity, capabilities/*, context/*, ...):
       └─ renderSection(ctx, sectionName)
            └─ evaluator.SelectTemplate(ctx, sectionName)
                 ├─ UCB not ready (< MinSessionsForUCB)? → nil, use registry default
                 └─ UCB ready? → compute UCBScore for each template variant
                                  → return best template content
                                  → RecordTemplateSelection(ctx, sessionID, templateID)
  └─ evaluator.GetActiveSkills(ctx, "general")  ← hardcoded "general"
       └─ inject as "## Learned Optimization Rules" section at end of prompt
```

### 2. Session End — Evaluation

```
session.EndSession(ctx, id)
  └─ globalEvaluator.EvaluateSession(ctx, sessionID)
       └─ [async goroutine if cfg.Async=true]
            └─ runEvaluation(ctx, sessionID)
                 ├─ Idempotency check: GetSessionScore → skip if already evaluated
                 ├─ Load messages: msgs.List(ctx, sessionID)
                 ├─ messagesToInfo: convert to []messageInfo
                 ├─ GetTokenBaseline: rolling avg of last N sessions' tokens
                 ├─ calculateReward(msgInfos, patterns, baseline, α, β)
                 │    ├─ S_success = 1.0 - 0.3 * corrections  (min 0)
                 │    ├─ S_tokens  = 1.0 - (totalTokens - baseline) / baseline  (clamped 0-1)
                 │    └─ R = α * S_success + β * S_tokens
                 ├─ InsertSessionScore (persists reward decomposition)
                 └─ if judge != nil && reward.Total > 0.5 || S_success == 1.0:
                      └─ judge.Evaluate(ctx, JudgeMeta{transcript, template, corrections, tokens})
                           └─ renderJudgePrompt → SendMessages → parseJudgeOutput (JSON)
                           └─ saveSkillFromJudge if confidence >= 0.7
                                ├─ Enforce MaxSkills: DeactivateLowestSkill if over limit
                                └─ InsertSkill(title, content, task_type, source_session)
```

### 3. UCB Template Selection (detail)

```go
UCBScore(avgReward, totalSessions, timesUsed, explorationC) float64
  = avgReward + explorationC * sqrt(ln(totalSessions) / timesUsed)
  // timesUsed == 0 → MaxFloat64 (always try unexplored templates first)
```

Templates are stored per `section` name (e.g. `"base/identity"`, `"capabilities/code_indexing"`).
UCB activates only after `MinSessionsForUCB` (default: 5) sessions are evaluated.

---

## Configuration (`EvaluatorConfig`)

```toml
[evaluator]
enabled = true
model = "claude-haiku-4-5-20251001"   # cheap judge model
provider = ""                          # auto-detected from model
alphaWeight = 0.8                      # weight for S_success
betaWeight = 0.2                       # weight for S_tokens
explorationC = 1.41                    # UCB exploration factor (sqrt(2))
minSessionsForUCB = 5                  # min sessions before UCB activates
correctionsPatterns = ["arréglalo", "así no", "está mal", "fix", "wrong"]
maxTokensBaseline = 50                 # rolling window for token baseline
maxSkills = 100                        # skill library size cap
judgePromptTemplate = ""              # optional custom judge prompt path
async = true                           # background evaluation
```

---

## Database Tables

| Table | Purpose |
|---|---|
| `prompt_templates` | Template variants per section (id, section, name, content, version, is_default) |
| `session_scores` | Per-session reward (reward, success_score, efficiency_score, corrections, template_id) |
| `prompt_ucb_stats` | Materialized UCB state (avg_reward, times_used) — updated by DB trigger |
| `skill_library` | Learned rules (title, content, task_type, success_rate, usage_count, active) |

---

## Anti-Cycle Architecture

To avoid import cycles (`internal/llm/tools ← internal/evaluator ← internal/llm/provider ← internal/llm/tools`):

1. `internal/llm/prompt/builder.go` defines `PromptEvaluator` interface with mirror types
2. `internal/app/app.go` provides `evaluatorPromptAdapter` that translates between packages
3. `internal/session/session.go` defines a local `evaluatorService` interface (only `EvaluateSession`)
4. `internal/llm/evaluatortools/` is a separate package just for MCP tool wrappers

---

## Judge Prompt (default)

The judge evaluates sessions on 6 dimensions:
1. **Scope compliance** — Did the agent respect explicit boundaries (NO, NEVER, only change X)?
2. **Step-by-step adherence** — Did it follow numbered steps in order?
3. **Constraint handling** — Did it treat ALL-CAPS instructions as hard constraints?
4. **Anti-patterns** — Scripts when direct action requested, unrequested features, verbose summaries
5. **Iterative correction handling** — Did it incorporate user corrections precisely?
6. **Context utilisation** — Did it use provided file refs, error messages, project context?

Output JSON: `{reasoning, key_points[], new_skill, task_type, confidence}`

Skill is saved only if `confidence >= 0.7`.

---

## Wiring in app.go

```go
// app.go initialization sequence:
evalSvc, _ := evaluator.New(cfg.Evaluator, q, messages)
app.Evaluator = evalSvc
session.SetEvaluator(evalSvc)                                    // trigger on EndSession
prompt.SetGlobalEvaluator(&evaluatorPromptAdapter{svc: evalSvc}) // UCB + skill injection
seedEvaluatorTemplates(ctx, q)                                   // seed default templates in DB
```

---

## MCP Tools Available to Agent

| Tool | Description |
|---|---|
| `pando_evaluator_stats` | UCB rankings, avg reward, skill count, top skills |
| `pando_evaluator_skills` | List skills filtered by task_type |
| `pando_evaluator_evaluate` | Trigger evaluation for a session_id |

These tools allow the agent to introspect its own self-improvement state mid-session.
