-- name: CreateGoal :one
INSERT INTO session_goals (
    id,
    session_id,
    objective,
    status,
    max_iterations,
    max_duration_seconds,
    started_at
) VALUES (
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?
) RETURNING
    id,
    session_id,
    objective,
    status,
    iteration,
    max_iterations,
    max_duration_seconds,
    started_at,
    completed_at,
    last_progress,
    next_step,
    blocked_reason,
    created_at;

-- name: GetGoalBySession :one
SELECT
    id,
    session_id,
    objective,
    status,
    iteration,
    max_iterations,
    max_duration_seconds,
    started_at,
    completed_at,
    last_progress,
    next_step,
    blocked_reason,
    created_at
FROM session_goals
WHERE session_id = ?
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateGoalStatus :one
UPDATE session_goals
SET
    status = ?,
    iteration = ?,
    last_progress = ?,
    next_step = ?,
    blocked_reason = ?,
    completed_at = ?
WHERE id = ?
RETURNING
    id,
    session_id,
    objective,
    status,
    iteration,
    max_iterations,
    max_duration_seconds,
    started_at,
    completed_at,
    last_progress,
    next_step,
    blocked_reason,
    created_at;

-- name: UpdateGoalProgress :one
UPDATE session_goals
SET
    last_progress = ?,
    next_step = ?,
    iteration = ?
WHERE id = ?
RETURNING
    id,
    session_id,
    objective,
    status,
    iteration,
    max_iterations,
    max_duration_seconds,
    started_at,
    completed_at,
    last_progress,
    next_step,
    blocked_reason,
    created_at;

-- name: DeleteGoalsBySession :exec
DELETE FROM session_goals
WHERE session_id = ?;

-- name: GetActiveGoal :one
SELECT
    id,
    session_id,
    objective,
    status,
    iteration,
    max_iterations,
    max_duration_seconds,
    started_at,
    completed_at,
    last_progress,
    next_step,
    blocked_reason,
    created_at
FROM session_goals
WHERE session_id = ?
  AND status = 'running'
ORDER BY created_at DESC
LIMIT 1;
