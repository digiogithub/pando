package acp

import (
	"context"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
)

// mockAgentSideConnection is a mock implementation for testing.
type mockAgentSideConnection struct {
	readTextFileFunc        func(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error)
	writeTextFileFunc       func(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error)
	createTerminalFunc      func(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error)
	terminalOutputFunc      func(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error)
	waitForTerminalExitFunc func(ctx context.Context, req acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error)
	killTerminalFunc        func(ctx context.Context, req acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error)
	releaseTerminalFunc     func(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error)
}

func (m *mockAgentSideConnection) ReadTextFile(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	if m.readTextFileFunc != nil {
		return m.readTextFileFunc(ctx, req)
	}
	return acpsdk.ReadTextFileResponse{Content: "mock content"}, nil
}

func (m *mockAgentSideConnection) WriteTextFile(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	if m.writeTextFileFunc != nil {
		return m.writeTextFileFunc(ctx, req)
	}
	return acpsdk.WriteTextFileResponse{}, nil
}

func (m *mockAgentSideConnection) CreateTerminal(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	if m.createTerminalFunc != nil {
		return m.createTerminalFunc(ctx, req)
	}
	return acpsdk.CreateTerminalResponse{TerminalId: "mock-terminal-1"}, nil
}

func (m *mockAgentSideConnection) TerminalOutput(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	if m.terminalOutputFunc != nil {
		return m.terminalOutputFunc(ctx, req)
	}
	return acpsdk.TerminalOutputResponse{Output: "mock output"}, nil
}

func (m *mockAgentSideConnection) WaitForTerminalExit(ctx context.Context, req acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	if m.waitForTerminalExitFunc != nil {
		return m.waitForTerminalExitFunc(ctx, req)
	}
	exitCode := 0
	return acpsdk.WaitForTerminalExitResponse{ExitCode: &exitCode}, nil
}

func (m *mockAgentSideConnection) KillTerminal(ctx context.Context, req acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	if m.killTerminalFunc != nil {
		return m.killTerminalFunc(ctx, req)
	}
	return acpsdk.KillTerminalResponse{}, nil
}

func (m *mockAgentSideConnection) ReleaseTerminal(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	if m.releaseTerminalFunc != nil {
		return m.releaseTerminalFunc(ctx, req)
	}
	return acpsdk.ReleaseTerminalResponse{}, nil
}

func TestReadTextFile(t *testing.T) {
	mock := &mockAgentSideConnection{
		readTextFileFunc: func(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
			if req.Path != "test.txt" {
				t.Errorf("expected path 'test.txt', got '%s'", req.Path)
			}
			return acpsdk.ReadTextFileResponse{Content: "test content"}, nil
		},
	}

	conn := NewACPClientConnection("test-session", mock, "/workspace", nil)

	ctx := context.Background()
	content, err := conn.ReadTextFile(ctx, "test.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if content != "test content" {
		t.Errorf("expected 'test content', got '%s'", content)
	}
}

func TestWriteTextFile(t *testing.T) {
	mock := &mockAgentSideConnection{
		writeTextFileFunc: func(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
			if req.Path != "test.txt" {
				t.Errorf("expected path 'test.txt', got '%s'", req.Path)
			}
			if req.Content != "new content" {
				t.Errorf("expected content 'new content', got '%s'", req.Content)
			}
			return acpsdk.WriteTextFileResponse{}, nil
		},
	}

	conn := NewACPClientConnection("test-session", mock, "/workspace", nil)

	ctx := context.Background()
	err := conn.WriteTextFile(ctx, "test.txt", "new content")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateTerminal(t *testing.T) {
	mock := &mockAgentSideConnection{
		createTerminalFunc: func(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
			if req.Command != "ls" {
				t.Errorf("expected command 'ls', got '%s'", req.Command)
			}
			if len(req.Args) != 1 || req.Args[0] != "-la" {
				t.Errorf("expected args ['-la'], got %v", req.Args)
			}
			return acpsdk.CreateTerminalResponse{TerminalId: "terminal-123"}, nil
		},
	}

	conn := NewACPClientConnection("test-session", mock, "/workspace", nil)

	ctx := context.Background()
	terminalID, err := conn.CreateTerminal(ctx, "ls", []string{"-la"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if terminalID != "terminal-123" {
		t.Errorf("expected terminal ID 'terminal-123', got '%s'", terminalID)
	}
}

func TestTerminalOutput(t *testing.T) {
	mock := &mockAgentSideConnection{
		terminalOutputFunc: func(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
			if req.TerminalId != "terminal-123" {
				t.Errorf("expected terminal ID 'terminal-123', got '%s'", req.TerminalId)
			}
			return acpsdk.TerminalOutputResponse{Output: "command output"}, nil
		},
	}

	conn := NewACPClientConnection("test-session", mock, "/workspace", nil)

	ctx := context.Background()
	output, err := conn.TerminalOutput(ctx, "terminal-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "command output" {
		t.Errorf("expected 'command output', got '%s'", output)
	}
}

func TestPathTraversalPrevention(t *testing.T) {
	conn := NewACPClientConnection("test-session", &mockAgentSideConnection{}, "/workspace", nil)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"relative path ok", "file.txt", false},
		{"subdirectory ok", "subdir/file.txt", false},
		{"absolute path denied", "/etc/passwd", true},
		{"path traversal denied", "../../../etc/passwd", true},
		{"hidden traversal denied", "subdir/../../file.txt", true},
		{"double dot start denied", "../file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := conn.validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath(%s) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestWaitForTerminalExit(t *testing.T) {
	exitCode := 0
	mock := &mockAgentSideConnection{
		waitForTerminalExitFunc: func(ctx context.Context, req acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
			if req.TerminalId != "terminal-123" {
				t.Errorf("expected terminal ID 'terminal-123', got '%s'", req.TerminalId)
			}
			return acpsdk.WaitForTerminalExitResponse{ExitCode: &exitCode}, nil
		},
	}

	conn := NewACPClientConnection("test-session", mock, "/workspace", nil)

	ctx := context.Background()
	resultCode, err := conn.WaitForTerminalExit(ctx, "terminal-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resultCode == nil {
		t.Fatal("expected non-nil exit code")
	}

	if *resultCode != 0 {
		t.Errorf("expected exit code 0, got %d", *resultCode)
	}
}

func TestKillTerminal(t *testing.T) {
	mock := &mockAgentSideConnection{
		killTerminalFunc: func(ctx context.Context, req acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
			if req.TerminalId != "terminal-123" {
				t.Errorf("expected terminal ID 'terminal-123', got '%s'", req.TerminalId)
			}
			return acpsdk.KillTerminalResponse{}, nil
		},
	}

	conn := NewACPClientConnection("test-session", mock, "/workspace", nil)

	ctx := context.Background()
	err := conn.KillTerminal(ctx, "terminal-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReleaseTerminal(t *testing.T) {
	mock := &mockAgentSideConnection{
		releaseTerminalFunc: func(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
			if req.TerminalId != "terminal-123" {
				t.Errorf("expected terminal ID 'terminal-123', got '%s'", req.TerminalId)
			}
			return acpsdk.ReleaseTerminalResponse{}, nil
		},
	}

	conn := NewACPClientConnection("test-session", mock, "/workspace", nil)

	ctx := context.Background()
	err := conn.ReleaseTerminal(ctx, "terminal-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
