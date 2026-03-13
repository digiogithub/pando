package mcpgateway_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/mcpgateway"
)

// createTestDB opens an in-memory SQLite database and creates the MCP gateway tables.
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
CREATE TABLE IF NOT EXISTS mcp_tool_registry (
    id TEXT PRIMARY KEY,
    server_name TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    description TEXT,
    input_schema TEXT,
    last_discovered TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(server_name, tool_name)
);

CREATE TABLE IF NOT EXISTS mcp_tool_usage_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_id TEXT NOT NULL,
    session_id TEXT,
    called_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    duration_ms INTEGER,
    success BOOLEAN DEFAULT TRUE,
    FOREIGN KEY(tool_id) REFERENCES mcp_tool_registry(id) ON DELETE CASCADE
);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

// insertTool inserts a tool into the registry for test setup.
func insertTool(t *testing.T, db *sql.DB, id, serverName, toolName string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO mcp_tool_registry (id, server_name, tool_name, description, input_schema, last_discovered)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, serverName, toolName, "test tool", "{}", time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("insert tool %q: %v", id, err)
	}
}

func TestStats_RecordUsage(t *testing.T) {
	db := createTestDB(t)
	insertTool(t, db, "srvA/toolX", "srvA", "toolX")

	cfg := mcpgateway.FavoriteConfig{
		Threshold:    1,
		MaxFavorites: 10,
		WindowDays:   30,
		DecayDays:    14,
	}
	stats := mcpgateway.NewStats(db, cfg)
	ctx := context.Background()

	if err := stats.RecordUsage(ctx, "srvA/toolX", "session-1", 42, true); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM mcp_tool_usage_stats WHERE tool_id = 'srvA/toolX'").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 usage record, got %d", count)
	}
}

func TestStats_GetTopTools(t *testing.T) {
	db := createTestDB(t)
	insertTool(t, db, "srvA/toolX", "srvA", "toolX")
	insertTool(t, db, "srvA/toolY", "srvA", "toolY")

	cfg := mcpgateway.FavoriteConfig{
		Threshold:    1,
		MaxFavorites: 10,
		WindowDays:   30,
		DecayDays:    14,
	}
	stats := mcpgateway.NewStats(db, cfg)
	ctx := context.Background()

	// Record 3 calls for toolX, 1 for toolY.
	for i := 0; i < 3; i++ {
		_ = stats.RecordUsage(ctx, "srvA/toolX", "s1", 10, true)
	}
	_ = stats.RecordUsage(ctx, "srvA/toolY", "s1", 10, true)

	top, err := stats.GetTopTools(ctx, 5)
	if err != nil {
		t.Fatalf("GetTopTools: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("expected 2 top tools, got %d", len(top))
	}
	if top[0] != "srvA/toolX" {
		t.Errorf("expected toolX to be top, got %q", top[0])
	}
}

func TestStats_GetFavorites(t *testing.T) {
	db := createTestDB(t)
	insertTool(t, db, "srvA/toolX", "srvA", "toolX")
	insertTool(t, db, "srvA/toolY", "srvA", "toolY")

	cfg := mcpgateway.FavoriteConfig{
		Threshold:    3,  // need at least 3 calls
		MaxFavorites: 10,
		WindowDays:   30,
		DecayDays:    14,
	}
	stats := mcpgateway.NewStats(db, cfg)
	ctx := context.Background()

	// toolX gets 5 calls (above threshold), toolY gets 1 (below threshold).
	for i := 0; i < 5; i++ {
		_ = stats.RecordUsage(ctx, "srvA/toolX", "s1", 10, true)
	}
	_ = stats.RecordUsage(ctx, "srvA/toolY", "s1", 10, true)

	favorites, err := stats.GetFavorites(ctx)
	if err != nil {
		t.Fatalf("GetFavorites: %v", err)
	}
	if len(favorites) != 1 {
		t.Fatalf("expected 1 favorite, got %d", len(favorites))
	}
	if favorites[0].ID != "srvA/toolX" {
		t.Errorf("expected srvA/toolX as favorite, got %q", favorites[0].ID)
	}
}

func TestStats_GetFavorites_Empty(t *testing.T) {
	db := createTestDB(t)

	cfg := mcpgateway.DefaultFavoriteConfig()
	stats := mcpgateway.NewStats(db, cfg)

	favorites, err := stats.GetFavorites(context.Background())
	if err != nil {
		t.Fatalf("GetFavorites on empty DB: %v", err)
	}
	if len(favorites) != 0 {
		t.Errorf("expected 0 favorites on empty DB, got %d", len(favorites))
	}
}

func TestStats_GetFavorites_DecayEviction(t *testing.T) {
	db := createTestDB(t)
	insertTool(t, db, "srvA/stale", "srvA", "stale")

	cfg := mcpgateway.FavoriteConfig{
		Threshold:    2,
		MaxFavorites: 10,
		WindowDays:   30,
		DecayDays:    7, // must have been used in last 7 days
	}
	stats := mcpgateway.NewStats(db, cfg)

	// Insert old usage records (older than DecayDays).
	oldTime := time.Now().UTC().AddDate(0, 0, -8).Format("2006-01-02 15:04:05")
	for i := 0; i < 5; i++ {
		_, _ = db.Exec(
			`INSERT INTO mcp_tool_usage_stats (tool_id, session_id, called_at, duration_ms, success)
			 VALUES ('srvA/stale', 's1', ?, 10, 1)`, oldTime,
		)
	}

	favorites, err := stats.GetFavorites(context.Background())
	if err != nil {
		t.Fatalf("GetFavorites: %v", err)
	}
	if len(favorites) != 0 {
		t.Errorf("stale tool should not appear in favorites, got %d", len(favorites))
	}
}
