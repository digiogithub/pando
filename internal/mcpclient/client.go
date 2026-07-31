package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpauth"
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

	// logsMu guards recentLogs, a small ring of the last stderr lines written
	// by the child process. When the handshake fails because the server exited
	// (typically a missing API key or a bad argument), those lines carry the
	// only actionable explanation, so they are appended to the returned error
	// instead of living solely in the log file.
	logsMu     sync.Mutex
	recentLogs []string
}

// maxRecentStderrLines bounds the retained stderr ring per stdio client.
const maxRecentStderrLines = 5

func New(ctx context.Context, serverName string, srv config.MCPServer) (Client, error) {
	resolved, err := config.ResolveMCPServerSecrets(srv)
	if err != nil {
		return nil, fmt.Errorf("resolve MCP server %s secrets: %w", serverName, err)
	}

	switch resolved.Type {
	case config.MCPStdio:
		if resolved.Auth != nil && resolved.Auth.ResolvedType() != config.MCPAuthNone {
			logging.Warn("MCP stdio server has an Auth block configured; stdio servers must take credentials from env per the MCP spec, ignoring Auth", "server", serverName, "authType", resolved.Auth.ResolvedType())
		}
		c, err := client.NewStdioMCPClient(resolved.Command, resolved.Env, resolved.Args...)
		if err != nil {
			return nil, err
		}
		wrapped := &stdioLogDrainingClient{inner: c, server: serverName}
		wrapped.startLogDrainer(ctx)
		return wrapped, nil
	case config.MCPSse:
		headers, err := resolved.AuthHeaders()
		if err != nil {
			return nil, fmt.Errorf("MCP server %s auth: %w", serverName, err)
		}
		tlsCfg, err := mcpauth.BuildTLSConfig(resolved)
		if err != nil {
			return nil, fmt.Errorf("MCP server %s tls config: %w", serverName, err)
		}
		baseTransport := mcpauth.TransportForTLS(tlsCfg)
		switch resolved.Auth.ResolvedType() {
		case config.MCPAuthOAuth:
			oauthCfg, err := mcpauth.Default().OAuthConfig(serverName, resolved)
			if err != nil {
				return nil, fmt.Errorf("MCP server %s oauth config: %w", serverName, err)
			}
			if tlsCfg != nil {
				oauthCfg.HTTPClient = mcpauth.HTTPClientForTLS(tlsCfg, 30*time.Second)
			}
			interceptor := mcpauth.NewScopeCapturingTransport(baseTransport)
			c, err := client.NewOAuthSSEClient(resolved.URL, oauthCfg, client.WithHeaders(headers), transport.WithHTTPClient(&http.Client{Transport: interceptor}))
			if err != nil {
				return nil, err
			}
			return newAuthAwareClient(c, interceptor, serverName, hasStaticClientID(resolved)), nil
		case config.MCPAuthOAuthClientCredentials:
			rt, err := mcpauth.Default().ClientCredentialsTransport(ctx, serverName, resolved, baseTransport)
			if err != nil {
				return nil, fmt.Errorf("MCP server %s client_credentials: %w", serverName, err)
			}
			return client.NewSSEMCPClient(resolved.URL, client.WithHeaders(headers), client.WithHTTPClient(&http.Client{Transport: rt}))
		default:
			if baseTransport == nil {
				return client.NewSSEMCPClient(resolved.URL, client.WithHeaders(headers))
			}
			return client.NewSSEMCPClient(resolved.URL, client.WithHeaders(headers), client.WithHTTPClient(&http.Client{Transport: baseTransport}))
		}
	case config.MCPStreamableHTTP:
		headers, err := resolved.AuthHeaders()
		if err != nil {
			return nil, fmt.Errorf("MCP server %s auth: %w", serverName, err)
		}
		tlsCfg, err := mcpauth.BuildTLSConfig(resolved)
		if err != nil {
			return nil, fmt.Errorf("MCP server %s tls config: %w", serverName, err)
		}
		baseTransport := mcpauth.TransportForTLS(tlsCfg)
		switch resolved.Auth.ResolvedType() {
		case config.MCPAuthOAuth:
			oauthCfg, err := mcpauth.Default().OAuthConfig(serverName, resolved)
			if err != nil {
				return nil, fmt.Errorf("MCP server %s oauth config: %w", serverName, err)
			}
			if tlsCfg != nil {
				oauthCfg.HTTPClient = mcpauth.HTTPClientForTLS(tlsCfg, 30*time.Second)
			}
			interceptor := mcpauth.NewScopeCapturingTransport(baseTransport)
			c, err := client.NewOAuthStreamableHttpClient(resolved.URL, oauthCfg, transport.WithHTTPHeaders(headers), transport.WithHTTPBasicClient(&http.Client{Transport: interceptor}))
			if err != nil {
				return nil, err
			}
			return newAuthAwareClient(c, interceptor, serverName, hasStaticClientID(resolved)), nil
		case config.MCPAuthOAuthClientCredentials:
			rt, err := mcpauth.Default().ClientCredentialsTransport(ctx, serverName, resolved, baseTransport)
			if err != nil {
				return nil, fmt.Errorf("MCP server %s client_credentials: %w", serverName, err)
			}
			return client.NewStreamableHttpClient(resolved.URL, transport.WithHTTPHeaders(headers), transport.WithHTTPBasicClient(&http.Client{Transport: rt}))
		default:
			if baseTransport == nil {
				return client.NewStreamableHttpClient(resolved.URL, transport.WithHTTPHeaders(headers))
			}
			return client.NewStreamableHttpClient(resolved.URL, transport.WithHTTPHeaders(headers), transport.WithHTTPBasicClient(&http.Client{Transport: baseTransport}))
		}
	default:
		return nil, fmt.Errorf("unknown MCP server type: %q", resolved.Type)
	}
}

// IsAuthorizationRequired reports whether err indicates that an MCP server
// needs interactive OAuth authorization (e.g. no token yet, or a refresh
// token that no longer works). Callers such as the agent layer and the
// future `pando mcp login` command use this to detect that a tool call
// failure means "the user needs to (re-)authenticate this server" rather
// than a generic operational error.
func IsAuthorizationRequired(err error) bool {
	return mcpauth.IsAuthorizationRequired(err)
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

// hasStaticClientID reports whether srv's config pins an explicit OAuth
// client_id, as opposed to relying on dynamic client registration. Used to
// gate authAwareClient's invalid_client handling: a config-pinned ClientID
// is never invalidated, since re-reading the same config value would just
// repeat the failure rather than fix anything — only a dynamically
// registered (and therefore mcpauth-persisted) client_id/secret pair is
// ever dropped.
func hasStaticClientID(srv config.MCPServer) bool {
	return srv.Auth != nil && srv.Auth.OAuth != nil && srv.Auth.OAuth.ClientID != ""
}

// authAwareClient wraps an OAuth-authenticated MCP Client to recover two
// conditions mcp-go v0.57.0 does not surface as typed errors on the
// general request path (only on its own 401 handling):
//
//   - RFC 6750 §3.1 "insufficient_scope": a 403 response whose
//     WWW-Authenticate challenge carries error="insufficient_scope". mcp-go's
//     generic non-2xx branch (see streamable_http.go SendRequest) only
//     returns fmt.Errorf("request failed with status %d: %s", ...) for
//     anything other than 401 — the response headers, including
//     WWW-Authenticate, are discarded entirely. To recover them, New()
//     installs an mcpauth.ScopeCapturingTransport as the actual HTTP
//     RoundTripper for tool-call requests (transport.WithHTTPClient /
//     WithHTTPBasicClient — a *different* http.Client than the one
//     OAuthConfig.HTTPClient controls, which only backs the OAuthHandler's
//     own metadata/token-refresh requests, not the tool calls themselves).
//     After any call fails, authAwareClient checks whether the interceptor
//     captured a 403 insufficient_scope challenge and, if so, replaces the
//     generic error with mcpauth.ErrInsufficientScope so the agent layer can
//     prompt for a scoped-up login instead of showing an opaque failure.
//   - OAuth "invalid_client": returned in-band by mcp-go as a
//     transport.OAuthError wrapped into the call error (e.g. during a
//     transparent token refresh inside getValidToken). When the server's
//     config does not pin a static client_id, this indicates the persisted
//     dynamic client registration is stale/revoked, so authAwareClient
//     drops it via Manager.InvalidateClientRegistration and annotates the
//     error with actionable guidance.
type authAwareClient struct {
	Client
	interceptor    *mcpauth.ScopeCapturingTransport
	serverName     string
	staticClientID bool
}

func newAuthAwareClient(c Client, interceptor *mcpauth.ScopeCapturingTransport, serverName string, staticClientID bool) Client {
	return &authAwareClient{Client: c, interceptor: interceptor, serverName: serverName, staticClientID: staticClientID}
}

func (c *authAwareClient) Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error) {
	res, err := c.Client.Initialize(ctx, req)
	return res, c.recoverAuthError(err)
}

func (c *authAwareClient) ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	res, err := c.Client.ListTools(ctx, req)
	return res, c.recoverAuthError(err)
}

func (c *authAwareClient) CallTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	res, err := c.Client.CallTool(ctx, req)
	return res, c.recoverAuthError(err)
}

func (c *authAwareClient) recoverAuthError(err error) error {
	if err == nil {
		return nil
	}
	if ch, ok := c.interceptor.LastChallenge(); ok && strings.EqualFold(ch.Error, "insufficient_scope") {
		scopeErr := mcpauth.Default().HandleInsufficientScope(c.serverName, ch.Scope)
		err = errors.Join(err, scopeErr)
	}
	var oerr transport.OAuthError
	if errors.As(err, &oerr) && oerr.ErrorCode == "invalid_client" && !c.staticClientID {
		if ierr := mcpauth.Default().InvalidateClientRegistration(c.serverName); ierr != nil {
			logging.Warn("mcpauth: failed to invalidate stale client registration", "server", c.serverName, "error", ierr)
		}
		err = fmt.Errorf("%w (the persisted MCP client registration for %q looks stale or was rejected; run: pando mcp login %s)", err, c.serverName, c.serverName)
	}
	return err
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
	res, err := c.inner.Initialize(ctx, req)
	return res, c.explainWithStderr(err)
}

// explainWithStderr enriches a failure with the child process's last stderr
// lines. A stdio server that refuses to start ("Either FIGMA_API_KEY or
// FIGMA_OAUTH_TOKEN is required") otherwise surfaces only as the opaque
// "transport error: transport closed".
func (c *stdioLogDrainingClient) explainWithStderr(err error) error {
	if err == nil {
		return nil
	}
	logs := c.recentStderr()
	if len(logs) == 0 {
		return err
	}
	return fmt.Errorf("%w (%s stderr: %s)", err, c.server, strings.Join(logs, " | "))
}

func (c *stdioLogDrainingClient) recentStderr() []string {
	c.logsMu.Lock()
	defer c.logsMu.Unlock()
	return append([]string(nil), c.recentLogs...)
}

func (c *stdioLogDrainingClient) recordStderr(line string) {
	c.logsMu.Lock()
	defer c.logsMu.Unlock()
	c.recentLogs = append(c.recentLogs, line)
	if len(c.recentLogs) > maxRecentStderrLines {
		c.recentLogs = c.recentLogs[len(c.recentLogs)-maxRecentStderrLines:]
	}
}

func (c *stdioLogDrainingClient) ListTools(ctx context.Context, req mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	res, err := c.inner.ListTools(ctx, req)
	return res, c.explainWithStderr(err)
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
	go drainStdioLogs(ctx, c.server, stderr, c.recordStderr)
}

func drainStdioLogs(ctx context.Context, serverName string, stderr io.Reader, record func(string)) {
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
		if record != nil {
			record(line)
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
