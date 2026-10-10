package cmd

import (
	"context"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/bridge"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/logging"
)

// wireIPCRole connects pandoApp to the IPC role Bootstrap gave this process.
// The role is unrelated to the database (every instance writes pando.db
// directly): the leader serves the bus (instance browsing, remote view, hot-peer
// delegation) and runs the singleton jobs; a follower watches the leader and
// takes over both on failover.
func wireIPCRole(ctx context.Context, rt *ipcruntime.BootstrapResult, pandoApp *app.App, instanceID, cwd, mode string) {
	if rt.Role == ipcruntime.RolePrimary {
		bus := rt.Bus
		registerBridgeHandlers(bus, instanceID, pandoApp)
		pandoApp.SetupIPC(bus)
		if err := ipc.StartBusWithRetry(ctx, bus, rt.PubPort, rt.RPCPort); err != nil {
			logging.Error("IPC: failed to start bus; this instance leads but is unreachable over IPC (it will not appear in the instances browser)",
				"mode", mode, "error", err)
			return
		}
		bridge.New(bus, pandoApp.Sessions, pandoApp.CoderAgent).Start(ctx)
		// Start the leader watcher after the bus is up so heartbeat publishes
		// have a live socket.
		rt.Watcher.Start(ctx)
		logging.Debug("IPC: leader announced", "mode", mode, "instanceID", instanceID, "pubPort", rt.PubPort, "rpcPort", rt.RPCPort)
		return
	}

	// Follower: on promotion, recreate the leader wiring on a new bus.
	busSetup := func(busCtx context.Context, newBus *ipc.Bus) error {
		registerBridgeHandlers(newBus, instanceID, pandoApp)
		bridge.New(newBus, pandoApp.Sessions, pandoApp.CoderAgent).Start(busCtx)
		return nil
	}
	pandoApp.SetIPCSecondaryContext(rt.IPCClient, cwd, instanceID, rt.PubPort, rt.RPCPort, rt.Watcher, busSetup)
	rt.Watcher.SetPromoteCallback(pandoApp.PromoteToPrimary)
}
