package acp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestPathTraversalPrevention_Client(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acp-security-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewMesnadaACPClient("test-task", tmpDir, nil, nil)

	tests := []struct {
		name        string
		path        string
		shouldError bool
		errContains string
	}{
		{
			name:        "valid relative path",
			path:        "test.txt",
			shouldError: false,
		},
		{
			name:        "valid subdirectory path",
			path:        "subdir/test.txt",
			shouldError: false,
		},
		{
			name:        "absolute path denied",
			path:        "/etc/passwd",
			shouldError: true,
			errContains: "absolute paths not allowed",
		},
		{
			name:        "path traversal denied",
			path:        "../../../etc/passwd",
			shouldError: true,
			errContains: "path outside workspace",
		},
		{
			name:        "hidden traversal denied",
			path:        "subdir/../../etc/passwd",
			shouldError: true,
			errContains: "path outside workspace",
		},
		{
			name:        "double dot start denied",
			path:        "../file.txt",
			shouldError: true,
			errContains: "path outside workspace",
		},
		{
			name:        "double dot in subdirectory",
			path:        "a/b/../../../file.txt",
			shouldError: true,
			errContains: "path outside workspace",
		},
		{
			name:        "empty path",
			path:        "",
			shouldError: false, // Empty path is valid (resolves to workDir)
		},
		{
			name:        "dot path",
			path:        ".",
			shouldError: false, // Dot is valid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test validatePathInWorkspace directly
			_, err := client.validatePathInWorkspace(tt.path)
			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for path %s, got nil", tt.path)
				} else if tt.errContains != "" && !containsStr(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %s, got %s", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for path %s: %v", tt.path, err)
				}
			}
		})
	}
}

func TestReadTextFile_WorkspaceBoundary(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acp-file-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test file inside workspace
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a test file outside workspace
	outsideDir := filepath.Dir(tmpDir)
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside content"), 0644); err != nil {
		t.Fatalf("Failed to create outside file: %v", err)
	}
	defer os.Remove(outsideFile)

	client := NewMesnadaACPClient("test-task", tmpDir, nil, nil)

	// Test reading file inside workspace
	_, err = client.ReadTextFile(context.Background(), acpsdk.ReadTextFileRequest{
		Path: "test.txt",
	})
	if err != nil {
		t.Errorf("Expected to read file inside workspace, got error: %v", err)
	}

	// Test reading file outside workspace (path traversal)
	_, err = client.ReadTextFile(context.Background(), acpsdk.ReadTextFileRequest{
		Path: "../outside.txt",
	})
	if err == nil {
		t.Error("Expected error when reading file outside workspace")
	}
}

func TestWriteTextFile_WorkspaceBoundary(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acp-write-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewMesnadaACPClient("test-task", tmpDir, nil, nil)

	// Test writing file inside workspace
	_, err = client.WriteTextFile(context.Background(), acpsdk.WriteTextFileRequest{
		Path:    "newfile.txt",
		Content: "new content",
	})
	if err != nil {
		t.Errorf("Expected to write file inside workspace, got error: %v", err)
	}

	// Verify the file was created
	content, err := os.ReadFile(filepath.Join(tmpDir, "newfile.txt"))
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}
	if string(content) != "new content" {
		t.Errorf("Expected content 'new content', got %s", string(content))
	}

	// Test writing to subdirectory
	_, err = client.WriteTextFile(context.Background(), acpsdk.WriteTextFileRequest{
		Path:    "subdir/another.txt",
		Content: "subdir content",
	})
	if err != nil {
		t.Errorf("Expected to write file in subdirectory, got error: %v", err)
	}

	// Verify subdirectory file
	content, err = os.ReadFile(filepath.Join(tmpDir, "subdir", "another.txt"))
	if err != nil {
		t.Fatalf("Failed to read subdirectory file: %v", err)
	}
	if string(content) != "subdir content" {
		t.Errorf("Expected content 'subdir content', got %s", string(content))
	}

	// Test writing file outside workspace
	_, err = client.WriteTextFile(context.Background(), acpsdk.WriteTextFileRequest{
		Path:    "../outside.txt",
		Content: "outside content",
	})
	if err == nil {
		t.Error("Expected error when writing file outside workspace")
	}
}

func TestCreateTerminal_WorkspaceBoundary(t *testing.T) {
	client := NewMesnadaACPClient("test-task", "/tmp/workspace", nil, nil)

	// Test with valid CWD
	_, err := client.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "ls",
		Args:    []string{"-la"},
	})
	if err != nil {
		t.Errorf("Unexpected error with default CWD: %v", err)
	}

	// Test with valid subdirectory CWD
	_, err = client.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "ls",
		Cwd:     strPtr("subdir"),
	})
	if err != nil {
		t.Errorf("Unexpected error with valid CWD: %v", err)
	}

	// Test with path traversal CWD
	_, err = client.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "ls",
		Cwd:     strPtr("../../../etc"),
	})
	if err == nil {
		t.Error("Expected error when using path traversal in CWD")
	}
}

func TestCapabilityRestrictions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "acp-capability-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("file_access_disabled", func(t *testing.T) {
		client := NewMesnadaACPClient("test-task", tmpDir, nil, nil)
		client.SetCapabilities(ACPCapabilities{
			FileAccess:  false,
			Terminals:   false,
			Permissions: false,
		})

		// ReadTextFile should fail
		_, err := client.ReadTextFile(context.Background(), acpsdk.ReadTextFileRequest{
			Path: "test.txt",
		})
		if err == nil {
			t.Error("Expected error when file access is disabled")
		}

		// WriteTextFile should fail
		_, err = client.WriteTextFile(context.Background(), acpsdk.WriteTextFileRequest{
			Path:    "test.txt",
			Content: "test",
		})
		if err == nil {
			t.Error("Expected error when file access is disabled")
		}
	})

	t.Run("terminal_disabled", func(t *testing.T) {
		client := NewMesnadaACPClient("test-task", tmpDir, nil, nil)
		client.SetCapabilities(ACPCapabilities{
			FileAccess:  true,
			Terminals:   false,
			Permissions: false,
		})

		// CreateTerminal should fail
		_, err := client.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
			Command: "ls",
		})
		if err == nil {
			t.Error("Expected error when terminal access is disabled")
		}
	})

	t.Run("all_enabled", func(t *testing.T) {
		client := NewMesnadaACPClient("test-task", tmpDir, nil, nil)
		client.SetCapabilities(ACPCapabilities{
			FileAccess:  true,
			Terminals:   true,
			Permissions: true,
		})

		// Should work fine
		_, err := client.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
			Command: "echo",
			Args:    []string{"hello"},
		})
		if err != nil {
			t.Errorf("Unexpected error with all capabilities enabled: %v", err)
		}
	})
}

func TestPermissionQueue_ManualApproval(t *testing.T) {
	client := NewMesnadaACPClient("test-task", "/tmp", nil, nil)
	client.SetAutoPermission(false)
	q := client.GetPermissionQueue()

	if q == nil {
		t.Fatal("Permission queue should not be nil")
	}

	// Create a permission request
	title := "Test Tool"
	req := acpsdk.RequestPermissionRequest{
		SessionId: "session-1",
		ToolCall: acpsdk.RequestPermissionToolCall{
			Title: &title,
		},
		Options: []acpsdk.PermissionOption{
			{OptionId: "approve", Name: "Approve"},
			{OptionId: "deny", Name: "Deny"},
		},
	}

	// Queue the permission
	requestID := q.QueuePermission("task-1", "session-1", req)
	if requestID == "" {
		t.Error("Expected non-empty request ID")
	}

	// Check pending permissions
	pending := q.GetPending("task-1")
	if len(pending) != 1 {
		t.Errorf("Expected 1 pending permission, got %d", len(pending))
	}

	// Resolve with approved
	err := q.ResolvePermission(requestID, acpsdk.NewRequestPermissionOutcomeSelected("approve"))
	if err != nil {
		t.Errorf("Failed to resolve permission: %v", err)
	}

	// Check it's no longer pending
	pending = q.GetPending("task-1")
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending permissions after resolution, got %d", len(pending))
	}
}

func TestPermissionQueue_AutoApproval(t *testing.T) {
	client := NewMesnadaACPClient("test-task", "/tmp", nil, nil)
	client.SetAutoPermission(true)

	title := "Test Tool"
	req := acpsdk.RequestPermissionRequest{
		SessionId: "session-1",
		ToolCall: acpsdk.RequestPermissionToolCall{
			Title: &title,
		},
		Options: []acpsdk.PermissionOption{
			{OptionId: "approve", Name: "Approve"},
		},
	}

	// With auto permission, this should return immediately
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.RequestPermission(ctx, req)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if resp.Outcome.Selected == nil {
		t.Error("Expected selected outcome")
	}
	if resp.Outcome.Selected.OptionId != "approve" {
		t.Errorf("Expected option 'approve', got %s", resp.Outcome.Selected.OptionId)
	}
}

func TestClientLogging(t *testing.T) {
	// Create temp file for logging
	tmpFile, err := os.CreateTemp("", "acp-log-test")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpDir, err := os.MkdirTemp("", "acp-workspace")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	client := NewMesnadaACPClient("test-task", tmpDir, tmpFile, nil)

	// Create a terminal (should generate log)
	_, err = client.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "echo",
		Args:    []string{"test"},
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Close log file to flush
	tmpFile.Close()

	// Read log content
	logContent, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	// Should contain log entries
	logStr := string(logContent)
	if !containsStr(logStr, "CREATE TERMINAL") {
		t.Error("Expected log to contain CREATE TERMINAL")
	}
}

func TestTerminalLifecycle(t *testing.T) {
	client := NewMesnadaACPClient("test-task", "/tmp", nil, nil)

	// Create terminal
	resp, err := client.CreateTerminal(context.Background(), acpsdk.CreateTerminalRequest{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("Failed to create terminal: %v", err)
	}

	terminalID := resp.TerminalId
	if terminalID == "" {
		t.Error("Expected non-empty terminal ID")
	}

	// Get output
	outputResp, err := client.TerminalOutput(context.Background(), acpsdk.TerminalOutputRequest{
		TerminalId: terminalID,
	})
	if err != nil {
		t.Errorf("Failed to get terminal output: %v", err)
	}

	// Output may be empty or contain "hello" depending on timing
	_ = outputResp

	// Wait for exit
	exitResp, err := client.WaitForTerminalExit(context.Background(), acpsdk.WaitForTerminalExitRequest{
		TerminalId: terminalID,
	})
	if err != nil {
		t.Errorf("Failed to wait for terminal exit: %v", err)
	}

	// Should have exit code 0
	if exitResp.ExitCode == nil {
		t.Error("Expected exit code")
	} else if *exitResp.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", *exitResp.ExitCode)
	}

	// Release terminal (terminal already finished)
	_, err = client.ReleaseTerminal(context.Background(), acpsdk.ReleaseTerminalRequest{
		TerminalId: terminalID,
	})
	if err != nil {
		t.Errorf("Unexpected error releasing terminal: %v", err)
	}
}

// Helper functions

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStrRecursive(s, substr))
}

func containsStrRecursive(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func strPtr(s string) *string {
	return &s
}