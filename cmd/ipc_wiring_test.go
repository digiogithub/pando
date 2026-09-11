// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

// isolatedIPCProject sets up a throwaway project directory with an isolated
// $HOME and a freshly loaded config, so Bootstrap's db.Connect and wireIPC's
// registry Announce/Revoke only ever touch resources this test owns.
func isolatedIPCProject(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	project := t.TempDir()
	t.Chdir(project)
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)
	if _, err := config.Load(project, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return project
}

// resetIPCTestGlobals undoes the process-wide globals SetupIPC/PromoteToPrimary
// touch (session.SetIPCPublisher, the remembrances IPC dispatcher), mirroring
// internal/app/promote_test.go's resetIPCGlobals so these tests do not leak a
// stale bus reference into whatever the cmd package runs next.
func resetIPCTestGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		session.SetIPCPublisher(nil)
		dbproxy.RegisterRemembrancesDispatcher(nil)
		dbproxy.RegisterStatementExecutor(nil)
	})
}

// fakeAgentService is a minimal, real (non-nil) agent.Service: enough for
// bridge.New(...).Start(ctx) to subscribe and run without panicking, since
// bridgeAgent calls Subscribe immediately. Every other method is an unused
// stub — wireIPC's tests never drive an actual agent run.
type fakeAgentService struct {
	*pubsub.Broker[agent.AgentEvent]
}

func newFakeAgentService() *fakeAgentService {
	return &fakeAgentService{Broker: pubsub.NewBroker[agent.AgentEvent]()}
}

func (f *fakeAgentService) Model() models.Model { return models.Model{} }
func (f *fakeAgentService) Run(context.Context, string, string, ...message.Attachment) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}
func (f *fakeAgentService) LastRunSystemMessages(string) []string { return nil }
func (f *fakeAgentService) Cancel(string)                         {}
func (f *fakeAgentService) Steer(string, string, ...message.Attachment) error {
	return nil
}
func (f *fakeAgentService) PendingSteering(string) int                   { return 0 }
func (f *fakeAgentService) InjectConclusion(string, string) error        { return nil }
func (f *fakeAgentService) Resume(context.Context, string, string) error { return nil }
func (f *fakeAgentService) ResurrectionCount(string) int                 { return 0 }
func (f *fakeAgentService) IsSessionBusy(string) bool                    { return false }
func (f *fakeAgentService) IsBusy() bool                                 { return false }
func (f *fakeAgentService) Update(config.AgentName, models.ModelID) (models.Model, error) {
	return models.Model{}, nil
}
func (f *fakeAgentService) Summarize(context.Context, string) error { return nil }
func (f *fakeAgentService) SummarizeStream(context.Context, string) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}
func (f *fakeAgentService) SetLuaManager(*luaengine.FilterManager) {}
func (f *fakeAgentService) GetTools() []tools.BaseTool             { return nil }

// bareAppForIPCTest builds an *app.App with just enough real (non-nil)
// services for wireIPC's shared busSetupFunc to wire and start a bridge
// without panicking, bypassing the heavy app.New (LLM providers, LSP, MCP
// gateway...) that nothing here needs to exercise.
func bareAppForIPCTest(sqlDB *ipcruntime.BootstrapResult) *app.App {
	q := db.New(sqlDB.SQLDB)
	return &app.App{
		Sessions:   session.NewService(q),
		Messages:   message.NewService(q),
		DBQuerier:  q,
		CoderAgent: newFakeAgentService(),
	}
}

// pingOverRPC dials rpcAddr and calls ipc.ping, failing the test if it does
// not answer within 2s. Used to prove a bus wired by wireIPC/
// primaryBusSetupFunc is actually up and serving, not just registered.
func pingOverRPC(t *testing.T, ctx context.Context, rpcAddr string) {
	t.Helper()
	client, err := ipc.NewClient(ctx)
	if err != nil {
		t.Fatalf("ipc.NewClient: %v", err)
	}
	defer client.Close()

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := client.Call(probeCtx, rpcAddr, "ipc.ping", nil); err != nil {
		t.Fatalf("ipc.ping against %s: %v", rpcAddr, err)
	}
}

// TestWireIPCPrimaryBranch covers wireIPC's primary branch: it must announce
// the instance in the registry, mark the app as the IPC primary, register the
// ordered-handover resources, and leave a live, answering bus behind — then
// its cleanup func must revoke the registry entry.
func TestWireIPCPrimaryBranch(t *testing.T) {
	resetIPCTestGlobals(t)
	project := isolatedIPCProject(t)
	ctx := context.Background()

	rt, err := ipcruntime.Bootstrap(ctx, project, "wireipc-test-primary")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	t.Cleanup(rt.Cleanup)
	if rt.Role != ipcruntime.RolePrimary {
		t.Fatalf("role = %s, want primary (nothing else should hold the lock in a fresh project)", rt.Role)
	}

	pandoApp := bareAppForIPCTest(rt)
	cleanup := wireIPC(ctx, rt, pandoApp, "wireipc-test-primary", project, instanceregistry.ModeTUI, wireOptions{})
	t.Cleanup(cleanup)

	if !pandoApp.IsIPCPrimary() {
		t.Fatal("wireIPC did not mark the app as IPC primary")
	}

	entry, err := instanceregistry.New().Get("wireipc-test-primary")
	if err != nil {
		t.Fatalf("registry Get: %v", err)
	}
	if entry == nil {
		t.Fatal("no registry entry after wireIPC")
	}
	if entry.Mode != instanceregistry.ModeTUI || !entry.IsPrimary {
		t.Fatalf("registry entry = %+v, want mode=%q is_primary=true", entry, instanceregistry.ModeTUI)
	}

	// A real RPC round-trip proves bus.Start, registerBridgeHandlers and the
	// coordinator wiring all succeeded — not just that no error was returned.
	pingOverRPC(t, ctx, fmt.Sprintf("tcp://127.0.0.1:%d", rt.RPCPort))

	cleanup()
	entry, err = instanceregistry.New().Get("wireipc-test-primary")
	if err != nil {
		t.Fatalf("registry Get after cleanup: %v", err)
	}
	if entry != nil {
		t.Fatal("registry entry still present after wireIPC's cleanup")
	}
}

// TestWireIPCSecondaryBranch covers wireIPC's secondary branch: it must
// announce the instance as non-primary, leave the app reporting itself as a
// secondary, and — the actual point of P1 — wire a busSetupFunc that, when a
// promotion later drives it (App.PromoteToPrimary), produces exactly the same
// working primary wiring wirePrimary would have produced directly. It
// exercises that by promoting the secondary and checking a real RPC
// round-trip on the ports the original primary used.
func TestWireIPCSecondaryBranch(t *testing.T) {
	resetIPCTestGlobals(t)
	project := isolatedIPCProject(t)
	ctx := context.Background()

	rtPrimary, err := ipcruntime.Bootstrap(ctx, project, "wireipc-test-secondary-primary")
	if err != nil {
		t.Fatalf("Bootstrap (primary): %v", err)
	}
	t.Cleanup(rtPrimary.Cleanup)
	primaryApp := bareAppForIPCTest(rtPrimary)
	cleanupPrimary := wireIPC(ctx, rtPrimary, primaryApp, "wireipc-test-secondary-primary", project, instanceregistry.ModeTUI, wireOptions{})
	t.Cleanup(cleanupPrimary)

	rtSecondary, err := ipcruntime.Bootstrap(ctx, project, "wireipc-test-secondary")
	if err != nil {
		t.Fatalf("Bootstrap (secondary): %v", err)
	}
	t.Cleanup(rtSecondary.Cleanup)
	if rtSecondary.Role != ipcruntime.RoleSecondary {
		t.Fatalf("role = %s, want secondary (a primary is already up in this project)", rtSecondary.Role)
	}

	secApp := bareAppForIPCTest(rtSecondary)
	cleanupSecondary := wireIPC(ctx, rtSecondary, secApp, "wireipc-test-secondary", project, instanceregistry.ModeACP, wireOptions{})
	t.Cleanup(cleanupSecondary)

	if secApp.IsIPCPrimary() {
		t.Fatal("secondary app reports itself as IPC primary before any promotion")
	}
	entry, err := instanceregistry.New().Get("wireipc-test-secondary")
	if err != nil {
		t.Fatalf("registry Get: %v", err)
	}
	if entry == nil || entry.IsPrimary {
		t.Fatalf("registry entry = %+v, want a non-primary entry", entry)
	}

	// Stop the secondary's own background watcher before driving promotion by
	// hand below: it is already monitoring rtPrimary's heartbeats (Bootstrap
	// starts it unconditionally), and this keeps the test deterministic
	// instead of racing its timers.
	rtSecondary.Watcher.Shutdown(ctx)

	// Tear the old primary down (lock, bus, DB — the same ordered Cleanup a
	// real shutdown runs) and re-acquire the now-free lock as this secondary,
	// exactly like the watcher would after losing heartbeats — but driven
	// directly so the test does not wait out HeartbeatTimeout. rtPrimary.Cleanup
	// is idempotent, so the t.Cleanup registered above still runs harmlessly.
	rtPrimary.Cleanup()
	isPrimaryNow, _, lockFile, err := ipc.AcquireLock(project, "wireipc-test-secondary", rtSecondary.PubPort, rtSecondary.RPCPort)
	if err != nil || !isPrimaryNow {
		t.Fatalf("AcquireLock after releasing the old primary: isPrimary=%v err=%v", isPrimaryNow, err)
	}

	if err := secApp.PromoteToPrimary(ctx, lockFile); err != nil {
		t.Fatalf("PromoteToPrimary: %v", err)
	}
	t.Cleanup(func() {
		if secApp.IPCBus != nil {
			_ = secApp.IPCBus.Shutdown()
		}
		ipc.ReleaseLock(lockFile)
	})

	if !secApp.IsIPCPrimary() {
		t.Fatal("secApp does not report itself as IPC primary after PromoteToPrimary")
	}
	// The promoted bus must rebind exactly the ports the original primary used
	// (from the lock file), so every other secondary's DBProxy keeps working —
	// prove it by pinging those same ports and getting the new primary's reply.
	pingOverRPC(t, ctx, fmt.Sprintf("tcp://127.0.0.1:%d", rtSecondary.RPCPort))
}
