// Hand-written in the sqlc style (sqlc is not part of the toolchain here).
// source: evaluator_observability.sql

package db

import (
	"context"
	"database/sql"
)

const countEligibleSessions = `-- name: CountEligibleSessions :one
SELECT COUNT(*)
FROM sessions s
WHERE s.message_count > 0
  AND (? = 1 OR s.parent_session_id IS NULL)
  AND (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') >= 2
`

func (q *Queries) CountEligibleSessions(ctx context.Context, includeChildren int64) (int64, error) {
	row := q.queryRow(ctx, nil, countEligibleSessions, includeChildren)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const countUnscoredEligibleSessions = `-- name: CountUnscoredEligibleSessions :one
SELECT COUNT(*)
FROM sessions s
WHERE s.message_count > 0
  AND (? = 1 OR s.parent_session_id IS NULL)
  AND NOT EXISTS (SELECT 1 FROM session_scores sc WHERE sc.session_id = s.id)
  AND (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') >= 2
`

func (q *Queries) CountUnscoredEligibleSessions(ctx context.Context, includeChildren int64) (int64, error) {
	row := q.queryRow(ctx, nil, countUnscoredEligibleSessions, includeChildren)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const countRecentEligibleScored = `-- name: CountRecentEligibleScored :one
SELECT COUNT(*) as total, CAST(COALESCE(SUM(scored), 0) AS INTEGER) as scored
FROM (
    SELECT EXISTS (SELECT 1 FROM session_scores sc WHERE sc.session_id = s.id) as scored
    FROM sessions s
    WHERE s.message_count > 0
      AND s.updated_at <= ?
      AND (? = 1 OR s.parent_session_id IS NULL)
      AND (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') >= 2
    ORDER BY s.updated_at DESC
    LIMIT ?
)
`

type CountRecentEligibleScoredParams struct {
	UpdatedAt       int64 `json:"updated_at"`
	IncludeChildren int64 `json:"include_children"`
	Limit           int64 `json:"limit"`
}

type CountRecentEligibleScoredRow struct {
	Total  int64 `json:"total"`
	Scored int64 `json:"scored"`
}

func (q *Queries) CountRecentEligibleScored(ctx context.Context, arg CountRecentEligibleScoredParams) (CountRecentEligibleScoredRow, error) {
	row := q.queryRow(ctx, nil, countRecentEligibleScored, arg.UpdatedAt, arg.IncludeChildren, arg.Limit)
	var i CountRecentEligibleScoredRow
	err := row.Scan(&i.Total, &i.Scored)
	return i, err
}

const getLastEvaluationAt = `-- name: GetLastEvaluationAt :one
SELECT CAST(COALESCE(MAX(evaluated_at), 0) AS INTEGER) FROM session_scores
`

func (q *Queries) GetLastEvaluationAt(ctx context.Context) (int64, error) {
	row := q.queryRow(ctx, nil, getLastEvaluationAt)
	var at int64
	err := row.Scan(&at)
	return at, err
}

const listSessionScoresWithTitle = `-- name: ListSessionScoresWithTitle :many
SELECT sc.id, sc.session_id, sc.template_id, sc.reward, sc.success_score, sc.efficiency_score,
       sc.judge_analysis, sc.judge_model, sc.prompt_tokens, sc.completion_tokens,
       sc.message_count, sc.user_corrections, sc.evaluated_at, sc.created_at, sc.components,
       sc.judge_prompt_tokens, sc.judge_completion_tokens,
       COALESCE(s.title, '') as title
FROM session_scores sc
LEFT JOIN sessions s ON s.id = sc.session_id
ORDER BY sc.created_at DESC
LIMIT ?
`

type ListSessionScoresWithTitleRow struct {
	ID                    string         `json:"id"`
	SessionID             string         `json:"session_id"`
	TemplateID            sql.NullString `json:"template_id"`
	Reward                float64        `json:"reward"`
	SuccessScore          float64        `json:"success_score"`
	EfficiencyScore       float64        `json:"efficiency_score"`
	JudgeAnalysis         sql.NullString `json:"judge_analysis"`
	JudgeModel            sql.NullString `json:"judge_model"`
	PromptTokens          int64          `json:"prompt_tokens"`
	CompletionTokens      int64          `json:"completion_tokens"`
	MessageCount          int64          `json:"message_count"`
	UserCorrections       int64          `json:"user_corrections"`
	EvaluatedAt           int64          `json:"evaluated_at"`
	CreatedAt             int64          `json:"created_at"`
	Components            string         `json:"components"`
	JudgePromptTokens     int64          `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int64          `json:"judge_completion_tokens"`
	Title                 string         `json:"title"`
}

func (q *Queries) ListSessionScoresWithTitle(ctx context.Context, limit int64) ([]ListSessionScoresWithTitleRow, error) {
	rows, err := q.query(ctx, nil, listSessionScoresWithTitle, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ListSessionScoresWithTitleRow{}
	for rows.Next() {
		var i ListSessionScoresWithTitleRow
		if err := rows.Scan(
			&i.ID, &i.SessionID, &i.TemplateID, &i.Reward, &i.SuccessScore, &i.EfficiencyScore,
			&i.JudgeAnalysis, &i.JudgeModel, &i.PromptTokens, &i.CompletionTokens,
			&i.MessageCount, &i.UserCorrections, &i.EvaluatedAt, &i.CreatedAt, &i.Components,
			&i.JudgePromptTokens, &i.JudgeCompletionTokens, &i.Title,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listRecentSessionVariants = `-- name: ListRecentSessionVariants :many
SELECT sel.session_id, sel.section, sel.variant_id
FROM session_template_selections sel
WHERE sel.session_id IN (SELECT session_id FROM session_scores ORDER BY created_at DESC LIMIT ?)
ORDER BY sel.session_id, sel.section
`

type ListRecentSessionVariantsRow struct {
	SessionID string `json:"session_id"`
	Section   string `json:"section"`
	VariantID string `json:"variant_id"`
}

func (q *Queries) ListRecentSessionVariants(ctx context.Context, limit int64) ([]ListRecentSessionVariantsRow, error) {
	rows, err := q.query(ctx, nil, listRecentSessionVariants, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ListRecentSessionVariantsRow{}
	for rows.Next() {
		var i ListRecentSessionVariantsRow
		if err := rows.Scan(&i.SessionID, &i.Section, &i.VariantID); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listRecentSessionSkills = `-- name: ListRecentSessionSkills :many
SELECT i.session_id, i.skill_id, COALESCE(k.title, '') as title
FROM session_skill_injections i
LEFT JOIN skill_library k ON k.id = i.skill_id
WHERE i.session_id IN (SELECT session_id FROM session_scores ORDER BY created_at DESC LIMIT ?)
ORDER BY i.session_id, i.position
`

type ListRecentSessionSkillsRow struct {
	SessionID string `json:"session_id"`
	SkillID   string `json:"skill_id"`
	Title     string `json:"title"`
}

func (q *Queries) ListRecentSessionSkills(ctx context.Context, limit int64) ([]ListRecentSessionSkillsRow, error) {
	rows, err := q.query(ctx, nil, listRecentSessionSkills, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ListRecentSessionSkillsRow{}
	for rows.Next() {
		var i ListRecentSessionSkillsRow
		if err := rows.Scan(&i.SessionID, &i.SkillID, &i.Title); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const listDailyEvaluationMetrics = `-- name: ListDailyEvaluationMetrics :many
SELECT
    date(evaluated_at, 'unixepoch', 'localtime') as day,
    COUNT(*) as evaluations,
    CAST(COALESCE(AVG(reward), 0) AS REAL) as avg_reward,
    CAST(COALESCE(SUM(CASE WHEN judge_model IS NOT NULL THEN 1 ELSE 0 END), 0) AS INTEGER) as judge_calls,
    CAST(COALESCE(SUM(judge_prompt_tokens), 0) AS INTEGER) as judge_prompt_tokens,
    CAST(COALESCE(SUM(judge_completion_tokens), 0) AS INTEGER) as judge_completion_tokens
FROM session_scores
WHERE evaluated_at >= ?
GROUP BY day
ORDER BY day
`

type ListDailyEvaluationMetricsRow struct {
	Day                   string  `json:"day"`
	Evaluations           int64   `json:"evaluations"`
	AvgReward             float64 `json:"avg_reward"`
	JudgeCalls            int64   `json:"judge_calls"`
	JudgePromptTokens     int64   `json:"judge_prompt_tokens"`
	JudgeCompletionTokens int64   `json:"judge_completion_tokens"`
}

func (q *Queries) ListDailyEvaluationMetrics(ctx context.Context, evaluatedAt int64) ([]ListDailyEvaluationMetricsRow, error) {
	rows, err := q.query(ctx, nil, listDailyEvaluationMetrics, evaluatedAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ListDailyEvaluationMetricsRow{}
	for rows.Next() {
		var i ListDailyEvaluationMetricsRow
		if err := rows.Scan(&i.Day, &i.Evaluations, &i.AvgReward, &i.JudgeCalls, &i.JudgePromptTokens, &i.JudgeCompletionTokens); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
