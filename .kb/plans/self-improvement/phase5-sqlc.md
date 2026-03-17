---
name: Self-Improvement Phase 5 — SQLC Queries & Repository
description: Type-safe DB queries for prompt_templates, session_scores, prompt_ucb_stats, skill_library using sqlc
type: project
---

# Phase 5: SQLC Queries & Repository

**File:** `internal/db/sql/self_improvement.sql`

All queries use sqlc annotations. After writing this file, run `make sqlc` or `sqlc generate`.

---

## Query File

```sql
-- name: InsertPromptTemplate :one
INSERT INTO prompt_templates (id, name, section, content, version, is_active, is_default, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, unixepoch(), unixepoch())
RETURNING *;

-- name: GetPromptTemplate :one
SELECT * FROM prompt_templates WHERE id = ? LIMIT 1;

-- name: ListActiveTemplatesBySection :many
SELECT pt.*, COALESCE(us.avg_reward, 0.0) as avg_reward, COALESCE(us.times_used, 0) as times_used, COALESCE(us.ucb_score, 9999.0) as ucb_score
FROM prompt_templates pt
LEFT JOIN prompt_ucb_stats us ON pt.id = us.template_id
WHERE pt.section = ? AND pt.is_active = 1
ORDER BY us.ucb_score DESC NULLS LAST;

-- name: CountPromptTemplates :one
SELECT COUNT(*) FROM prompt_templates WHERE is_active = 1;

-- name: InsertSessionScore :one
INSERT INTO session_scores (id, session_id, template_id, reward, success_score, efficiency_score, judge_analysis, judge_model, prompt_tokens, completion_tokens, message_count, user_corrections, evaluated_at, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch(), unixepoch())
RETURNING *;

-- name: GetSessionScore :one
SELECT * FROM session_scores WHERE session_id = ? LIMIT 1;

-- name: CountSessionScores :one
SELECT COUNT(*) FROM session_scores;

-- name: GetTokenBaseline :one
-- Rolling average of total tokens over last N sessions
SELECT AVG(prompt_tokens + completion_tokens) as baseline
FROM (
    SELECT prompt_tokens, completion_tokens
    FROM session_scores
    ORDER BY created_at DESC
    LIMIT ?
);

-- name: GetUCBStats :one
SELECT * FROM prompt_ucb_stats WHERE template_id = ?;

-- name: UpsertUCBStats :one
INSERT INTO prompt_ucb_stats (template_id, times_used, total_reward, avg_reward, ucb_score, last_used_at, updated_at)
VALUES (?, 1, ?, ?, ?, unixepoch(), unixepoch())
ON CONFLICT(template_id) DO UPDATE SET
    times_used = times_used + 1,
    total_reward = total_reward + excluded.total_reward,
    avg_reward = (total_reward + excluded.total_reward) / (times_used + 1),
    ucb_score = excluded.ucb_score,
    last_used_at = unixepoch(),
    updated_at = unixepoch()
RETURNING *;

-- name: InsertSkill :one
INSERT INTO skill_library (id, title, content, source_session_id, source_template_id, task_type, usage_count, success_rate, is_active, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 0, 0.0, 1, unixepoch(), unixepoch())
RETURNING *;

-- name: ListActiveSkillsByType :many
SELECT * FROM skill_library
WHERE is_active = 1 AND (task_type = ? OR task_type = 'general')
ORDER BY success_rate DESC, usage_count DESC
LIMIT ?;

-- name: ListAllActiveSkills :many
SELECT * FROM skill_library
WHERE is_active = 1
ORDER BY success_rate DESC, usage_count DESC;

-- name: CountActiveSkills :one
SELECT COUNT(*) FROM skill_library WHERE is_active = 1;

-- name: DeactivateLowestSkill :exec
UPDATE skill_library SET is_active = 0, updated_at = unixepoch()
WHERE id = (
    SELECT id FROM skill_library
    WHERE is_active = 1
    ORDER BY success_rate ASC, usage_count ASC
    LIMIT 1
);

-- name: IncrementSkillUsage :exec
UPDATE skill_library
SET usage_count = usage_count + 1, updated_at = unixepoch()
WHERE id = ?;

-- name: UpdateSkillSuccessRate :exec
-- Called when a session using this skill is evaluated
UPDATE skill_library
SET success_rate = (success_rate * usage_count + ?) / (usage_count + 1),
    usage_count = usage_count + 1,
    updated_at = unixepoch()
WHERE id = ?;

-- name: ListUCBRanking :many
-- For TUI display: all templates with their UCB stats
SELECT
    pt.id, pt.name, pt.section, pt.version, pt.is_default,
    COALESCE(us.times_used, 0) as times_used,
    COALESCE(us.avg_reward, 0.0) as avg_reward,
    COALESCE(us.ucb_score, 9999.0) as ucb_score,
    COALESCE(us.last_used_at, 0) as last_used_at
FROM prompt_templates pt
LEFT JOIN prompt_ucb_stats us ON pt.id = us.template_id
WHERE pt.is_active = 1
ORDER BY us.ucb_score DESC NULLS LAST, pt.name;
```

---

## sqlc.yaml Addition

Add to existing `sqlc.yaml` (or create separate entry):

```yaml
queries:
  - internal/db/sql/self_improvement.sql
```

---

## Repository Pattern (evaluator/repository.go)

Wrap SQLC queries with domain-specific methods:

```go
type Repository struct {
    q *db.Queries
}

// GetTokenBaseline returns the rolling average total tokens for normalization.
func (r *Repository) GetTokenBaseline(ctx context.Context, windowSize int) (float64, error) {
    baseline, err := r.q.GetTokenBaseline(ctx, int64(windowSize))
    if err != nil || !baseline.Valid {
        return 0, nil
    }
    return baseline.Float64, nil
}

// RecordEvaluation saves session_score and updates UCB stats atomically.
func (r *Repository) RecordEvaluation(ctx context.Context, params RecordEvaluationParams) error {
    // Insert session_score (trigger handles UCB update)
    _, err := r.q.InsertSessionScore(ctx, db.InsertSessionScoreParams{...})
    return err
}

// GetBestTemplate returns the template with highest UCB score for a section.
func (r *Repository) GetBestTemplate(ctx context.Context, section string) (*db.ListActiveTemplatesBySectionRow, error) {
    rows, err := r.q.ListActiveTemplatesBySection(ctx, section)
    if err != nil || len(rows) == 0 {
        return nil, err
    }
    return &rows[0], nil // already sorted by ucb_score DESC
}
```

---

## Types (evaluator/types.go)

```go
package evaluator

type PromptTemplate struct {
    ID      string
    Name    string
    Section string
    Content string
    Version int
}

type Skill struct {
    ID          string
    Title       string
    Content     string
    TaskType    string
    SuccessRate float64
    UsageCount  int
}

type Stats struct {
    TotalEvaluations  int
    Templates         []TemplateStats
    SkillCount        int
    TopSkills         []Skill
    AvgReward         float64
    LastEvaluation    time.Time
}

type TemplateStats struct {
    Template  PromptTemplate
    TimesUsed int
    AvgReward float64
    UCBScore  float64
    Rank      int
}
```

**Why:** SQLC ensures compile-time SQL safety (same pattern as existing db/queries). DB trigger handles UCB update atomically to avoid race conditions. Repository layer keeps evaluator business logic clean.

**How to apply:** Run `make sqlc` after creating the SQL file. Check existing `internal/db/queries/` for naming conventions.
