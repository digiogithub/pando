-- Read-only queries behind `pando evaluator doctor` and the evaluator
-- observability surfaces (TUI page, WebUI view, /api/v1/evaluator/*).

-- Sessions the evaluator can score: at least two user turns and, unless
-- include_children is 1, root sessions only.
-- name: CountEligibleSessions :one
SELECT COUNT(*)
FROM sessions s
WHERE s.message_count > 0
  AND (? = 1 OR s.parent_session_id IS NULL)
  AND (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') >= 2;

-- name: CountUnscoredEligibleSessions :one
SELECT COUNT(*)
FROM sessions s
WHERE s.message_count > 0
  AND (? = 1 OR s.parent_session_id IS NULL)
  AND NOT EXISTS (SELECT 1 FROM session_scores sc WHERE sc.session_id = s.id)
  AND (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') >= 2;

-- Of the most recently updated eligible sessions idle since before the cutoff,
-- how many were looked at and how many have a score.
-- name: CountRecentEligibleScored :one
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
);

-- name: GetLastEvaluationAt :one
SELECT CAST(COALESCE(MAX(evaluated_at), 0) AS INTEGER) FROM session_scores;

-- name: ListSessionScoresWithTitle :many
SELECT sc.id, sc.session_id, sc.template_id, sc.reward, sc.success_score, sc.efficiency_score,
       sc.judge_analysis, sc.judge_model, sc.prompt_tokens, sc.completion_tokens,
       sc.message_count, sc.user_corrections, sc.evaluated_at, sc.created_at, sc.components,
       sc.judge_prompt_tokens, sc.judge_completion_tokens,
       COALESCE(s.title, '') as title
FROM session_scores sc
LEFT JOIN sessions s ON s.id = sc.session_id
ORDER BY sc.created_at DESC
LIMIT ?;

-- Variants served to the most recently scored sessions.
-- name: ListRecentSessionVariants :many
SELECT sel.session_id, sel.section, sel.variant_id
FROM session_template_selections sel
WHERE sel.session_id IN (SELECT session_id FROM session_scores ORDER BY created_at DESC LIMIT ?)
ORDER BY sel.session_id, sel.section;

-- Skills injected into the most recently scored sessions.
-- name: ListRecentSessionSkills :many
SELECT i.session_id, i.skill_id, COALESCE(k.title, '') as title
FROM session_skill_injections i
LEFT JOIN skill_library k ON k.id = i.skill_id
WHERE i.session_id IN (SELECT session_id FROM session_scores ORDER BY created_at DESC LIMIT ?)
ORDER BY i.session_id, i.position;

-- Evaluations, mean reward and judge usage per local day since a unix time.
-- name: ListDailyEvaluationMetrics :many
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
ORDER BY day;
