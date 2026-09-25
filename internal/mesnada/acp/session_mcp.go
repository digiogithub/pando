package acp

import (
	"context"
	"sort"
	"strings"
	"time"

	acpsdk "github.com/madeindigio/acp-go-sdk"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// Transport kinds of a SessionMCPServer.
const (
	SessionMCPStdio = "stdio"
	SessionMCPHTTP  = "http"
	SessionMCPSSE   = "sse"
)

// sessionMCPReadyTimeout bounds how long a prompt waits for the session's MCP
// servers to finish connecting. Discovery of a slow bridge (Xcode's mcpbridge
// needs ~6s) normally completes well inside it; past it the prompt runs
// without the tools that are not ready yet.
const sessionMCPReadyTimeout = 20 * time.Second

// SessionMCPServer is an MCP server an ACP client asked Pando to use for one
// session. It is a plain struct so the agent-service implementation does not
// depend on the ACP SDK types.
type SessionMCPServer struct {
	Name    string
	Type    string // SessionMCPStdio, SessionMCPHTTP or SessionMCPSSE
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
}

// ConvertACPMCPServers converts the SDK union type into SessionMCPServer
// values, skipping empty entries.
func ConvertACPMCPServers(servers []acpsdk.McpServer) []SessionMCPServer {
	out := make([]SessionMCPServer, 0, len(servers))
	for _, s := range servers {
		switch {
		case s.Stdio != nil:
			env := make(map[string]string, len(s.Stdio.Env))
			for _, e := range s.Stdio.Env {
				if e.Name != "" {
					env[e.Name] = e.Value
				}
			}
			out = append(out, SessionMCPServer{
				Name:    s.Stdio.Name,
				Type:    SessionMCPStdio,
				Command: s.Stdio.Command,
				Args:    append([]string(nil), s.Stdio.Args...),
				Env:     env,
			})
		case s.Http != nil:
			out = append(out, SessionMCPServer{
				Name:    s.Http.Name,
				Type:    SessionMCPHTTP,
				URL:     s.Http.Url,
				Headers: headersToMap(s.Http.Headers),
			})
		case s.Sse != nil:
			out = append(out, SessionMCPServer{
				Name:    s.Sse.Name,
				Type:    SessionMCPSSE,
				URL:     s.Sse.Url,
				Headers: headersToMap(s.Sse.Headers),
			})
		}
	}
	return out
}

// ToConfig converts the server into the config.MCPServer internal/mcpclient
// understands. Stdio env entries become "K=V" pairs appended to Pando's own
// environment by the client.
func (s SessionMCPServer) ToConfig() config.MCPServer {
	switch s.Type {
	case SessionMCPHTTP, SessionMCPSSE:
		mcpType := config.MCPStreamableHTTP
		if s.Type == SessionMCPSSE {
			mcpType = config.MCPSse
		}
		var headers map[string]string
		if len(s.Headers) > 0 {
			headers = make(map[string]string, len(s.Headers))
			for k, v := range s.Headers {
				headers[k] = v
			}
		}
		return config.MCPServer{Type: mcpType, URL: s.URL, Headers: headers}
	default:
		keys := make([]string, 0, len(s.Env))
		for k := range s.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		env := make([]string, 0, len(keys))
		for _, k := range keys {
			env = append(env, k+"="+s.Env[k])
		}
		return config.MCPServer{
			Type:    config.MCPStdio,
			Command: s.Command,
			Args:    append([]string(nil), s.Args...),
			Env:     env,
			// Servers handed over by the ACP client (Xcode, Zed, VS Code, ...)
			// for this session are trusted by the IDE that started it and
			// commonly need to reach back into the IDE's own process (IPC,
			// Unix sockets, PID-based identity checks such as Xcode's
			// mcpbridge) that the host sandbox would hide or block. Keep them
			// out of it regardless of the global Sandbox.ExtendTo policy; see
			// config.MCPServer.NoSandbox / SandboxExempt.
			NoSandbox: true,
		}
	}
}

// SessionMCPServerConfigs converts servers into the name-keyed map the agent
// package attaches. An unnamed server is called "mcp".
func SessionMCPServerConfigs(servers []SessionMCPServer) map[string]config.MCPServer {
	out := make(map[string]config.MCPServer, len(servers))
	for _, s := range servers {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = "mcp"
		}
		out[name] = s.ToConfig()
	}
	return out
}

func headersToMap(headers []acpsdk.HttpHeader) map[string]string {
	m := make(map[string]string, len(headers))
	for _, h := range headers {
		if h.Name != "" {
			m[h.Name] = h.Value
		}
	}
	return m
}

func sessionMCPServerNames(servers []SessionMCPServer) string {
	names := make([]string, 0, len(servers))
	for _, s := range servers {
		names = append(names, s.Name+"("+s.Type+")")
	}
	return strings.Join(names, ",")
}

// attachSessionMCPServers starts connecting the client's MCP servers for the
// session in the background and records a readiness channel on the session
// that Prompt waits on. The attach replaces any previous set (session/load).
func (a *PandoACPAgent) attachSessionMCPServers(acpSession *ACPServerSession, requested []acpsdk.McpServer) {
	if acpSession == nil || len(requested) == 0 {
		return
	}
	servers := ConvertACPMCPServers(requested)
	if len(servers) == 0 {
		return
	}
	pandoSessionID := acpSession.PandoSessionID()
	ready := make(chan struct{})
	previous := acpSession.MCPReady()
	acpSession.SetMCPReady(ready)
	a.logger.Printf("[ACP AGENT] Attaching %d client MCP server(s) to session %s: %s", len(servers), pandoSessionID, sessionMCPServerNames(servers))
	logging.Info("acp: session mcp attach started", "session_id", pandoSessionID, "servers", sessionMCPServerNames(servers))

	go func() {
		defer close(ready)
		// Attaches of one session run in order, so the latest request wins.
		if previous != nil {
			<-previous
		}
		started := time.Now()
		// The connections outlive this request; the context only bounds the
		// discovery (the agent service applies its own per-server timeouts).
		err := a.agentService.AttachSessionMCPServers(context.Background(), pandoSessionID, servers)
		if acpSession.MCPReady() == nil {
			// The session was closed while connecting: do not leak the servers.
			a.agentService.DetachSessionMCPServers(pandoSessionID)
			return
		}
		if err != nil {
			a.logger.Printf("[ACP AGENT] Session %s MCP attach finished with errors: %v", pandoSessionID, err)
			logging.Warn("acp: session mcp attach failed", "session_id", pandoSessionID, "duration_ms", time.Since(started).Milliseconds(), "error", err)
			return
		}
		logging.Info("acp: session mcp attached", "session_id", pandoSessionID, "duration_ms", time.Since(started).Milliseconds())
	}()
}

// waitSessionMCPReady blocks until the session's MCP servers finished
// connecting, the timeout elapses or ctx is cancelled.
func (a *PandoACPAgent) waitSessionMCPReady(ctx context.Context, acpSession *ACPServerSession, timeout time.Duration) {
	ready := acpSession.MCPReady()
	if ready == nil {
		return
	}
	select {
	case <-ready:
		return
	default:
	}
	started := time.Now()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ready:
		logging.Info("acp: prompt waited for session mcp", "session_id", acpSession.PandoSessionID(), "wait_ms", time.Since(started).Milliseconds())
	case <-timer.C:
		a.logger.Printf("[ACP AGENT] Session %s MCP servers not ready after %s; continuing without them", acpSession.PandoSessionID(), timeout)
		logging.Warn("acp: session mcp not ready, prompt continues", "session_id", acpSession.PandoSessionID(), "timeout_ms", timeout.Milliseconds())
	case <-ctx.Done():
	}
}

// detachSessionMCPServers releases the session's MCP connections.
func (a *PandoACPAgent) detachSessionMCPServers(acpSession *ACPServerSession) {
	if acpSession == nil || acpSession.MCPReady() == nil {
		return
	}
	acpSession.SetMCPReady(nil)
	a.agentService.DetachSessionMCPServers(acpSession.PandoSessionID())
}

// ReleaseSessionMCPServers detaches the MCP servers of every session. It is
// called when the transport stops so no client-provided server process
// outlives the connection that asked for it.
func (a *PandoACPAgent) ReleaseSessionMCPServers() {
	a.sessionsMu.RLock()
	sessions := make([]*ACPServerSession, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.sessionsMu.RUnlock()
	for _, s := range sessions {
		a.detachSessionMCPServers(s)
	}
}
