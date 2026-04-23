package acp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// ACPClientCallbacks defines the interface for calling back to the client.
type ACPClientCallbacks interface {
	ReadTextFile(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error)
	WriteTextFile(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error)
	CreateTerminal(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error)
	TerminalOutput(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error)
	WaitForTerminalExit(ctx context.Context, req acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error)
	KillTerminal(ctx context.Context, req acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error)
	ReleaseTerminal(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error)
}

// ACPClientConnection wraps a client connection and provides methods to call
// back to the client for file operations and terminal execution.
// This is the INVERSE of MesnadaACPClient - it's used by Pando when acting as
// an ACP server to call methods on the connected client.
type ACPClientConnection struct {
	sessionID acpsdk.SessionId
	conn      ACPClientCallbacks // Connection to the client
	workDir   string

	// For logging
	logFile *os.File
}

// NewACPClientConnection creates a new client connection wrapper.
func NewACPClientConnection(sessionID acpsdk.SessionId, conn ACPClientCallbacks, workDir string, logFile *os.File) *ACPClientConnection {
	return &ACPClientConnection{
		sessionID: sessionID,
		conn:      conn,
		workDir:   workDir,
		logFile:   logFile,
	}
}

// ReadTextFile calls the client to read a file from their filesystem.
func (c *ACPClientConnection) ReadTextFile(ctx context.Context, path string) (string, error) {
	// Validate path security
	if err := c.validatePath(path); err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] ReadTextFile denied: %v\n", err)
		}
		return "", err
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] ReadTextFile: %s\n", path)
	}

	// Call the client's ReadTextFile method
	resp, err := c.conn.ReadTextFile(ctx, acpsdk.ReadTextFileRequest{
		Path: path,
	})
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] ReadTextFile error: %v\n", err)
		}
		return "", fmt.Errorf("failed to read file from client: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] ReadTextFile success: %d bytes\n", len(resp.Content))
	}

	return resp.Content, nil
}

// WriteTextFile calls the client to write a file to their filesystem.
func (c *ACPClientConnection) WriteTextFile(ctx context.Context, path string, content string) error {
	// Validate path security
	if err := c.validatePath(path); err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WriteTextFile denied: %v\n", err)
		}
		return err
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WriteTextFile: %s (%d bytes)\n", path, len(content))
	}

	// Call the client's WriteTextFile method
	_, err := c.conn.WriteTextFile(ctx, acpsdk.WriteTextFileRequest{
		Path:    path,
		Content: content,
	})
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WriteTextFile error: %v\n", err)
		}
		return fmt.Errorf("failed to write file to client: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WriteTextFile success\n")
	}

	return nil
}

// CreateTerminal calls the client to create a terminal and execute a command.
func (c *ACPClientConnection) CreateTerminal(ctx context.Context, command string, args []string, cwd string) (string, error) {
	// Validate cwd if provided
	if cwd != "" {
		if err := c.validatePath(cwd); err != nil {
			if c.logFile != nil {
				fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] CreateTerminal denied: invalid cwd: %v\n", err)
			}
			return "", err
		}
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] CreateTerminal: cmd=%s, args=%v, cwd=%s\n", command, args, cwd)
	}

	// Prepare request
	req := acpsdk.CreateTerminalRequest{
		Command: command,
		Args:    args,
	}
	if cwd != "" {
		req.Cwd = &cwd
	}

	// Call the client's CreateTerminal method
	resp, err := c.conn.CreateTerminal(ctx, req)
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] CreateTerminal error: %v\n", err)
		}
		return "", fmt.Errorf("failed to create terminal on client: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] CreateTerminal success: terminalId=%s\n", resp.TerminalId)
	}

	return resp.TerminalId, nil
}

// TerminalOutput calls the client to get the current output from a terminal.
func (c *ACPClientConnection) TerminalOutput(ctx context.Context, terminalID string) (string, error) {
	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] TerminalOutput: terminalId=%s\n", terminalID)
	}

	// Call the client's TerminalOutput method
	resp, err := c.conn.TerminalOutput(ctx, acpsdk.TerminalOutputRequest{
		TerminalId: terminalID,
	})
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] TerminalOutput error: %v\n", err)
		}
		return "", fmt.Errorf("failed to get terminal output from client: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] TerminalOutput success: %d bytes\n", len(resp.Output))
	}

	return resp.Output, nil
}

// WaitForTerminalExit calls the client to wait for a terminal to exit.
func (c *ACPClientConnection) WaitForTerminalExit(ctx context.Context, terminalID string) (*int, error) {
	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WaitForTerminalExit: terminalId=%s\n", terminalID)
	}

	// Call the client's WaitForTerminalExit method
	resp, err := c.conn.WaitForTerminalExit(ctx, acpsdk.WaitForTerminalExitRequest{
		TerminalId: terminalID,
	})
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WaitForTerminalExit error: %v\n", err)
		}
		return nil, fmt.Errorf("failed to wait for terminal exit on client: %w", err)
	}

	if c.logFile != nil {
		if resp.ExitCode != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WaitForTerminalExit success: exitCode=%d\n", *resp.ExitCode)
		} else {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] WaitForTerminalExit success: no exit code\n")
		}
	}

	return resp.ExitCode, nil
}

// KillTerminal calls the client to kill a running terminal.
func (c *ACPClientConnection) KillTerminal(ctx context.Context, terminalID string) error {
	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] KillTerminal: terminalId=%s\n", terminalID)
	}

	// Call the client's KillTerminal method
	_, err := c.conn.KillTerminal(ctx, acpsdk.KillTerminalRequest{
		TerminalId: terminalID,
	})
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] KillTerminal error: %v\n", err)
		}
		return fmt.Errorf("failed to kill terminal on client: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] KillTerminal success\n")
	}

	return nil
}

// ReleaseTerminal calls the client to release a terminal without waiting for it to exit.
func (c *ACPClientConnection) ReleaseTerminal(ctx context.Context, terminalID string) error {
	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] ReleaseTerminal: terminalId=%s\n", terminalID)
	}

	// Call the client's ReleaseTerminal method
	_, err := c.conn.ReleaseTerminal(ctx, acpsdk.ReleaseTerminalRequest{
		TerminalId: terminalID,
	})
	if err != nil {
		if c.logFile != nil {
			fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] ReleaseTerminal error: %v\n", err)
		}
		return fmt.Errorf("failed to release terminal on client: %w", err)
	}

	if c.logFile != nil {
		fmt.Fprintf(c.logFile, "[CLIENT CALLBACK] ReleaseTerminal success\n")
	}

	return nil
}

// validatePath validates that a path is safe and doesn't attempt path traversal.
// This prevents security issues where malicious prompts might try to access
// files outside the workspace.
func (c *ACPClientConnection) validatePath(path string) error {
	// Prevent absolute paths
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths not allowed: %s", path)
	}

	// Clean the path to resolve any ".." or "." components
	cleanPath := filepath.Clean(path)

	// Check for path traversal attempts with ".."
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}

	// Additional check: ensure the cleaned path doesn't start with ".."
	if strings.HasPrefix(cleanPath, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}

	return nil
}

// GetSessionID returns the session ID for this connection.
func (c *ACPClientConnection) GetSessionID() acpsdk.SessionId {
	return c.sessionID
}

// GetWorkDir returns the working directory for this connection.
func (c *ACPClientConnection) GetWorkDir() string {
	return c.workDir
}
