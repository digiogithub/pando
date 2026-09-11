package app

import (
	"context"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/logging"
)

// Primary-only background services
//
// Every process that shares a project's database used to run the same
// background services, so on each fsnotify event every process reindexed the
// same file and every cron job fired once per process. Those services now run
// only on the IPC primary: New registers them with registerPrimaryService and
// starts them when this process is the primary (applyStartupIPCRole); a
// secondary starts them when it is promoted (PromoteToPrimary). Explicit user
// actions (code_index_project, a manual KB import, cron "run now", ...) are not
// gated: on a secondary they write through the IPC proxy as before.
//
// See pando/plans/mcp_server_ipc_bootstrap.md §5.4 (P3).

// primaryService is one background service only the IPC primary runs. start
// must return quickly: it spawns its own goroutine(s), registering them in
// watcherCancelFuncs/watcherWG (or stopped by Shutdown, like CronService), so
// App.Shutdown stops them the same way whether they started in New or on a
// promotion.
type primaryService struct {
	name  string
	start func(ctx context.Context)
}

// resolveIPCRole returns the IPC role New gates the primary-only services on.
// An explicit AppOptions.IPCRole wins. Otherwise a remote *dbproxy.DBProxy
// querier means secondary, and anything else (no IPC: tests, one-shot CLIs)
// means primary, which keeps today's behaviour for callers without IPC.
func resolveIPCRole(opt AppOptions) ipcruntime.Role {
	switch opt.IPCRole {
	case ipcruntime.RolePrimary, ipcruntime.RoleSecondary:
		return opt.IPCRole
	}
	if p, ok := opt.DBQuerier.(*dbproxy.DBProxy); ok && p.IsRemote() {
		return ipcruntime.RoleSecondary
	}
	return ipcruntime.RolePrimary
}

// registerPrimaryService adds a primary-only service. Called by New (single
// goroutine, before the App is shared); the services start later, together, in
// startPrimaryServices.
func (app *App) registerPrimaryService(name string, start func(ctx context.Context)) {
	app.primarySvcMu.Lock()
	defer app.primarySvcMu.Unlock()
	app.primarySvcs = append(app.primarySvcs, primaryService{name: name, start: start})
}

// applyStartupIPCRole is the end of New: a primary starts its primary-only
// services now, a secondary logs that they wait for a promotion.
func (app *App) applyStartupIPCRole(role ipcruntime.Role, startupMode string) {
	if role == ipcruntime.RoleSecondary {
		app.primarySvcMu.Lock()
		names := app.primaryServiceNamesLocked()
		app.primarySvcMu.Unlock()
		logging.Info("IPC role: secondary, primary-only background services skipped until promotion",
			"startup_mode", startupMode,
			"services", names,
		)
		return
	}
	app.startPrimaryServices("startup")
}

// startPrimaryServices starts every registered primary-only service, at most
// once per App: a later call (a spurious promotion of a primary, a concurrent
// caller) is a no-op, and so is any call after Shutdown began. It reports
// whether this call started them.
//
// The services run on app.lifetimeCtx (the context New got), never on the
// caller's: PromoteToPrimary runs on the failover watcher's goroutine with a
// context that ends when the watcher stops. primarySvcMu is held while the
// services start (each start only spawns goroutines), so closePrimaryServices
// cannot return while a start is still adding to watcherWG.
func (app *App) startPrimaryServices(trigger string) bool {
	app.primarySvcMu.Lock()
	defer app.primarySvcMu.Unlock()

	if app.primarySvcClosed {
		logging.Info("IPC role: primary-only background services not started, app is shutting down",
			"trigger", trigger)
		return false
	}
	if app.primarySvcStarted {
		logging.Debug("IPC role: primary-only background services already running", "trigger", trigger)
		return false
	}
	app.primarySvcStarted = true

	ctx := app.lifetimeCtx
	if ctx == nil {
		ctx = context.Background()
	}
	logging.Info("IPC role: primary, starting primary-only background services",
		"trigger", trigger,
		"services", app.primaryServiceNamesLocked(),
	)
	for _, svc := range app.primarySvcs {
		svc.start(ctx)
	}
	return true
}

// closePrimaryServices makes every later startPrimaryServices a no-op. Shutdown
// calls it before it cancels and waits for the background goroutines.
func (app *App) closePrimaryServices() {
	app.primarySvcMu.Lock()
	defer app.primarySvcMu.Unlock()
	app.primarySvcClosed = true
}

// primaryServiceNamesLocked lists the registered services, for logs. Requires
// primarySvcMu.
func (app *App) primaryServiceNamesLocked() []string {
	names := make([]string, 0, len(app.primarySvcs))
	for _, svc := range app.primarySvcs {
		names = append(names, svc.name)
	}
	return names
}
