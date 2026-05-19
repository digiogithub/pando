package mcpgateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpclient"

	"github.com/mark3labs/mcp-go/mcp"
)

// pooledMCPClient is the interface for MCP clients managed by the pool.
// It is a subset of the full MCPClient interface — only the operations needed
// during a gateway tool call (no ListTools required here).
type pooledMCPClient interface {
	Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error)
	CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

// clientEntry holds a cached MCP client alongside bookkeeping metadata.
type clientEntry struct {
	client     pooledMCPClient
	serverName string
	createdAt  time.Time
	lastUsed   time.Time
	cancel     context.CancelFunc
}

// MCPClientPool manages long-lived MCP client connections to avoid creating a
// new subprocess / HTTP client on every tool call. Clients are keyed by server
// name and are initialized (handshake done) on first use. A client is evicted
// when an error occurs, so the next caller will transparently reconnect.
type MCPClientPool struct {
	mu      sync.Mutex
	clients map[string]*clientEntry
}

// NewClientPool creates an empty MCPClientPool.
func NewClientPool() *MCPClientPool {
	return &MCPClientPool{
		clients: make(map[string]*clientEntry),
	}
}

// GetOrCreate returns a cached and already-initialized MCP client for the
// given server, creating and initializing one if none exists. The caller must
// NOT close the returned client — the pool owns its lifecycle.
func (p *MCPClientPool) GetOrCreate(ctx context.Context, serverName string, srv config.MCPServer) (pooledMCPClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.clients[serverName]; ok {
		entry.lastUsed = time.Now()
		logging.Debug("MCP client pool: reusing existing client", "server", serverName)
		return entry.client, nil
	}

	clientCtx, clientCancel := context.WithCancel(ctx)
	c, err := newPooledClient(clientCtx, serverName, srv)
	if err != nil {
		clientCancel()
		return nil, fmt.Errorf("create MCP client for %q: %w", serverName, err)
	}

	timeout := mcpclient.ResolveTimeout(srv.Timeout, mcpclient.DefaultDiscoveryTimeout)
	initReq := mcpclient.BuildInitializeRequest("pando-gateway")
	initCtx, initCancel := mcpclient.WithTimeout(ctx, timeout)
	if _, err := c.Initialize(initCtx, initReq); err != nil {
		initCancel()
		clientCancel()
		_ = c.Close()
		return nil, fmt.Errorf("initialize MCP client for %q: %w", serverName, err)
	}
	initCancel()

	now := time.Now()
	p.clients[serverName] = &clientEntry{
		client:     c,
		serverName: serverName,
		createdAt:  now,
		lastUsed:   now,
		cancel:     clientCancel,
	}
	logging.Debug("MCP client pool: created new client", "server", serverName)
	return c, nil
}

// Evict closes and removes the cached client for a server. Called after a
// call error so that the next invocation creates a fresh connection.
func (p *MCPClientPool) Evict(serverName string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry, ok := p.clients[serverName]
	if !ok {
		return
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	if err := entry.client.Close(); err != nil {
		logging.Debug("MCP client pool: error closing evicted client", "server", serverName, "error", err)
	}
	delete(p.clients, serverName)
	logging.Debug("MCP client pool: evicted client", "server", serverName)
}

// StopAll closes all pooled clients and resets the pool.
func (p *MCPClientPool) StopAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for name, entry := range p.clients {
		if entry.cancel != nil {
			entry.cancel()
		}
		if err := entry.client.Close(); err != nil {
			logging.Debug("MCP client pool: error stopping client", "server", name, "error", err)
		}
	}
	p.clients = make(map[string]*clientEntry)
	logging.Debug("MCP client pool: all clients stopped")
}

// newPooledClient constructs a concrete MCP client based on the server type.
func newPooledClient(ctx context.Context, serverName string, srv config.MCPServer) (pooledMCPClient, error) {
	return mcpclient.New(ctx, serverName, srv)
}
