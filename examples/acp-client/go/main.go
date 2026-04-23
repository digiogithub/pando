package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	fmt.Println("🚀 Pando ACP Client Example")
	fmt.Println("===========================\n")

	// Create temporary workspace
	workspace, err := os.MkdirTemp("", "pando-example-*")
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	fmt.Printf("📁 Workspace: %s\n", workspace)

	// Start Pando ACP server
	fmt.Println("🔧 Starting Pando ACP server...")
	cmd := exec.Command("pando", "--acp-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start pando: %w", err)
	}
	defer cmd.Process.Kill()
	fmt.Println("✓ Pando server started\n")

	// Create client
	client := NewExampleACPClient(workspace)
	conn := acpsdk.NewClientSideConnection(client, stdin, stdout)

	ctx := context.Background()

	// Initialize connection
	fmt.Println("🔌 Initializing connection...")
	initResp, err := conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
		ClientInfo: &acpsdk.Implementation{
			Name:    "pando-go-example",
			Version: "1.0.0",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	fmt.Printf("✓ Connected to %s v%s\n", initResp.AgentInfo.Name, initResp.AgentInfo.Version)
	fmt.Printf("  Protocol version: %d\n\n", initResp.ProtocolVersion)

	// Create session
	fmt.Println("📋 Creating session...")
	sessionResp, err := conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd: workspace,
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	fmt.Printf("✓ Session created: %s\n\n", sessionResp.SessionId)

	// Example 1: Create a simple Python file
	fmt.Println("Example 1: Create a Python hello world program")
	fmt.Println("-----------------------------------------------")
	prompt1 := "Create a simple hello world program in Python called hello.py"
	promptResp1, err := sendPrompt(ctx, conn, sessionResp.SessionId, prompt1)
	if err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}
	displayPromptResponse(promptResp1)

	// Wait a moment for file to be created
	time.Sleep(1 * time.Second)

	// Verify file was created
	if _, err := os.Stat(filepath.Join(workspace, "hello.py")); err == nil {
		fmt.Println("✓ File hello.py was created successfully\n")
	}

	// Example 2: Run the Python program
	fmt.Println("Example 2: Run the Python program")
	fmt.Println("----------------------------------")
	prompt2 := "Run the hello.py program and show me the output"
	promptResp2, err := sendPrompt(ctx, conn, sessionResp.SessionId, prompt2)
	if err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}
	displayPromptResponse(promptResp2)

	// Example 3: Create multiple files
	fmt.Println("\nExample 3: Create a simple web server")
	fmt.Println("--------------------------------------")
	prompt3 := "Create a simple HTTP web server in Go that responds with 'Hello, World!' on port 8080"
	promptResp3, err := sendPrompt(ctx, conn, sessionResp.SessionId, prompt3)
	if err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}
	displayPromptResponse(promptResp3)

	// List files created in workspace
	fmt.Println("\n📂 Files in workspace:")
	files, err := os.ReadDir(workspace)
	if err == nil {
		for _, file := range files {
			fmt.Printf("  - %s\n", file.Name())
		}
	}

	fmt.Println("\n✅ Examples completed successfully!")
	return nil
}

func sendPrompt(ctx context.Context, conn acpsdk.ClientSideConnection, sessionID, prompt string) (acpsdk.PromptResponse, error) {
	fmt.Printf("💭 Prompt: %s\n", prompt)

	resp, err := conn.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: sessionID,
		Prompt: []acpsdk.ContentBlock{
			acpsdk.TextBlock(prompt),
		},
	})
	if err != nil {
		return acpsdk.PromptResponse{}, err
	}

	return resp, nil
}

func displayPromptResponse(resp acpsdk.PromptResponse) {
	fmt.Println("📝 Response:")
	for _, block := range resp.Message {
		if textBlock, ok := block.(acpsdk.TextContentBlock); ok {
			fmt.Printf("   %s\n", textBlock.Text)
		}
	}
}

// ExampleACPClient implements the ACP client interface
type ExampleACPClient struct {
	workspace string
	terminals map[string]*exec.Cmd
}

func NewExampleACPClient(workspace string) *ExampleACPClient {
	return &ExampleACPClient{
		workspace: workspace,
		terminals: make(map[string]*exec.Cmd),
	}
}

// ReadTextFile reads a text file from the workspace
func (c *ExampleACPClient) ReadTextFile(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	fmt.Printf("  [CLIENT] Reading file: %s\n", req.Path)

	path := filepath.Join(c.workspace, req.Path)

	// Security: ensure path is within workspace
	absPath, err := filepath.Abs(path)
	if err != nil {
		return acpsdk.ReadTextFileResponse{}, fmt.Errorf("invalid path: %w", err)
	}
	absWorkspace, _ := filepath.Abs(c.workspace)
	if !filepath.HasPrefix(absPath, absWorkspace) {
		return acpsdk.ReadTextFileResponse{}, fmt.Errorf("path outside workspace")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return acpsdk.ReadTextFileResponse{}, fmt.Errorf("failed to read file: %w", err)
	}

	return acpsdk.ReadTextFileResponse{
		Content: string(content),
	}, nil
}

// WriteTextFile writes content to a file
func (c *ExampleACPClient) WriteTextFile(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	fmt.Printf("  [CLIENT] Writing file: %s (%d bytes)\n", req.Path, len(req.Content))

	path := filepath.Join(c.workspace, req.Path)

	// Security: ensure path is within workspace
	absPath, err := filepath.Abs(path)
	if err != nil {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("invalid path: %w", err)
	}
	absWorkspace, _ := filepath.Abs(c.workspace)
	if !filepath.HasPrefix(absPath, absWorkspace) {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("path outside workspace")
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	return acpsdk.WriteTextFileResponse{}, nil
}

// CreateTerminal creates a new terminal session
func (c *ExampleACPClient) CreateTerminal(ctx context.Context, req acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	fmt.Printf("  [CLIENT] Creating terminal: %s %v\n", req.Command, req.Args)

	terminalID := fmt.Sprintf("term-%d", len(c.terminals))

	cmd := exec.Command(req.Command, req.Args...)
	cmd.Dir = c.workspace
	if req.Cwd != nil {
		cmd.Dir = filepath.Join(c.workspace, *req.Cwd)
	}

	if err := cmd.Start(); err != nil {
		return acpsdk.CreateTerminalResponse{}, fmt.Errorf("failed to start command: %w", err)
	}

	c.terminals[terminalID] = cmd

	return acpsdk.CreateTerminalResponse{
		TerminalId: terminalID,
	}, nil
}

// TerminalOutput gets output from a terminal
func (c *ExampleACPClient) TerminalOutput(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	// In a real implementation, this would read from the terminal's output buffer
	return acpsdk.TerminalOutputResponse{
		Output: "",
	}, nil
}

// WaitForTerminalExit waits for a terminal to exit
func (c *ExampleACPClient) WaitForTerminalExit(ctx context.Context, req acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	cmd, ok := c.terminals[req.TerminalId]
	if !ok {
		return acpsdk.WaitForTerminalExitResponse{}, fmt.Errorf("terminal not found")
	}

	err := cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
	}

	delete(c.terminals, req.TerminalId)

	return acpsdk.WaitForTerminalExitResponse{
		ExitCode: &exitCode,
	}, nil
}

// KillTerminalCommand kills a running terminal command
func (c *ExampleACPClient) KillTerminalCommand(ctx context.Context, req acpsdk.KillTerminalCommandRequest) (acpsdk.KillTerminalCommandResponse, error) {
	cmd, ok := c.terminals[req.TerminalId]
	if !ok {
		return acpsdk.KillTerminalCommandResponse{}, fmt.Errorf("terminal not found")
	}

	if err := cmd.Process.Kill(); err != nil {
		return acpsdk.KillTerminalCommandResponse{}, fmt.Errorf("failed to kill process: %w", err)
	}

	return acpsdk.KillTerminalCommandResponse{}, nil
}

// ReleaseTerminal releases terminal resources
func (c *ExampleACPClient) ReleaseTerminal(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	delete(c.terminals, req.TerminalId)
	return acpsdk.ReleaseTerminalResponse{}, nil
}

// RequestPermission handles permission requests
func (c *ExampleACPClient) RequestPermission(ctx context.Context, req acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	// Auto-approve all permissions in this example
	if len(req.Options) > 0 {
		return acpsdk.RequestPermissionResponse{
			Outcome: acpsdk.NewRequestPermissionOutcomeSelected(req.Options[0].OptionId),
		}, nil
	}

	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.NewRequestPermissionOutcomeCancelled(),
	}, nil
}
