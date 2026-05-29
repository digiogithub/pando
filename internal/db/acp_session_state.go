package db

import (
	"context"
	"database/sql"
)

const getSessionACPState = `-- name: GetSessionACPState :one
SELECT acp_session_state
FROM sessions
WHERE id = ? LIMIT 1
`

func (q *Queries) GetSessionACPState(ctx context.Context, id string) (sql.NullString, error) {
	row := q.queryRow(ctx, nil, getSessionACPState, id)
	var state sql.NullString
	err := row.Scan(&state)
	return state, err
}

const updateSessionACPState = `-- name: UpdateSessionACPState :exec
UPDATE sessions
SET acp_session_state = ?
WHERE id = ?
`

type UpdateSessionACPStateParams struct {
	ACPState sql.NullString `json:"acp_state"`
	ID       string         `json:"id"`
}

func (q *Queries) UpdateSessionACPState(ctx context.Context, arg UpdateSessionACPStateParams) error {
	_, err := q.exec(ctx, nil, updateSessionACPState, arg.ACPState, arg.ID)
	return err
}
