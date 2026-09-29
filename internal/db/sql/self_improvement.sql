-- name: InsertSessionScore :one
INSERT INTO session_scores (
    id, session_id, template_id, reward, success_score, efficiency_score,
    judge_analysis, judge_model, prompt_tokens, completion_tokens,
    message_count, user_corrections, components, judge_prompt_tokens, judge_completion_tokens,
    evaluated_at, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now'))
RETURNING *;

-- name: GetSessionScore :one
SELECT * FROM session_scores WHERE session_id = ? LIMIT 1;

-- name: CountSessionScores :one
SELECT COUNT(*) FROM session_scores;

-- name: ListSessionScores :many
SELECT id, session_id, template_id, reward, success_score, efficiency_score,
       judge_analysis, judge_model, prompt_tokens, completion_tokens,
       message_count, user_corrections, evaluated_at, created_at, components, judge_prompt_tokens, judge_completion_tokens
FROM session_scores
ORDER BY created_at DESC
LIMIT ?;

-- Re-scores a session in place (explicit feedback). The row is UPDATEd, never
-- re-inserted, so the insert-only UCB trigger cannot double count it; the
-- update_ucb_after_rescore trigger applies just the reward delta.
-- name: UpdateSessionScore :one
UPDATE session_scores
SET reward = ?,
    success_score = ?,
    efficiency_score = ?,
    prompt_tokens = ?,
    completion_tokens = ?,
    message_count = ?,
    user_corrections = ?,
    components = ?,
    evaluated_at = strftime('%s', 'now')
WHERE session_id = ?
RETURNING *;

-- Token baseline from the sessions table itself (not session_scores), so it is
-- available before any session has been scored: mean prompt+completion tokens
-- of the most recent root sessions with at least 4 messages (>= 2 user turns
-- and their replies), excluding the session being evaluated.
-- name: GetSessionsTokenBaseline :one
SELECT COALESCE(AVG(prompt_tokens + completion_tokens), 0) as baseline
FROM (
    SELECT prompt_tokens, completion_tokens
    FROM sessions
    WHERE id != ?
      AND parent_session_id IS NULL
      AND message_count >= 4
      AND prompt_tokens + completion_tokens > 0
    ORDER BY created_at DESC
    LIMIT ?
);

-- Explicit user feedback (/feedback good|bad) is stored as an event whose
-- subject is "session_feedback:<session id>"; the payload lives in metadata.
-- name: InsertSessionFeedbackEvent :one
INSERT INTO events (subject, content, metadata)
VALUES (?, ?, ?)
RETURNING id;

-- name: GetLatestSessionFeedback :one
SELECT metadata FROM events WHERE subject = ? ORDER BY id DESC LIMIT 1;

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

-- name: IncrementSkillUsage :exec
UPDATE skill_library
SET usage_count = usage_count + 1, updated_at = strftime('%s', 'now')
WHERE id = ?;

-- name: GetEvaluatorStats :one
SELECT
    (SELECT COUNT(*) FROM session_scores) as total_evaluations,
    (SELECT COALESCE(AVG(reward), 0.0) FROM session_scores) as avg_reward,
    (SELECT COUNT(*) FROM skill_library WHERE is_active = 1) as active_skills,
    (SELECT MAX(created_at) FROM session_scores) as last_evaluation;

-- Stores the LLM judge output on an existing score. It does not touch reward,
-- so neither UCB trigger fires.
-- name: UpdateSessionScoreJudge :exec
UPDATE session_scores
SET judge_analysis = ?,
    judge_model = ?,
    judge_prompt_tokens = ?,
    judge_completion_tokens = ?
WHERE session_id = ?;

-- Judge calls and tokens spent since a unix timestamp (start of the local day),
-- derived from persisted scores so the daily budget survives restarts.
-- name: GetJudgeUsageSince :one
SELECT
    COUNT(*) as calls,
    CAST(COALESCE(SUM(judge_prompt_tokens + judge_completion_tokens), 0) AS INTEGER) as tokens
FROM session_scores
WHERE judge_model IS NOT NULL AND created_at >= ?;

-- Prompt template variants: the DB holds selections and statistics only; the
-- variant sources are files. variant_id is "<section>#<variant>".

-- First writer wins, so concurrent processes agree on the frozen choice.
-- name: InsertSessionTemplateSelection :exec
INSERT OR IGNORE INTO session_template_selections (session_id, section, variant_id, selected_at)
VALUES (?, ?, ?, strftime('%s', 'now'));

-- name: GetSessionTemplateSelection :one
SELECT variant_id FROM session_template_selections WHERE session_id = ? AND section = ?;

-- name: ListVariantSelectionCounts :many
SELECT variant_id, COUNT(*) as selections
FROM session_template_selections
WHERE section = ?
GROUP BY variant_id;

-- name: ListVariantStatsBySection :many
SELECT variant_id, section, times_used, total_reward, avg_reward, updated_at
FROM prompt_variant_stats
WHERE section = ?;

-- name: ListAllVariantStats :many
SELECT variant_id, section, times_used, total_reward, avg_reward, updated_at
FROM prompt_variant_stats
ORDER BY section, variant_id;

-- Counts an evaluated session once for every variant it was served: one
-- statement, joined to the session's selections.
-- name: ApplySessionRewardToVariantStats :exec
INSERT INTO prompt_variant_stats (variant_id, section, times_used, total_reward, avg_reward, updated_at)
SELECT variant_id, section, 1, ?, ?, unixepoch()
FROM session_template_selections
WHERE session_id = ?
ON CONFLICT(variant_id) DO UPDATE SET
    times_used = times_used + 1,
    total_reward = total_reward + excluded.total_reward,
    avg_reward = (total_reward + excluded.total_reward) / (times_used + 1),
    updated_at = unixepoch();

-- A re-score applies only the reward delta and never touches times_used.
-- name: ApplyRewardDeltaToVariantStats :exec
UPDATE prompt_variant_stats
SET total_reward = total_reward + ?,
    avg_reward = (total_reward + ?) / MAX(times_used, 1),
    updated_at = unixepoch()
WHERE variant_id IN (SELECT variant_id FROM session_template_selections WHERE session_id = ?);

-- Reviewable learned skills: files under .pando/skills/learned are the source
-- of truth; skill_library mirrors their status and keeps the statistics.

-- Mirrors a skill file. Statistics columns are never touched by the mirror.
-- name: UpsertSkillMirror :exec
INSERT INTO skill_library (
    id, title, content, source_session_id, task_type, is_active,
    status, confidence, judge_model, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'))
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    content = excluded.content,
    source_session_id = excluded.source_session_id,
    task_type = excluded.task_type,
    is_active = excluded.is_active,
    status = excluded.status,
    confidence = excluded.confidence,
    judge_model = excluded.judge_model,
    updated_at = strftime('%s', 'now');

-- name: SetSkillMirrorState :exec
UPDATE skill_library SET status = ?, is_active = ?, updated_at = strftime('%s', 'now') WHERE id = ?;

-- name: ListAllSkills :many
SELECT * FROM skill_library ORDER BY created_at DESC, id;

-- name: GetSkill :one
SELECT * FROM skill_library WHERE id = ?;

-- First writer wins so concurrent processes agree on the frozen set.
-- name: InsertSessionSkillInjection :exec
INSERT OR IGNORE INTO session_skill_injections (session_id, skill_id, position, injected_at)
VALUES (?, ?, ?, strftime('%s', 'now'));

-- name: CountSessionSkillInjections :one
SELECT COUNT(*) FROM session_skill_injections WHERE session_id = ?;

-- name: ListSessionInjectedSkills :many
SELECT s.* FROM session_skill_injections i
JOIN skill_library s ON s.id = i.skill_id
WHERE i.session_id = ?
ORDER BY i.position, s.id;

-- Adds an evaluated session's reward to every skill injected into it.
-- name: ApplySessionRewardToSkillStats :exec
UPDATE skill_library
SET eval_count = eval_count + 1,
    reward_total = reward_total + ?,
    success_rate = (reward_total + ?) / (eval_count + 1),
    updated_at = strftime('%s', 'now')
WHERE id IN (SELECT skill_id FROM session_skill_injections WHERE session_id = ?);

-- A re-score applies only the reward delta and never touches eval_count.
-- name: ApplyRewardDeltaToSkillStats :exec
UPDATE skill_library
SET reward_total = reward_total + ?,
    success_rate = (reward_total + ?) / MAX(eval_count, 1),
    updated_at = strftime('%s', 'now')
WHERE id IN (SELECT skill_id FROM session_skill_injections WHERE session_id = ?);
