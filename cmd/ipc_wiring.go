// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"context"
	"database/sql"
	"os"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/bridge"
	"github.com/digiogithub/pando/internal/ipc/changepub"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/ipc/writecoordinator"
	"github.com/digiogithub/pando/internal/logging"
)

// wireOptions customizes wireIPC's behaviour for the handful of differences
// that matter between entrypoints. The zero value reproduces today's TUI/ACP/
// serve/desktop/app behaviour.
type wireOptions struct {
	// AcceptDelegations overrides config.Get().Mesnada.Delegation.AcceptDelegations
	// for this instance's bridge handlers (see resolveAcceptDelegations). nil
	// keeps the configured default; every entrypoint migrated in P1 passes nil.
	// P2's ephemeral mcp-server passes a pointer to false, so it never accepts a
	// peer delegation regardless of the user's persisted setting: the process is
	// spawned by an editor/agent and dies with it, and is not a stable target
	// for another instance to route work to.
	AcceptDelegations *bool

	// OneShot is for short-lived processes (`pando cronjob run`, P4): on a
	// secondary, the promotion callback is NOT armed, so this process never
	// takes over as primary through failover — it would hold the IPC lock for
	// a few seconds and then hand it over again, delaying the long-running
	// instance that should win. The secondary watcher keeps running with no
	// callback, and P0 guarantees it then never takes the lock. On a primary
	// nothing changes: the process serves the bus (so a secondary started
	// meanwhile can forward its writes) and hands the role over in order when
	// it exits. Pair it with app.AppOptions.OneShot, which keeps the
	// primary-only background services off.
	OneShot bool
}

// wireIPC performs the post-app.New half of the IPC bootstrap shared by every
// long-lived entrypoint (TUI, ACP, serve, desktop, app; P2 adds mcp-server):
//
//   - Announces this instance in the registry under mode, and returns a
//     cleanup func (typically deferred) that revokes it.
//   - Primary (rt.Role == ipcruntime.RolePrimary): wires the write coordinator,
//     changepub publisher, db.write RPC handlers, bridge handlers/heartbeats
//     and the remembrances dispatcher onto rt.Bus (SetupIPC), starts the bus
//     and the failover watcher, and registers the ordered-handover resources
//     (SetIPCPrimaryHandover) so App.Shutdown drains the coordinator and
//     releases the lock before announcing instance.shutdown.
//   - Secondary: registers the secondary IPC context (SetIPCSecondaryContext)
//     with a busSetupFunc that recreates the exact same primary wiring on a
//     fresh Bus during promotion, and arms the promotion callback
//     (Watcher.SetPromoteCallback) so a failover can hand this instance the
//     primary role. The secondary watcher itself is already running —
//     ipcruntime.Bootstrap starts it unconditionally so it can heartbeat-watch
//     even before a promotion callback exists — so wireIPC does not call
//     Watcher.Start again here (Start is not safe to call twice: it would spawn
//     a second monitoring goroutine and double-close the watcher's done channel).
//
// Call it once, right after app.New, with the *ipcruntime.BootstrapResult from
// Bootstrap/BootstrapWithOptions. It does not touch rt.Cleanup or
// pandoApp.Shutdown; callers still own those (typically both deferred
// alongside wireIPC's own cleanup).
func wireIPC(
	ctx context.Context,
	rt *ipcruntime.BootstrapResult,
	pandoApp *app.App,
	instanceID, cwd string,
	mode instanceregistry.Mode,
	opts wireOptions,
) (cleanup func()) {
	_ = instanceregistry.Announce(&instanceregistry.Entry{
		InstanceID: instanceID,
		Path:       cwd,
		PID:        os.Getpid(),
		PubPort:    rt.PubPort,
		RPCPort:    rt.RPCPort,
		StartedAt:  time.Now(),
		Mode:       mode,
		IsPrimary:  rt.Role == ipcruntime.RolePrimary,
	})

	if rt.Role == ipcruntime.RolePrimary {
		wirePrimary(ctx, rt, pandoApp, instanceID, cwd, opts)
	} else {
		wireSecondary(rt, pandoApp, instanceID, cwd, opts)
	}

	return func() { _ = instanceregistry.Revoke(instanceID) }
}

// shutdownEntrypointOrdered runs the teardown of a wireIPC entrypoint in the
// order the plan requires (§5.5, same order as mcp-server's
// shutdownMCPServerOrdered minus its transport step): first the App's
// Shutdown, whose releasePrimaryRole drains the write coordinator, releases
// the IPC lock and announces instance.shutdown before the slower teardown;
// then the registry revoke, once this instance has definitely stopped acting
// as primary; then the bootstrap runtime (DB, watcher; ReleaseLock again is a
// no-op by then). Used by agui-serve and cronjob run as a single defer, so
// the order is a property of this function, not of defer stacking.
func shutdownEntrypointOrdered(shutdownApp, unwireIPC, cleanupRuntime func()) {
	shutdownApp()
	unwireIPC()
	cleanupRuntime()
}

// primaryBusSetupFunc returns the app.IPCBusSetupFunc closure that wires the
// write coordinator, changepub publisher, db.write RPC handlers and bridge
// handlers/heartbeats onto bus — the "primary wiring" every entrypoint needs.
//
// It is used in exactly two places, so they can never drift apart: once
// directly by wirePrimary, against the Bus Bootstrap created for an instance
// that started as primary, and once as the busSetupFunc a promoted secondary's
// App.PromoteToPrimary calls against a freshly created Bus (see
// app.IPCBusSetupFunc). In both cases the caller is responsible for starting
// or binding bus afterwards (bus.Start / the runtime's startPrimaryBus retry);
// this function must not do it.
func primaryBusSetupFunc(instanceID, cwd string, pandoApp *app.App, opts wireOptions) app.IPCBusSetupFunc {
	return func(ctx context.Context, bus *ipc.Bus, rwConn *sql.DB) (app.PrimaryWriteCoordinator, error) {
		coord := writecoordinator.New(ctx, db.New(rwConn), 256)
		pub := changepub.NewBusPublisher(bus.Publish, instanceID, cwd)
		coord.SetPublisher(pub)
		dbproxy.RegisterHandlersWithCoordinator(bus, coord)
		registerBridgeHandlers(bus, instanceID, pandoApp, opts.AcceptDelegations)
		registerPrimaryMaintenanceHandlers(bus, pandoApp)
		br := bridge.New(bus, pandoApp.Sessions, pandoApp.CoderAgent)
		br.Start(ctx)
		return coord, nil
	}
}

// wirePrimary wires rt.Bus with the shared primary handlers, starts it, and —
// once it is up — starts bridge heartbeats and the primary failover watcher.
// It registers the ordered-handover resources on pandoApp regardless of
// whether the bus actually starts, so a shutdown still drains the coordinator
// and releases the IPC lock even if this instance ends up running without a
// working bus (matching Bootstrap's own "continue without IPC" fallback).
func wirePrimary(ctx context.Context, rt *ipcruntime.BootstrapResult, pandoApp *app.App, instanceID, cwd string, opts wireOptions) {
	bus := rt.Bus
	setup := primaryBusSetupFunc(instanceID, cwd, pandoApp, opts)
	coord, err := setup(ctx, bus, rt.SQLDB)
	if err != nil {
		// The shared setup func never actually errors today (its steps are all
		// in-memory registrations); guarded so a future error path is not
		// silently swallowed instead of surfacing as "continuing without IPC".
		logging.Warn("IPC: failed to wire primary bus handlers, continuing without IPC", "error", err)
		return
	}
	pandoApp.SetupIPC(bus)
	// Ordered handover on shutdown: App.Shutdown drains coord, releases the
	// lock, then announces instance.shutdown and closes the bus, before the
	// rest of a (possibly slow) shutdown runs. Do not also `defer coord.Shutdown()`
	// at the call site: that would run first (LIFO) and discard queued writes
	// instead of draining them.
	pandoApp.SetIPCPrimaryHandover(coord, rt.ReleaseLock)

	if busErr := bus.Start(ctx, rt.PubPort, rt.RPCPort); busErr != nil {
		logging.Warn("IPC: failed to start bus, continuing without IPC", "error", busErr)
		return
	}
	// Start the primary failover watcher after the bus is up so heartbeat
	// publishes have a live socket. The bridge (started inside setup above)
	// also publishes heartbeats, but the watcher covers the shutdown-signal
	// path independently.
	rt.Watcher.Start(ctx)
}

// wireSecondary registers the secondary IPC context (probe function, promote
// callback) and the busSetupFunc a promotion uses to recreate the exact
// primary wiring on a new Bus.
func wireSecondary(rt *ipcruntime.BootstrapResult, pandoApp *app.App, instanceID, cwd string, opts wireOptions) {
	busSetupFunc := primaryBusSetupFunc(instanceID, cwd, pandoApp, opts)
	pandoApp.SetIPCSecondaryContext(
		rt.IPCClient,
		rt.SQLDB,
		cwd,
		instanceID,
		rt.PubPort,
		rt.RPCPort,
		rt.Watcher,
		busSetupFunc,
	)
	if opts.OneShot {
		// A one-shot process is never a promotion candidate (see
		// wireOptions.OneShot). With no callback the watcher never takes the
		// lock (P0's G3 guard), so it cannot leave a zombie lock behind either.
		logging.Info("IPC: one-shot secondary, failover promotion not armed", "instance_id", instanceID)
		return
	}
	// Register the promotion callback so the watcher can call PromoteToPrimary
	// when it wins the lock race. The secondary watcher is already running
	// (ipcruntime.Bootstrap starts it unconditionally); see the wireIPC doc
	// comment for why it is not started again here.
	rt.Watcher.SetPromoteCallback(pandoApp.PromoteToPrimary)
}
