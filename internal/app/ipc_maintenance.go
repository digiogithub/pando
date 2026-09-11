package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// Primary-side maintenance operations reachable over IPC (P4 of
// pando/plans/mcp_server_ipc_bootstrap.md):
//
//   - cronjob.reload: a secondary that persisted a cron edit hands the new
//     configuration to the primary, whose scheduler is the only one running.
//   - kb.relink: `pando kb relink` runs on the primary's writer instead of
//     opening a second writer next to it.
//
// The RPC handlers themselves are registered by the entrypoint wiring
// (cmd/ipc_primary_rpc.go), on the primary bus and on a promoted one alike.

// cronJobReloadCallTimeout bounds a secondary's cronjob.reload call. The
// primary only swaps an in-memory config and reschedules, so this is short.
const cronJobReloadCallTimeout = 10 * time.Second

// ForwardCronJobsToPrimary sends jobs (a cron configuration this process has
// just persisted with config.UpdateCronJobs) to the IPC primary over the
// cronjob.reload RPC. It reports whether a forward was attempted: on the
// primary itself, or without IPC, there is nobody to tell and it returns
// (false, nil) — the caller's own CronService.Reload already covered it.
//
// Cron jobs live in the config file, not the database, so no changepub event
// reaches the primary, and only a TUI watches the file. Without this call a
// non-TUI primary (ACP, mcp-server, serve, ...) would keep scheduling the old
// jobs until it restarts.
func (app *App) ForwardCronJobsToPrimary(ctx context.Context, jobs config.CronJobsConfig) (bool, error) {
	if app.IsIPCPrimary() || app.ipcClient == nil || app.ipcRPCPort == 0 {
		return false, nil
	}
	raw, err := json.Marshal(jobs)
	if err != nil {
		return false, fmt.Errorf("cronjob reload: encode config: %w", err)
	}
	rpcAddr := fmt.Sprintf("tcp://127.0.0.1:%d", app.ipcRPCPort)
	if _, err := app.ipcClient.CallWithTimeout(ctx, rpcAddr, protocol.MethodCronJobReload,
		protocol.CronJobReloadParams{CronJobs: raw}, cronJobReloadCallTimeout); err != nil {
		return true, fmt.Errorf("cronjob reload via primary: %w", err)
	}
	return true, nil
}

// ApplyCronJobsFromPeer is the primary's cronjob.reload handler body: it
// installs jobs as the in-memory cron configuration (config.SetCronJobsInMemory:
// no file write, no config.Reload, so in-memory runtime overrides survive) and
// reschedules the cron service. The file was already written by the sender.
func (app *App) ApplyCronJobsFromPeer(jobs config.CronJobsConfig) (protocol.CronJobReloadResult, error) {
	res := protocol.CronJobReloadResult{Jobs: len(jobs.Jobs)}
	if err := config.SetCronJobsInMemory(jobs); err != nil {
		return res, err
	}
	if app.CronService != nil {
		res.Scheduler = true
		if err := app.CronService.Reload(jobs); err != nil {
			return res, err
		}
	}
	logging.Info("cronjob.reload: applied cron configuration from another instance",
		"jobs", res.Jobs, "enabled", jobs.Enabled, "scheduler", res.Scheduler)
	return res, nil
}

// RelinkKB rebuilds the knowledge-base wiki-link graph on this process's own
// writer: BackfillLinks (documents with no links yet) or, with force, RelinkAll.
//
// It is the primary's kb.relink handler body. It uses the KB store's own write
// path — short, batched IMMEDIATE transactions on the primary pool, exactly
// what the primary's kb-link-backfill service does — rather than the write
// coordinator: the coordinator is a single goroutine, and a bulk relink queued
// on it would hold up every write forwarded by secondaries until it finished,
// while per-batch transactions already serialize with the coordinator's own
// writes through SQLite's write lock and the primary's busy timeout.
//
// On a secondary it refuses: its store would silently skip the pass (a remote
// proxy means "the primary owns the writer").
func (app *App) RelinkKB(ctx context.Context, force bool) (kb.BackfillStats, error) {
	if !app.IsIPCPrimary() && app.ipcClient != nil {
		return kb.BackfillStats{}, errors.New("kb relink: this instance is an IPC secondary; run it on the primary")
	}
	if cfg := config.Get(); cfg != nil && !cfg.KBWikiLinksEnabled() {
		return kb.BackfillStats{}, errors.New("wiki links are disabled on the running instance: set Remembrances.KBWikiLinks = true")
	}

	var store *kb.KBStore
	switch {
	case app.Remembrances != nil && app.Remembrances.KB != nil:
		store = app.Remembrances.KB
	case app.rwConn != nil:
		// Remembrances is disabled on this instance: link extraction needs no
		// embedder or chunking, so a bare store on the writer pool is enough
		// (the same thing the CLI does when no instance is running).
		store = kb.NewKBStore(app.rwConn, nil, 0, 0)
	default:
		return kb.BackfillStats{}, errors.New("kb relink: no writable database connection")
	}

	if force {
		return store.RelinkAll(ctx)
	}
	return store.BackfillLinks(ctx)
}
