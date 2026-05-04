-- name: CreateProject :one
INSERT INTO projects (
    id,
    name,
    path,
    status,
    initialized,
    acp_pid,
    acp_port,
    last_opened,
    created_at,
    updated_at
) VALUES (
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    ?,
    strftime('%s', 'now'),
    strftime('%s', 'now')
) RETURNING *;

-- name: GetProject :one
SELECT *
FROM projects
WHERE id = ? LIMIT 1;

-- name: GetProjectByPath :one
SELECT *
FROM projects
WHERE path = ? LIMIT 1;

-- name: ListProjects :many
SELECT *
FROM projects
ORDER BY last_opened DESC NULLS LAST, created_at DESC;

-- name: UpdateProjectStatus :exec
UPDATE projects
SET
    status     = ?,
    acp_pid    = ?,
    acp_port   = ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;

-- name: UpdateProjectLastOpened :exec
UPDATE projects
SET
    last_opened = ?,
    updated_at  = strftime('%s', 'now')
WHERE id = ?;

-- name: MarkProjectInitialized :exec
UPDATE projects
SET
    initialized = 1,
    updated_at  = strftime('%s', 'now')
WHERE id = ?;

-- name: UpdateProjectName :exec
UPDATE projects
SET
    name       = ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;

-- name: DeleteProject :exec
DELETE FROM projects
WHERE id = ?;
