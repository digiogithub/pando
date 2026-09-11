// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/instanceregistry"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
)

// TestMCPServerUsesSharedIPCBootstrap guards P2 of
// pando/plans/mcp_server_ipc_bootstrap.md: `pando mcp-server` must join the
// shared primary/secondary bootstrap (ipcruntime.BootstrapWithOptions +
// wireIPC) instead of calling db.Connect() directly, and must do so with
// mcp-server's specific policy: never kill an unresponsive primary, and never
// accept peer delegations. It is a source-shape test in the same style as
// TestEntrypointsUseSharedIPCWiring (root_test.go), which covers the other
// five entrypoints but explicitly left mcp_server.go for P2.
func TestMCPServerUsesSharedIPCBootstrap(t *testing.T) {
	source, err := os.ReadFile("mcp_server.go")
	if err != nil {
		t.Fatalf("read mcp_server.go: %v", err)
	}
	body := string(source)

	checks := []string{
		"ipcruntime.BootstrapWithOptions(",
		"AllowKillStalePrimary: false",
		"wireIPC(",
		"instanceregistry.ModeMCP",
		"AcceptDelegations: &acceptDelegations",
		"acceptDelegations := false",
	}
	for _, needle := range checks {
		if !strings.Contains(body, needle) {
			t.Errorf("mcp_server.go is missing %q", needle)
		}
	}

	// Look for an actual call ("= db.Connect()"), not this test's own
	// description of one showing up in a doc comment elsewhere in the file.
	if strings.Contains(body, "= db.Connect()") {
		t.Error("mcp_server.go still calls db.Connect() directly instead of the shared IPC bootstrap")
	}
}

// TestMCPServerBootstrapAndWiringStdoutIsClean guards P2's stdout-cleanliness
// requirement (pando/plans/mcp_server_ipc_bootstrap.md §5.3): on the stdio
// path, os.Stdout must carry nothing but the MCP JSON-RPC stream written by
// internal/mesnada/server.Server's own encoder. Everything the IPC bootstrap
// and wiring stack does — BootstrapWithOptions, the bus, the bridge, the
// failover watcher, the instance registry — must log through slog/stderr
// only.
//
// This drives ipcruntime.BootstrapWithOptions and wireIPC directly against a
// bare test App with mcp-server's exact options (mirroring
// bootstrapMCPServer, asserted by TestMCPServerUsesSharedIPCBootstrap above)
// rather than calling bootstrapMCPServer itself: a real app.New pulls in LLM
// providers, LSP and the MCP gateway, which cmd's tests deliberately never
// exercise (see cmd/ipc_wiring_test.go's bareAppForIPCTest doc comment). The
// MCP JSON-RPC encoder itself (internal/mesnada/server) is intentionally not
// exercised here either, since it is SUPPOSED to write to stdout.
func TestMCPServerBootstrapAndWiringStdoutIsClean(t *testing.T) {
	resetIPCTestGlobals(t)
	project := isolatedIPCProject(t)
	ctx := context.Background()

	rt, err := ipcruntime.BootstrapWithOptions(ctx, project, "mcp-stdout-test", ipcruntime.Options{
		ProbeTimeout:          mcpServerProbeTimeout,
		AllowKillStalePrimary: false,
	})
	if err != nil {
		t.Fatalf("BootstrapWithOptions: %v", err)
	}
	t.Cleanup(rt.Cleanup)
	if rt.Role != ipcruntime.RolePrimary {
		t.Fatalf("role = %s, want primary (nothing else should hold the lock in a fresh project)", rt.Role)
	}

	origStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe: %v", pipeErr)
	}
	os.Stdout = w
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		os.Stdout = origStdout
		_ = w.Close()
	}
	// Belt-and-braces: restore os.Stdout even if a later fatal assertion
	// short-circuits the explicit restore() call below.
	t.Cleanup(restore)

	pandoApp := bareAppForIPCTest(rt)
	acceptDelegations := false
	cleanup := wireIPC(ctx, rt, pandoApp, "mcp-stdout-test", project, instanceregistry.ModeMCP, wireOptions{
		AcceptDelegations: &acceptDelegations,
	})
	t.Cleanup(cleanup)

	// Give any stray background writer (bridge heartbeats, watcher ticks) a
	// moment to misbehave before checking.
	time.Sleep(50 * time.Millisecond)

	restore()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("bootstrap+wiring wrote %d byte(s) to stdout, want 0: %q", buf.Len(), buf.String())
	}
}

// TestShutdownMCPServerOrdered guards the exact step order P2 requires (plan
// §5.5): stop the MCP transports, then hand over the IPC primary role
// (App.Shutdown), then drop the registry entry (unwireIPC), then release the
// bootstrap runtime (rt.Cleanup). Uses plain recording closures so the order
// itself is asserted without needing real IPC/DB resources.
func TestShutdownMCPServerOrdered(t *testing.T) {
	var order []string
	record := func(step string) func() {
		return func() { order = append(order, step) }
	}

	shutdownMCPServerOrdered(
		record("stop-transports"),
		record("shutdown-app"),
		record("unwire-ipc"),
		record("cleanup-runtime"),
	)

	want := []string{"stop-transports", "shutdown-app", "unwire-ipc", "cleanup-runtime"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestWaitForMCPServerShutdown_SignalIsGraceful covers the SIGINT/SIGTERM
// trigger: a cancelled sigCtx must return nil (a graceful stop), regardless
// of whether either transport channel has anything pending.
func TestWaitForMCPServerShutdown_SignalIsGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitForMCPServerShutdown(ctx, make(chan error), make(chan error)); err != nil {
		t.Fatalf("err = %v, want nil for a signal-triggered shutdown", err)
	}
}

// TestWaitForMCPServerShutdown_StdioEOFIsGraceful covers the stdin-EOF
// trigger: the stdio transport returning nil (the client closed stdin) must
// be treated exactly like a graceful signal, not an error.
func TestWaitForMCPServerShutdown_StdioEOFIsGraceful(t *testing.T) {
	done := make(chan error, 1)
	done <- nil

	if err := waitForMCPServerShutdown(context.Background(), done, make(chan error)); err != nil {
		t.Fatalf("err = %v, want nil for stdin EOF", err)
	}
}

// TestWaitForMCPServerShutdown_StdioErrorPropagates covers a genuine stdio
// transport failure (not EOF): it must be returned, not swallowed.
func TestWaitForMCPServerShutdown_StdioErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	done := make(chan error, 1)
	done <- wantErr

	err := waitForMCPServerShutdown(context.Background(), done, make(chan error))
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

// TestWaitForMCPServerShutdown_HTTPErrorPropagates covers an HTTP transport
// failure: it must be returned, not swallowed.
func TestWaitForMCPServerShutdown_HTTPErrorPropagates(t *testing.T) {
	wantErr := errors.New("bind failed")
	errCh := make(chan error, 1)
	errCh <- wantErr

	err := waitForMCPServerShutdown(context.Background(), make(chan error), errCh)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
