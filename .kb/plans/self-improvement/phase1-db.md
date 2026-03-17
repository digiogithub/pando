---
name: Self-Improvement Phase 1 — Database Schema
description: Migration file for self-improvement tables: prompt_templates, session_scores, prompt_ucb_stats, skill_library
type: project
---

# Phase 1: Database Schema

**File:** `internal/db/migrations/20260318000001_add_self_improvement.sql`

## Tables

### prompt_templates
Stores versioned template variants that UCB will compare and rank.

```sql
CREATE TABLE IF NOT EXISTS prompt_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    section TEXT NOT NULL,     -- 'base' | 'capabilities' | 'environment' | 'skills' | 'full'
    content TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    is_default BOOLEAN NOT NULL DEFAULT 0,  -- seed/original template
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(name, version)
);
CREATE INDEX IF NOT EXISTS idx_prompt_templates_name ON prompt_templates(name, is_active);
```

### session_scores
Stores reward score computed for each session. One row per session evaluation.

```sql
CREATE TABLE IF NOT EXISTS session_scores (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    template_id TEXT REFERENCES prompt_templates(id),
    reward REAL NOT NULL DEFAULT 0.0,       -- R = α*S_success + β*S_tokens
    success_score REAL NOT NULL DEFAULT 0.0, -- S_success: 1.0 or 0.0
    efficiency_score REAL NOT NULL DEFAULT 0.0, -- S_tokens: normalized [0,1]
    judge_analysis TEXT,                        -- JSON from LLM judge output
    judge_model TEXT,                           -- model used for evaluation
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    message_count INTEGER NOT NULL DEFAULT 0,
    user_corrections INTEGER NOT NULL DEFAULT 0,
    evaluated_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_session_scores_session ON session_scores(session_id);
CREATE INDEX IF NOT EXISTS idx_session_scores_template ON session_scores(template_id);
```

### prompt_ucb_stats
UCB algorithm state — updated after each evaluation. Drives template selection.

```sql
CREATE TABLE IF NOT EXISTS prompt_ucb_stats (
    template_id TEXT PRIMARY KEY REFERENCES prompt_templates(id) ON DELETE CASCADE,
    times_used INTEGER NOT NULL DEFAULT 0,
    total_reward REAL NOT NULL DEFAULT 0.0,
    avg_reward REAL NOT NULL DEFAULT 0.0,
    ucb_score REAL NOT NULL DEFAULT 9999.0,  -- high initial value = explore first
    last_used_at INTEGER,
    updated_at INTEGER NOT NULL
);
```

### skill_library
Skills/rules extracted by the LLM judge from successful sessions.

```sql
CREATE TABLE IF NOT EXISTS skill_library (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    content TEXT NOT NULL,               -- concise rule, max 2 lines
    source_session_id TEXT,
    source_template_id TEXT REFERENCES prompt_templates(id),
    task_type TEXT,                      -- 'code' | 'refactor' | 'debug' | 'explain' | 'general'
    usage_count INTEGER NOT NULL DEFAULT 0,
    success_rate REAL NOT NULL DEFAULT 0.0,
    is_active BOOLEAN NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_skill_library_active ON skill_library(is_active, success_rate DESC);
CREATE INDEX IF NOT EXISTS idx_skill_library_task ON skill_library(task_type, is_active);
```

## Trigger: auto-update UCB stats after session_scores insert

```sql
CREATE TRIGGER IF NOT EXISTS update_ucb_after_score
AFTER INSERT ON session_scores
WHEN NEW.template_id IS NOT NULL
BEGIN
    INSERT INTO prompt_ucb_stats (template_id, times_used, total_reward, avg_reward, updated_at)
    VALUES (NEW.template_id, 1, NEW.reward, NEW.reward, unixepoch())
    ON CONFLICT(template_id) DO UPDATE SET
        times_used = times_used + 1,
        total_reward = total_reward + NEW.reward,
        avg_reward = total_reward / times_used,
        updated_at = unixepoch();
END;
```

## Implementation Notes

- Use `unixepoch()` for all timestamps (consistent with existing migrations)
- `prompt_templates` is seeded on first run by reading existing templates from disk and inserting as `is_default=1, version=1`
- `ucb_score` starts at 9999.0 so all new templates are tried before exploiting best ones
- `session_scores.judge_analysis` is JSON with structure: `{"reasoning": "...", "key_points": [...], "new_skill": "..."}`

**Why:** Additive schema, no changes to existing tables. Cascade delete on session ensures cleanup. Trigger keeps UCB stats synchronized without extra code in the evaluator goroutine.

**How to apply:** Run with goose migration. Seed default templates after migration in app.go initialization.
