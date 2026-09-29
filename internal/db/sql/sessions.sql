-- name: CreateSession :one
INSERT INTO sessions (
    id,
    parent_session_id,
    title,
    message_count,
    prompt_tokens,
    completion_tokens,
    cost,
    summary_message_id,
    updated_at,
    created_at
) VALUES (
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    null,
    strftime('%s', 'now'),
    strftime('%s', 'now')
) RETURNING *;

-- name: GetSessionByID :one
SELECT *
FROM sessions
WHERE id = ? LIMIT 1;

-- name: ListSessions :many
SELECT *
FROM sessions
WHERE parent_session_id is NULL
  AND message_count > 0
ORDER BY updated_at DESC, created_at DESC;

-- name: ListUnscoredSessions :many
-- Sessions that have no session_scores row yet, at least two user messages and
-- have been idle since before the cutoff (unix seconds), oldest first. Child
-- (subagent) sessions are only returned when include_children is 1.
SELECT s.id
FROM sessions s
WHERE s.message_count > 0
  AND s.updated_at <= ?
  AND (? = 1 OR s.parent_session_id IS NULL)
  AND NOT EXISTS (SELECT 1 FROM session_scores sc WHERE sc.session_id = s.id)
  AND (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') >= 2
ORDER BY s.updated_at ASC, s.created_at ASC
LIMIT ?;

-- name: UpdateSession :one
UPDATE sessions
SET
    title = ?,
    prompt_tokens = ?,
    completion_tokens = ?,
    cache_read_tokens = ?,
    cache_creation_tokens = ?,
    reasoning_tokens = ?,
    summary_message_id = ?,
    cost = ?
WHERE id = ?
RETURNING *;


-- name: DeleteSession :exec
DELETE FROM sessions
WHERE id = ?;
