package mcpgateway

import (
	"context"
	"database/sql"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// Stats manages usage statistics and favorite computation.
type Stats struct {
	db     *sql.DB
	config FavoriteConfig
}

// NewStats creates a new Stats instance with the given database and configuration.
func NewStats(db *sql.DB, cfg FavoriteConfig) *Stats {
	return &Stats{db: db, config: cfg}
}

// RecordUsage inserts a single invocation record into mcp_tool_usage_stats.
func (s *Stats) RecordUsage(ctx context.Context, toolID, sessionID string, durationMs int64, success bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_tool_usage_stats (tool_id, session_id, called_at, duration_ms, success)
		VALUES (?, ?, ?, ?, ?)
	`, toolID, sessionID, time.Now().UTC(), durationMs, success)
	return err
}

// GetTopTools returns the tool IDs ordered by call count within the configured window.
func (s *Stats) GetTopTools(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		limit = s.config.MaxFavorites
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -s.config.WindowDays)
	rows, err := s.db.QueryContext(ctx, `
		SELECT tool_id, COUNT(*) as calls
		FROM mcp_tool_usage_stats
		WHERE called_at > ?
		GROUP BY tool_id
		ORDER BY calls DESC
		LIMIT ?
	`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		var calls int
		if err := rows.Scan(&id, &calls); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetFavorites returns tools that meet the favorite criteria:
//   - called at least Threshold times in the last WindowDays days
//   - active within the last DecayDays days
//   - capped at MaxFavorites entries
//
// It joins with mcp_tool_registry to return full RegisteredTool records.
func (s *Stats) GetFavorites(ctx context.Context) ([]RegisteredTool, error) {
	windowCutoff := time.Now().UTC().AddDate(0, 0, -s.config.WindowDays)
	decayCutoff := time.Now().UTC().AddDate(0, 0, -s.config.DecayDays)

	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.server_name, r.tool_name, r.description, r.input_schema, r.last_discovered
		FROM mcp_tool_registry r
		INNER JOIN (
			SELECT tool_id,
			       COUNT(*) as total_calls,
			       MAX(called_at) as last_call
			FROM mcp_tool_usage_stats
			WHERE called_at > ?
			GROUP BY tool_id
			HAVING total_calls >= ?
		) stats ON stats.tool_id = r.id
		WHERE stats.last_call > ?
		ORDER BY stats.total_calls DESC
		LIMIT ?
	`, windowCutoff, s.config.Threshold, decayCutoff, s.config.MaxFavorites)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result, err := scanTools(rows)
	if err != nil {
		logging.Error("MCP gateway: error scanning favorites", "error", err)
	}
	return result, err
}
