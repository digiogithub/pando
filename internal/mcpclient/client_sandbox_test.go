package mcpclient

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// fakeWrapper is an "enforced" backend that records the commands it wraps
// and optionally rewrites them (mirroring internal/llm/tools/shell's test
// double), so these tests exercise the real wiring in sandbox.go/client.go
// without depending on a real OS backend.
type fakeWrapper struct {
	mu      sync.Mutex
	calls   int
	err     error
	rewrite func(cmd *exec.Cmd)
}

func (f *fakeWrapper) Capability() sandbox.Capability {
	return sandbox.Capability{Backend: "fake", Version: "1", Enforced: true}
}

func (f *fakeWrapper) Wrap(cmd *exec.Cmd, p sandbox.Policy) error {
	if !p.Enabled() {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	if f.rewrite != nil {
		f.rewrite(cmd)
	}
	return nil
}

func (f *fakeWrapper) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// isolateSandboxConfig gives the test an isolated Sandbox config and undoes
// it on cleanup; per the repo pitfall, config-under-test never calls Load().
func isolateSandboxConfig(t *testing.T, sc config.SandboxConfig) {
	t.Helper()
	t.Setenv(config.SandboxEnvVar, "")
	prev := config.Get()
	config.SetForTests(&config.Config{Sandbox: sc})
	t.Cleanup(func() { config.SetForTests(prev) })
}

func TestWrapStdioCommandNotCoveredByDefault(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{})
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	cmd := exec.Command("true")
	if _, _, err := wrapStdioCommand(cmd, false); err != nil {
		t.Fatalf("wrapStdioCommand: %v", err)
	}
	if w.Calls() != 0 {
		t.Fatalf("calls = %d, want 0 (mcp is not in the default ExtendTo)", w.Calls())
	}
}

func TestWrapStdioCommandExtendedGlobally(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{ExtendTo: []string{"mcp"}})
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	t.Setenv("FAKE_MCP_API_KEY", "super-secret")
	cmd := exec.Command("true")
	if _, _, err := wrapStdioCommand(cmd, false); err != nil {
		t.Fatalf("wrapStdioCommand: %v", err)
	}
	if w.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (Sandbox.ExtendTo includes mcp)", w.Calls())
	}
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "FAKE_MCP_API_KEY=") {
			t.Fatalf("env not scrubbed: %q leaked into the wrapped command", kv)
		}
	}
}

func TestWrapStdioCommandExplicitServerFlag(t *testing.T) {
	// No global ExtendTo at all: only the per-server Sandbox=true flag asks
	// for confinement, which wrapStdioCommand must honor on its own since
	// sandbox.WrapCmd only looks at the global policy.
	isolateSandboxConfig(t, config.SandboxConfig{})
	w := &fakeWrapper{}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	cmd := exec.Command("true")
	if _, _, err := wrapStdioCommand(cmd, true); err != nil {
		t.Fatalf("wrapStdioCommand: %v", err)
	}
	if w.Calls() != 1 {
		t.Fatalf("calls = %d, want 1 (explicit server Sandbox=true)", w.Calls())
	}
}

func TestNewSandboxedCommandFuncFailsClosedWhenExplicit(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{})
	w := &fakeWrapper{err: sandbox.ErrWrapFailed}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	fn := newSandboxedCommandFunc("explicit-server", true)
	if _, err := fn(context.Background(), "true", nil, nil); err == nil {
		t.Fatal("expected a wrap failure to fail closed for an explicitly-sandboxed server")
	}
}

func TestNewSandboxedCommandFuncFallsBackWhenGlobalOnly(t *testing.T) {
	isolateSandboxConfig(t, config.SandboxConfig{ExtendTo: []string{"mcp"}})
	w := &fakeWrapper{err: sandbox.ErrWrapFailed}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	fn := newSandboxedCommandFunc("global-only-server", false)
	cmd, err := fn(context.Background(), "true", nil, nil)
	if err != nil {
		t.Fatalf("expected a fallback to the unwrapped command, got error: %v", err)
	}
	if cmd == nil {
		t.Fatal("expected a plain, unwrapped *exec.Cmd")
	}
}

// mcpHelperEnv, when set, turns this test binary into a trivial MCP stdio
// server instead of the mcpclient test suite (the self-exec pattern used by
// the Go standard library's os/exec tests: TestHelperProcess).
const mcpHelperEnv = "PANDO_MCP_SANDBOX_TEST_HELPER"

// TestHelperMCPStdioServer is not a real test. TestStdioHandshakeWithFakeWrapper
// re-executes the test binary with -test.run=TestHelperMCPStdioServer and
// PANDO_MCP_SANDBOX_TEST_HELPER=1 set, so it acts as a real (if trivial) MCP
// server the sandboxed CommandFunc launches and the handshake talks to.
func TestHelperMCPStdioServer(t *testing.T) {
	if os.Getenv(mcpHelperEnv) != "1" {
		t.Skip("helper process, not a real test")
	}
	srv := mcpserver.NewMCPServer("pando-sandbox-test", "0.0.1")
	if err := mcpserver.ServeStdio(srv); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}
}

// TestStdioHandshakeWithFakeWrapper is the "MCP handshake test with a
// trivial stdio server" the story asks for when feasible with the fake
// wrapper: a real subprocess (this test binary, re-invoked as a server) is
// spawned through newSandboxedCommandFunc with an enforced fake wrapper that
// prefixes the command with "env" (a real, harmless launcher), and the
// initialize handshake must still complete.
func TestStdioHandshakeWithFakeWrapper(t *testing.T) {
	envPath, err := exec.LookPath("env")
	if err != nil {
		t.Skip("requires the env(1) binary")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	isolateSandboxConfig(t, config.SandboxConfig{})
	w := &fakeWrapper{
		rewrite: func(cmd *exec.Cmd) {
			cmd.Args = append([]string{envPath, cmd.Path}, cmd.Args[1:]...)
			cmd.Path = envPath
		},
	}
	restore := sandbox.SetDefaultForTests(w)
	defer restore()

	srv := config.MCPServer{
		Type:    config.MCPStdio,
		Command: self,
		Args:    []string{"-test.run=^TestHelperMCPStdioServer$"},
		Env:     []string{mcpHelperEnv + "=1"},
		Sandbox: true, // per-server opt-in; no global ExtendTo needed
	}

	c, err := New(context.Background(), "trivial-stdio", srv)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), DefaultDiscoveryTimeout)
	defer cancel()
	if _, err := c.Initialize(ctx, BuildInitializeRequest("pando-sandbox-test")); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if w.Calls() != 1 {
		t.Fatalf("calls = %d, want 1: the stdio server must be wrapped (Sandbox=true)", w.Calls())
	}
}
