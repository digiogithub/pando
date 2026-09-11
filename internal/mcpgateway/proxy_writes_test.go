package mcpgateway

import (
	"context"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	"github.com/digiogithub/pando/internal/ipc/dbproxy/proxytest"
)

func countRows(t *testing.T, tp *proxytest.Topology, query string, args ...any) int {
	t.Helper()
	var n int
	if err := tp.PrimaryDB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// On an IPC secondary the gateway's catalog and usage writes are forwarded to
// the primary when the secondary's pool cannot get the write lock in time.
func TestGatewayWritesThroughProxyOnSecondary(t *testing.T) {
	tp := proxytest.New(t)
	ctx := context.Background()
	gw := NewGateway(tp.SecondaryDB, FavoriteConfig{Threshold: 1, MaxFavorites: 5, WindowDays: 7, DecayDays: 7})
	gw.SetWriteProxy(tp.Proxy)

	tp.HoldWriteLock(t, 700*time.Millisecond)
	started := time.Now()
	if err := gw.registry.UpsertTool(ctx, "srv", "tool", "desc", map[string]interface{}{"a": 1}); err != nil {
		t.Fatalf("UpsertTool under primary lock: %v", err)
	}
	if time.Since(started) < 500*time.Millisecond {
		t.Fatalf("UpsertTool returned in %s while the lock was held", time.Since(started))
	}

	tp.HoldWriteLock(t, 500*time.Millisecond)
	if err := gw.stats.RecordUsage(ctx, "srv/tool", "sess", 12, true); err != nil {
		t.Fatalf("RecordUsage under primary lock: %v", err)
	}
	if n := countRows(t, tp, `SELECT COUNT(*) FROM mcp_tool_usage_stats WHERE tool_id = 'srv/tool'`); n != 1 {
		t.Fatalf("usage rows = %d, want 1", n)
	}
	// called_at is bound as time.Time on both paths, so the favourites query
	// (which compares against time.Time cutoffs) sees the forwarded row.
	favs, err := gw.GetFavorites(ctx)
	if err != nil || len(favs) != 1 || favs[0].ID != "srv/tool" {
		t.Fatalf("favorites after forwarded usage = %+v err=%v", favs, err)
	}

	// A registry built only from the shared pool (the WebUI fallback) finds
	// the proxy through the pool binding app.New sets up.
	dbproxy.BindPool(tp.SecondaryDB, tp.Proxy)
	t.Cleanup(func() { dbproxy.BindPool(tp.SecondaryDB, nil) })
	tp.HoldWriteLock(t, 500*time.Millisecond)
	if err := NewRegistry(tp.SecondaryDB).DeleteServer(ctx, "srv"); err != nil {
		t.Fatalf("DeleteServer via bound pool under primary lock: %v", err)
	}
	if n := countRows(t, tp, `SELECT COUNT(*) FROM mcp_tool_registry WHERE server_name = 'srv'`); n != 0 {
		t.Fatalf("registry rows = %d, want 0", n)
	}
}
