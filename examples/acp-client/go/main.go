package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

	workspace, err := os.MkdirTemp("", "pando-example-*")
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	fmt.Printf("📁 Workspace: %s\n", workspace)

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

	client := NewExampleACPClient(workspace)
	conn := acpsdk.NewClientSideConnection(client, stdin, stdout)

	ctx := context.Background()

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

	fmt.Println("📋 Creating session...")
	sessionResp, err := conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd: workspace,
	})
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	fmt.Printf("✓ Session created: %s\n\n", sessionResp.SessionId)

	fmt.Println("Example 1: Create a Python hello world program")
	fmt.Println("-----------------------------------------------")
	prompt1 := "Create a simple hello world program in Python called hello.py"
	promptResp1, err := sendPrompt(ctx, conn, sessionResp.SessionId, prompt1)
	if err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}
	displayPromptResponse(promptResp1)

	if _, err := os.Stat(filepath.Join(workspace, "hello.py")); err == nil {
		fmt.Println("✓ File hello.py was created successfully\n")
	}

	fmt.Println("Example 2: Run the Python program")
	fmt.Println("----------------------------------")
	prompt2 := "Run the hello.py program and show me the output"
	promptResp2, err := sendPrompt(ctx, conn, sessionResp.SessionId, prompt2)
	if err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}
	displayPromptResponse(promptResp2)

	fmt.Println("\nExample 3: Create a simple web server")
	fmt.Println("--------------------------------------")
	prompt3 := "Create a simple HTTP web server in Go that responds with 'Hello, World!' on port 8080"
	promptResp3, err := sendPrompt(ctx, conn, sessionResp.SessionId, prompt3)
	if err != nil {
		return fmt.Errorf("failed to send prompt: %w", err)
	}
	displayPromptResponse(promptResp3)

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

func sendPrompt(ctx context.Context, conn *acpsdk.ClientSideConnection, sessionID acpsdk.SessionId, prompt string) (acpsdk.PromptResponse, error) {
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
	fmt.Printf("📝 Prompt completed (stopReason=%s)\n", resp.StopReason)
}

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

func (c *ExampleACPClient) ReadTextFile(ctx context.Context, req acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	fmt.Printf("  [CLIENT] Reading file: %s\n", req.Path)

	path := filepath.Join(c.workspace, req.Path)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return acpsdk.ReadTextFileResponse{}, fmt.Errorf("invalid path: %w", err)
	}
	absWorkspace, _ := filepath.Abs(c.workspace)
	if !strings.HasPrefix(absPath, absWorkspace+string(os.PathSeparator)) && absPath != absWorkspace {
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

func (c *ExampleACPClient) WriteTextFile(ctx context.Context, req acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	fmt.Printf("  [CLIENT] Writing file: %s (%d bytes)\n", req.Path, len(req.Content))

	path := filepath.Join(c.workspace, req.Path)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("invalid path: %w", err)
	}
	absWorkspace, _ := filepath.Abs(c.workspace)
	if !strings.HasPrefix(absPath, absWorkspace+string(os.PathSeparator)) && absPath != absWorkspace {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("path outside workspace")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		return acpsdk.WriteTextFileResponse{}, fmt.Errorf("failed to write file: %w", err)
	}

	return acpsdk.WriteTextFileResponse{}, nil
}

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

func (c *ExampleACPClient) TerminalOutput(ctx context.Context, req acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{
		Output: "",
	}, nil
}

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

func (c *ExampleACPClient) KillTerminal(ctx context.Context, req acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	cmd, ok := c.terminals[req.TerminalId]
	if !ok {
		return acpsdk.KillTerminalResponse{}, fmt.Errorf("terminal not found")
	}

	if err := cmd.Process.Kill(); err != nil {
		return acpsdk.KillTerminalResponse{}, fmt.Errorf("failed to kill process: %w", err)
	}

	return acpsdk.KillTerminalResponse{}, nil
}

func (c *ExampleACPClient) ReleaseTerminal(ctx context.Context, req acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	delete(c.terminals, req.TerminalId)
	return acpsdk.ReleaseTerminalResponse{}, nil
}

func (c *ExampleACPClient) RequestPermission(ctx context.Context, req acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	if len(req.Options) > 0 {
		return acpsdk.RequestPermissionResponse{
			Outcome: acpsdk.NewRequestPermissionOutcomeSelected(req.Options[0].OptionId),
		}, nil
	}

	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.NewRequestPermissionOutcomeCancelled(),
	}, nil
}

// SessionUpdate handles real-time session updates streamed from the agent.
func (c *ExampleACPClient) SessionUpdate(ctx context.Context, params acpsdk.SessionNotification) error {
	u := params.Update
	switch {
	case u.AgentMessageChunk != nil:
		if u.AgentMessageChunk.Content.Text != nil {
			fmt.Print(u.AgentMessageChunk.Content.Text.Text)
		}
	case u.ToolCall != nil:
		fmt.Printf("\n🔧 %s (%s)\n", u.ToolCall.Title, u.ToolCall.Status)
	case u.ToolCallUpdate != nil:
		status := "unknown"
		if u.ToolCallUpdate.Status != nil {
			status = string(*u.ToolCallUpdate.Status)
		}
		fmt.Printf("\n🔧 Tool call `%s` updated: %v\n\n", u.ToolCallUpdate.ToolCallId, status)
	}
	return nil
}