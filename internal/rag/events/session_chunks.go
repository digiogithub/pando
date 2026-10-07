package events

import (
	"context"
	"fmt"
	"strings"
)

// StoredChunk is one indexed chunk of a session conversation.
type StoredChunk struct {
	Content   string
	Embedding []float32
}

// SessionChunks returns the chunks currently indexed for a session, in chunk
// order, with their embeddings. The session indexer uses it to embed only the
// chunks that changed since the last run and to skip the write entirely when
// nothing changed. Reads go to the local connection, so it works on secondary
// instances too.
func (s *EventStore) SessionChunks(ctx context.Context, subject, sessionID string) ([]StoredChunk, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("events: session_id cannot be empty")
	}
	if strings.TrimSpace(subject) == "" {
		subject = "session"
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT content, embedding
		FROM events
		WHERE subject = ?
		  AND json_extract(metadata, '$.session_id') = ?
		ORDER BY CAST(json_extract(metadata, '$.chunk_index') AS INTEGER), id`,
		subject, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("events: query session chunks: %w", err)
	}
	defer rows.Close()

	var out []StoredChunk
	for rows.Next() {
		var content string
		var blob []byte
		if err := rows.Scan(&content, &blob); err != nil {
			return nil, fmt.Errorf("events: scan session chunk: %w", err)
		}
		out = append(out, StoredChunk{Content: content, Embedding: deserializeFloat32(blob)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("events: iterate session chunks: %w", err)
	}
	return out, nil
}
