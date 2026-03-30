//go:build integration

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// pandoBin is the path to the built pando binary, built once per test run.
var (
	pandoBinOnce sync.Once
	pandoBinPath string
	pandoBinErr  error
)

// getPandoBin returns the path to a built pando binary.
// It prefers the PANDO_BIN environment variable, then looks for pando in PATH,
// and finally builds it from source.
func getPandoBin(t *testing.T) string {
	t.Helper()

	// Allow override via env var
	if bin := os.Getenv("PANDO_BIN"); bin != "" {
		return bin
	}

	pandoBinOnce.Do(func() {
		// Check if pando is already in PATH
		if path, err := exec.LookPath("pando"); err == nil {
			pandoBinPath = path
			return
		}

		// Build from source
		dir, err := os.MkdirTemp("", "pando-acp-test-*")
		if err != nil {
			pandoBinErr = err
			return
		}

		bin := filepath.Join(dir, "pando")
		cmd := exec.Command("go", "build", "-o", bin, "github.com/digiogithub/pando/cmd/pando")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		// Build from the module root
		cmd.Dir = findModuleRoot()
		if err := cmd.Run(); err != nil {
			pandoBinErr = err
			return
		}
		pandoBinPath = bin
	})

	if pandoBinErr != nil {
		t.Skipf("pando binary not available (build failed: %v); set PANDO_BIN to point to a pre-built binary", pandoBinErr)
	}
	return pandoBinPath
}

// findModuleRoot walks up from the test file to find the go.mod.
func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// noopClient is a minimal ACP client implementation for testing.
type noopClient struct{}

func (c *noopClient) RequestPermission(_ context.Context, params acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	// Auto-approve the first option
	if len(params.Options) > 0 {
		return acpsdk.RequestPermissionResponse{
			Outcome: acpsdk.RequestPermissionOutcome{
				Selected: &acpsdk.RequestPermissionOutcomeSelected{OptionId: params.Options[0].OptionId},
			},
		}, nil
	}
	return acpsdk.RequestPermissionResponse{}, nil
}

func (c *noopClient) SessionUpdate(_ context.Context, _ acpsdk.SessionNotification) error {
	return nil
}

func (c *noopClient) WriteTextFile(_ context.Context, params acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, nil
}

func (c *noopClient) ReadTextFile(_ context.Context, params acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return acpsdk.ReadTextFileResponse{}, err
	}
	return acpsdk.ReadTextFileResponse{Content: string(content)}, nil
}

func (c *noopClient) CreateTerminal(_ context.Context, _ acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{TerminalId: "test-term"}, nil
}

func (c *noopClient) TerminalOutput(_ context.Context, _ acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, nil
}

func (c *noopClient) ReleaseTerminal(_ context.Context, _ acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, nil
}

func (c *noopClient) WaitForTerminalExit(_ context.Context, _ acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, nil
}

func (c *noopClient) KillTerminalCommand(_ context.Context, _ acpsdk.KillTerminalCommandRequest) (acpsdk.KillTerminalCommandResponse, error) {
	return acpsdk.KillTerminalCommandResponse{}, nil
}

// startACPSubprocess starts pando acp as a subprocess and returns a client connection.
func startACPSubprocess(t *testing.T) (*exec.Cmd, *acpsdk.ClientSideConnection) {
	t.Helper()

	pandoBin := getPandoBin(t)

	cwd := t.TempDir()

	cmd := exec.Command(pandoBin, "acp", "--cwd", cwd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start pando acp: %v", err)
	}

	conn := acpsdk.NewClientSideConnection(&noopClient{}, stdin, stdout)

	t.Cleanup(func() {
		stdin.Close()
		// Give the process a moment to finish cleanly
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
		}
	})

	return cmd, conn
}

// TestACPInitialize tests that pando acp responds correctly to an initialize request.
func TestACPInitialize(t *testing.T) {
	_, conn := startACPSubprocess(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{
			Fs: acpsdk.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	if resp.ProtocolVersion == 0 {
		t.Error("expected non-zero protocolVersion in response")
	}

	if resp.AgentInfo.Name == "" {
		t.Error("expected agentInfo.name to be set")
	} else {
		t.Logf("connected to agent: %s v%s (protocol v%d)", resp.AgentInfo.Name, resp.AgentInfo.Version, resp.ProtocolVersion)
	}
}

// TestACPNewSession tests that a new session can be created after initialization.
func TestACPNewSession(t *testing.T) {
	_, conn := startACPSubprocess(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion:    acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{},
	}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	workDir := t.TempDir()
	sessResp, err := conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        workDir,
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	if sessResp.SessionId == "" {
		t.Fatal("expected non-empty sessionId")
	}
	t.Logf("created session: %s", sessResp.SessionId)
}

// TestACPPromptSimple tests sending a simple prompt and receiving a response.
func TestACPPromptSimple(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prompt test in short mode (requires LLM)")
	}

	_, conn := startACPSubprocess(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion:    acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{},
	}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	workDir := t.TempDir()
	sessResp, err := conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        workDir,
		McpServers: []acpsdk.McpServer{},
	})
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	promptResp, err := conn.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: sessResp.SessionId,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock("Say 'hello' and nothing else.")},
	})
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	if promptResp.StopReason == "" {
		t.Error("expected non-empty stopReason in prompt response")
	}
	t.Logf("prompt stop reason: %s", promptResp.StopReason)
}

// TestACPMultipleSessions tests creating multiple sessions and verifying they are independent.
func TestACPMultipleSessions(t *testing.T) {
	_, conn := startACPSubprocess(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion:    acpsdk.ProtocolVersionNumber,
		ClientCapabilities: acpsdk.ClientCapabilities{},
	}); err != nil {
		t.Fatalf("initialize: %v", err)
	}

	sessionIDs := make([]string, 3)
	for i := range sessionIDs {
		dir := t.TempDir()
		resp, err := conn.NewSession(ctx, acpsdk.NewSessionRequest{
			Cwd:        dir,
			McpServers: []acpsdk.McpServer{},
		})
		if err != nil {
			t.Fatalf("newSession %d: %v", i, err)
		}
		if resp.SessionId == "" {
			t.Fatalf("session %d: empty sessionId", i)
		}
		sessionIDs[i] = resp.SessionId
	}

	// Verify all session IDs are unique
	seen := make(map[string]bool)
	for _, id := range sessionIDs {
		if seen[id] {
			t.Errorf("duplicate sessionId: %s", id)
		}
		seen[id] = true
	}
	t.Logf("created %d unique sessions", len(sessionIDs))
}
