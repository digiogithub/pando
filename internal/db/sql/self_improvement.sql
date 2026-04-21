-- name: InsertPromptTemplate :one
INSERT INTO prompt_templates (id, name, section, content, version, is_active, is_default, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now'))
RETURNING *;

-- name: GetPromptTemplate :one
SELECT * FROM prompt_templates WHERE id = ? LIMIT 1;

-- name: ListActiveTemplatesBySection :many
SELECT
    pt.id,
    pt.name,
    pt.section,
    pt.content,
    pt.version,
    pt.is_active,
    pt.is_default,
    pt.created_at,
    pt.updated_at,
    COALESCE(us.avg_reward, 0.0) as avg_reward,
    COALESCE(us.times_used, 0) as times_used,
    COALESCE(us.ucb_score, 9999.0) as ucb_score
FROM prompt_templates pt
LEFT JOIN prompt_ucb_stats us ON pt.id = us.template_id
WHERE pt.section = ? AND pt.is_active = 1
ORDER BY us.ucb_score DESC, pt.name;

-- name: CountPromptTemplates :one
SELECT COUNT(*) FROM prompt_templates WHERE is_active = 1;

-- name: InsertSessionScore :one
INSERT INTO session_scores (
    id, session_id, template_id, reward, success_score, efficiency_score,
    judge_analysis, judge_model, prompt_tokens, completion_tokens,
    message_count, user_corrections, evaluated_at, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now'))
RETURNING *;

-- name: GetSessionScore :one
SELECT * FROM session_scores WHERE session_id = ? LIMIT 1;

-- name: CountSessionScores :one
SELECT COUNT(*) FROM session_scores;

-- name: ListSessionScores :many
SELECT id, session_id, template_id, reward, success_score, efficiency_score,
       judge_analysis, judge_model, prompt_tokens, completion_tokens,
       message_count, user_corrections, evaluated_at, created_at
FROM session_scores
ORDER BY created_at DESC
LIMIT ?;

-- name: GetTokenBaseline :one
SELECT COALESCE(AVG(prompt_tokens + completion_tokens), 0) as baseline
FROM (
    SELECT prompt_tokens, completion_tokens
    FROM session_scores
    ORDER BY created_at DESC
    LIMIT ?
);

-- name: GetUCBStats :one
SELECT * FROM prompt_ucb_stats WHERE template_id = ?;

-- name: InsertSkill :one
INSERT INTO skill_library (
    id, title, content, source_session_id, source_template_id,
    task_type, usage_count, success_rate, is_active, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, 0, 0.0, 1, strftime('%s', 'now'), strftime('%s', 'now'))
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
UPDATE skill_library SET is_active = 0, updated_at = strftime('%s', 'now')
WHERE id = (
    SELECT id FROM skill_library
    WHERE is_active = 1
    ORDER BY success_rate ASC, usage_count ASC
    LIMIT 1
);

-- name: IncrementSkillUsage :exec
UPDATE skill_library
SET usage_count = usage_count + 1, updated_at = strftime('%s', 'now')
WHERE id = ?;

-- name: ListUCBRanking :many
SELECT
    pt.id,
    pt.name,
    pt.section,
    pt.version,
    pt.is_default,
    COALESCE(us.times_used, 0) as times_used,
    COALESCE(us.avg_reward, 0.0) as avg_reward,
    COALESCE(us.ucb_score, 9999.0) as ucb_score,
    COALESCE(us.last_used_at, 0) as last_used_at
FROM prompt_templates pt
LEFT JOIN prompt_ucb_stats us ON pt.id = us.template_id
WHERE pt.is_active = 1
ORDER BY us.ucb_score DESC, pt.name;

-- name: GetEvaluatorStats :one
SELECT
    (SELECT COUNT(*) FROM session_scores) as total_evaluations,
    (SELECT COALESCE(AVG(reward), 0.0) FROM session_scores) as avg_reward,
    (SELECT COUNT(*) FROM skill_library WHERE is_active = 1) as active_skills,
    (SELECT MAX(created_at) FROM session_scores) as last_evaluation;
