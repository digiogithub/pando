package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/version"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	DefaultOperationTimeout = 30 * time.Second
	DefaultDiscoveryTimeout = 15 * time.Second
)

type Client interface {
	Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

type stdioLogDrainingClient struct {
	inner  *client.Client
	server string
}

func New(ctx context.Context, serverName string, srv config.MCPServer) (Client, error) {
	resolved, err := config.ResolveMCPServerSecrets(srv)
	if err != nil {
		return nil, fmt.Errorf("resolve MCP server %s secrets: %w", serverName, err)
	}

	switch resolved.Type {
	case config.MCPStdio:
		c, err := client.NewStdioMCPClient(resolved.Command, resolved.Env, resolved.Args...)
		if err != nil {
			return nil, err
		}
		wrapped := &stdioLogDrainingClient{inner: c, server: serverName}
		wrapped.startLogDrainer(ctx)
		return wrapped, nil
	case config.MCPSse:
		return client.NewSSEMCPClient(resolved.URL, client.WithHeaders(resolved.Headers))
	case config.MCPStreamableHTTP:
		return client.NewStreamableHttpClient(resolved.URL, transport.WithHTTPHeaders(resolved.Headers))
	default:
		return nil, fmt.Errorf("unknown MCP server type: %q", resolved.Type)
	}
}

func ResolveTimeout(raw string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || timeout <= 0 {
		return fallback
	}
	return timeout
}

func WithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithCancel(parent)
		}
		if remaining <= timeout {
			return context.WithCancel(parent)
		}
	}
	return context.WithTimeout(parent, timeout)
}

func BuildTimeoutError(serverName, operation string, timeout time.Duration) error {
	return fmt.Errorf("MCP server %q timed out during %s after %s", serverName, operation, timeout)
}

func BuildCallError(serverName, toolName, operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("MCP server %q %s for tool %q failed: %w", serverName, operation, toolName, err)
}

func PublishError(serverName, message string) {
	logging.Error("MCP operation failed", "server", serverName, "message", message)
	notify.Error(notify.SourceTool, message)
}

func PublishWarn(serverName, message string) {
	logging.Warn("MCP warning", "server", serverName, "message", message)
	notify.Warn(notify.SourceTool, message, 15*time.Second)
}

func BuildInitializeRequest(clientName string) mcp.InitializeRequest {
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    clientName,
		Version: version.Version,
	}
	return initReq
}

func (c *stdioLogDrainingClient) Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	return c.inner.Initialize(ctx, req)
}

func (c *stdioLogDrainingClient) ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return c.inner.ListTools(ctx, req)
}

func (c *stdioLogDrainingClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return c.inner.CallTool(ctx, req)
}

func (c *stdioLogDrainingClient) Close() error {
	return c.inner.Close()
}

func (c *stdioLogDrainingClient) startLogDrainer(ctx context.Context) {
	stderr, ok := client.GetStderr(c.inner)
	if !ok || stderr == nil {
		return
	}
	go drainStdioLogs(ctx, c.server, stderr)
}

func drainStdioLogs(ctx context.Context, serverName string, stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if looksLikeJSONRPCLine(line) {
			logging.Debug("MCP stdio stderr emitted JSON-RPC-like line", "server", serverName, "line", line)
			continue
		}
		logging.Warn("MCP stdio server log", "server", serverName, "line", line)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		logging.Debug("MCP stdio log drain stopped", "server", serverName, "error", err)
	}
}

func looksLikeJSONRPCLine(line string) bool {
	var probe struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		return false
	}
	return probe.JSONRPC == "2.0" && probe.Method != ""
}
