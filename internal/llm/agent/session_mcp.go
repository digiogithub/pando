package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpclient"
	"github.com/digiogithub/pando/internal/permission"

	"github.com/mark3labs/mcp-go/mcp"
)

// Per-session MCP servers (PANDO-US-0066).
//
// ACP clients (Xcode's `xcode-tools` mcpbridge, Zed context servers, ...) pass
// MCP servers in session/new and session/load. Unlike the globally configured
// MCP servers (mcp-tools.go), whose tools spawn a fresh client per call, these
// servers keep ONE persistent, initialized client per (session, server): a
// stdio bridge such as mcpbridge is bound to the client process that launched
// it and can take several seconds to start, so paying that on every call would
// be both slow and lossy. Their tools are visible only to agent runs of the
// session that attached them (see agent.toolsForSession).

const (
	// sessionMCPDiscoveryTimeout bounds initialize + tools/list when the server
	// declares no timeout of its own. mcpbridge alone needs ~6s.
	sessionMCPDiscoveryTimeout = 45 * time.Second
	// sessionMCPCallTimeout bounds a single tool call when the server declares
	// no timeout of its own. IDE bridges run builds and tests through tool
	// calls, so the global 30s default would cut legitimate work short; the
	// run context still cancels a call the user aborts.
	sessionMCPCallTimeout = 15 * time.Minute
	// maxMCPToolNameLen is the tool-name limit most providers enforce.
	maxMCPToolNameLen = 64
)

// newSessionMCPClient builds the transport client for a session MCP server.
// Tests replace it with an in-process client.
var newSessionMCPClient = func(ctx context.Context, name string, srv config.MCPServer) (MCPClient, error) {
	return mcpclient.New(ctx, name, srv)
}

// sessionMCPServer is one live connection owned by the registry.
type sessionMCPServer struct {
	sessionID string
	name      string // sanitized; used as the tool-name prefix
	cfg       config.MCPServer

	mu     sync.Mutex
	client MCPClient
	cancel context.CancelFunc
	closed bool
}

// sessionMCPSet is everything attached to one session.
type sessionMCPSet struct {
	servers []*sessionMCPServer
	tools   []tools.BaseTool
}

var (
	// sessionMCPMu serializes attach/detach; reads go through the sync.Map.
	sessionMCPMu       sync.Mutex
	sessionMCPRegistry sync.Map // session id -> *sessionMCPSet
)

// AttachSessionMCPServers connects to every server in servers, initializes it
// and lists its tools, then exposes those tools to the session's agent runs
// only. It replaces whatever was attached to the session before (a
// session/load re-attaches). A server that fails is skipped: the successful
// ones stay attached and the failures come back joined in the error.
//
// Connections are owned by the registry, not by ctx: ctx only bounds the
// discovery, so a stdio server keeps running after the ACP request that asked
// for it has returned. DetachSessionMCPServers releases them.
func AttachSessionMCPServers(ctx context.Context, sessionID string, servers map[string]config.MCPServer, permissions permission.Service) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session MCP: empty session id")
	}

	// Connect outside the registry lock: discovery can take seconds per server.
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	set := &sessionMCPSet{}
	usedPrefixes := make(map[string]bool, len(names))
	seenTools := make(map[string]bool)
	var errs []error
	for _, rawName := range names {
		prefix := uniqueMCPPrefix(sanitizeMCPName(rawName), usedPrefixes)
		srv := &sessionMCPServer{sessionID: sessionID, name: prefix, cfg: servers[rawName]}
		listed, err := srv.connect(ctx)
		if err != nil {
			logging.Warn("session MCP: server unavailable", "session_id", sessionID, "server", rawName, "error", err)
			mcpclient.PublishWarn(prefix, fmt.Sprintf("MCP server %q from the ACP client could not be started: %v", rawName, err))
			errs = append(errs, fmt.Errorf("MCP server %q: %w", rawName, err))
			continue
		}
		set.servers = append(set.servers, srv)
		for _, t := range listed {
			tool := &sessionMCPTool{server: srv, tool: t, permissions: permissions, name: sessionMCPToolName(prefix, t.Name)}
			if seenTools[tool.name] {
				continue
			}
			seenTools[tool.name] = true
			set.tools = append(set.tools, tool)
		}
		logging.Info("session MCP: server attached", "session_id", sessionID, "server", rawName, "tools", len(listed))
	}

	sessionMCPMu.Lock()
	previous, _ := sessionMCPRegistry.Load(sessionID)
	if len(set.servers) > 0 {
		sessionMCPRegistry.Store(sessionID, set)
	} else {
		sessionMCPRegistry.Delete(sessionID)
	}
	sessionMCPMu.Unlock()

	if prev, ok := previous.(*sessionMCPSet); ok {
		prev.close()
	}
	return errors.Join(errs...)
}

// DetachSessionMCPServers closes every connection attached to the session and
// removes its tools. It is a no-op for a session with nothing attached.
func DetachSessionMCPServers(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	sessionMCPMu.Lock()
	previous, ok := sessionMCPRegistry.LoadAndDelete(sessionID)
	sessionMCPMu.Unlock()
	if !ok {
		return
	}
	if set, ok := previous.(*sessionMCPSet); ok {
		set.close()
		logging.Info("session MCP: servers detached", "session_id", sessionID, "servers", len(set.servers))
	}
}

// SessionTools returns the tools attached to the session, or nil.
func SessionTools(sessionID string) []tools.BaseTool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	value, ok := sessionMCPRegistry.Load(sessionID)
	if !ok {
		return nil
	}
	set, ok := value.(*sessionMCPSet)
	if !ok {
		return nil
	}
	return set.tools
}

// sessionMCPToolNames returns the names of the session's MCP tools, sorted.
func sessionMCPToolNames(sessionID string) []string {
	sessionTools := SessionTools(sessionID)
	if len(sessionTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(sessionTools))
	for _, t := range sessionTools {
		names = append(names, t.Info().Name)
	}
	sort.Strings(names)
	return names
}

// mergeSessionTools appends the session tools to base; a session tool replaces
// a base tool with the same name. base is never mutated.
func mergeSessionTools(base []tools.BaseTool, sessionTools []tools.BaseTool) []tools.BaseTool {
	if len(sessionTools) == 0 {
		return base
	}
	override := make(map[string]bool, len(sessionTools))
	for _, t := range sessionTools {
		override[t.Info().Name] = true
	}
	merged := make([]tools.BaseTool, 0, len(base)+len(sessionTools))
	for _, t := range base {
		if !override[t.Info().Name] {
			merged = append(merged, t)
		}
	}
	return append(merged, sessionTools...)
}

// toolsForSession is the tool set an agent run for sessionID sees: the agent's
// own tools plus the session's MCP tools. Agents configured without tools
// (title, summarizer) stay tool-less.
func (a *agent) toolsForSession(sessionID string) []tools.BaseTool {
	base := a.currentTools()
	if len(base) == 0 {
		return base
	}
	// A session with its own Xcode bridge must not also reach Xcode through a
	// global bridge server: every extra connection is another Xcode prompt
	// (PANDO-US-0068).
	if sessionHasXcodeBridge(sessionID) {
		base = withoutGlobalXcodeBridgeTools(base)
	}
	return mergeSessionTools(base, SessionTools(sessionID))
}

func (set *sessionMCPSet) close() {
	for _, srv := range set.servers {
		srv.close()
	}
}

func (s *sessionMCPServer) discoveryTimeout() time.Duration {
	return mcpclient.ResolveTimeout(s.cfg.Timeout, sessionMCPDiscoveryTimeout)
}

func (s *sessionMCPServer) callTimeout() time.Duration {
	return mcpclient.ResolveTimeout(s.cfg.Timeout, sessionMCPCallTimeout)
}

// connect opens and initializes the client and lists the server's tools.
func (s *sessionMCPServer) connect(ctx context.Context) ([]mcp.Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.openLocked(ctx); err != nil {
		return nil, err
	}
	listCtx, cancel := mcpclient.WithTimeout(ctx, s.discoveryTimeout())
	result, err := s.client.ListTools(listCtx, mcp.ListToolsRequest{})
	cancel()
	if err != nil {
		s.dropClientLocked()
		return nil, fmt.Errorf("list tools: %w", err)
	}
	return result.Tools, nil
}

// openLocked creates, starts and initializes a fresh client. Callers hold s.mu.
// The client lives on a registry-owned background context; waitCtx only
// bounds how long this call waits for the handshake.
func (s *sessionMCPServer) openLocked(waitCtx context.Context) error {
	if s.closed {
		return errors.New("session MCP server was detached")
	}
	s.dropClientLocked()

	clientCtx, cancel := context.WithCancel(context.Background())
	c, err := newSessionMCPClient(clientCtx, s.name, s.cfg)
	if err != nil {
		cancel()
		return fmt.Errorf("connect: %w", err)
	}
	// SSE and streamable-HTTP clients must be started before initialize (the
	// stdio constructor already did it; Start is idempotent in mcp-go).
	if starter, ok := c.(interface{ Start(context.Context) error }); ok {
		if err := starter.Start(clientCtx); err != nil {
			_ = c.Close()
			cancel()
			return fmt.Errorf("start transport: %w", err)
		}
	}

	handshakeCfg := s.cfg
	handshakeCfg.Timeout = s.discoveryTimeout().String()
	if _, err := mcpclient.Handshake(waitCtx, c, s.name, handshakeCfg, "Pando"); err != nil {
		_ = c.Close()
		cancel()
		return fmt.Errorf("initialize: %w", err)
	}
	s.client = c
	s.cancel = cancel
	return nil
}

func (s *sessionMCPServer) dropClientLocked() {
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *sessionMCPServer) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.dropClientLocked()
}

// activeClient returns the live client, reconnecting when a previous failure
// dropped it.
func (s *sessionMCPServer) activeClient(ctx context.Context) (MCPClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	if err := s.openLocked(ctx); err != nil {
		return nil, err
	}
	return s.client, nil
}

// invalidate drops c if it is still the current client, so the next call
// reconnects.
func (s *sessionMCPServer) invalidate(c MCPClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == c {
		s.dropClientLocked()
	}
}

// call runs a tool on the persistent client. When the call fails because the
// transport died (the bridge process exited, the connection closed), it
// reconnects once and retries.
func (s *sessionMCPServer) call(ctx context.Context, toolName, input string) tools.ToolResponse {
	timeout := s.callTimeout()
	for attempt := 0; ; attempt++ {
		c, err := s.activeClient(ctx)
		if err != nil {
			return mcpToolErrorResponse(s.name, err.Error())
		}
		response, callErr := invokeMCPTool(ctx, c, s.name, timeout, toolName, input)
		if callErr == nil {
			return response
		}
		if attempt > 0 || ctx.Err() != nil || !isMCPTransportError(callErr) {
			return mcpOperationError(s.name, toolName, "call", callErr, timeout)
		}
		logging.Warn("session MCP: transport failed, reconnecting", "session_id", s.sessionID, "server", s.name, "tool", toolName, "error", callErr)
		s.invalidate(c)
	}
}

// isMCPTransportError reports whether err means the connection itself is gone
// (as opposed to a tool or protocol error the server answered with).
func isMCPTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"transport closed", "transport not started", "closed pipe", "broken pipe", "connection reset", "connection refused", "eof", "use of closed"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// sessionMCPTool is a tool advertised by a session MCP server.
type sessionMCPTool struct {
	server      *sessionMCPServer
	tool        mcp.Tool
	permissions permission.Service
	name        string
}

func (t *sessionMCPTool) Info() tools.ToolInfo {
	required := t.tool.InputSchema.Required
	if required == nil {
		required = make([]string, 0)
	}
	return tools.ToolInfo{
		Name:        t.name,
		Description: t.tool.Description,
		Parameters:  t.tool.InputSchema.Properties,
		Required:    required,
	}
}

func (t *sessionMCPTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	sessionID, messageID := tools.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return tools.ToolResponse{}, fmt.Errorf("session ID and message ID are required for running MCP tool %s", t.name)
	}
	if t.permissions != nil {
		allowed := t.permissions.RequestWithContext(ctx, permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        config.WorkingDirectory(),
			ToolName:    t.name,
			Action:      "execute",
			Description: fmt.Sprintf("execute %s with the following parameters: %s", t.name, params.Input),
			Params:      params.Input,
		})
		if !allowed {
			return tools.NewTextErrorResponse("permission denied"), nil
		}
	}

	response := t.server.call(ctx, t.tool.Name, params.Input)
	if cache := tools.GetSessionCache(ctx); cache != nil {
		response = tools.InterceptToolResponse(cache, params.ID, t.name, response)
	}
	return response, nil
}

// sanitizeMCPName keeps [A-Za-z0-9_-] and replaces anything else with '_'.
func sanitizeMCPName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "mcp"
	}
	return b.String()
}

func uniqueMCPPrefix(prefix string, used map[string]bool) string {
	candidate := prefix
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s_%d", prefix, i)
	}
	used[candidate] = true
	return candidate
}

// sessionMCPToolName renders "<server>_<tool>" (like the global MCP tools),
// sanitized and capped at maxMCPToolNameLen.
func sessionMCPToolName(prefix, toolName string) string {
	name := prefix + "_" + sanitizeMCPName(toolName)
	if len(name) > maxMCPToolNameLen {
		name = name[:maxMCPToolNameLen]
	}
	return name
}
