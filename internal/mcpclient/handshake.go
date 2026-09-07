package mcpclient

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extevents"
	"github.com/mark3labs/mcp-go/mcp"
)

// The MCP handshake, in one place.
//
// Every part of the host that talks to an MCP server does the same three
// steps: resolve the timeout, build the initialize request, call Initialize.
// Handshake is that sequence, and it is also where the connection outcome is
// observable, so it is where the mcp topic is published. Publishing anywhere
// else would either miss a caller or report a connection that never happened.

// Initializer is the one method Handshake needs. It is declared here rather
// than taking Client so that the gateway's pooled client and the agent's own
// client interface both satisfy it without conversion.
type Initializer interface {
	Initialize(ctx context.Context, req mcp.InitializeRequest) (*mcp.InitializeResult, error)
}

// Handshake initializes c against the configured server and reports the
// outcome on the extension mcp topic. clientName identifies the part of the
// host that is connecting and travels both in the MCP client info and in the
// event, so an operator can tell a discovery pass from a gateway connection.
//
// The error is returned unchanged: callers keep whatever handling they had,
// and the event is a side effect that cannot fail the connection.
func Handshake(ctx context.Context, c Initializer, serverName string, srv config.MCPServer, clientName string) (*mcp.InitializeResult, error) {
	timeout := ResolveTimeout(srv.Timeout, DefaultDiscoveryTimeout)
	initCtx, cancel := WithTimeout(ctx, timeout)
	res, err := c.Initialize(initCtx, BuildInitializeRequest(clientName))
	cancel()

	extevents.MCPHandshake(serverName, transportName(srv.Type), clientName, err, err != nil && IsAuthorizationRequired(err))
	return res, err
}

// transportName renders the configured server type as the stable, lower-case
// name the event contract documents.
func transportName(t config.MCPType) string {
	switch t {
	case config.MCPStdio:
		return "stdio"
	case config.MCPSse:
		return "sse"
	case config.MCPStreamableHTTP:
		return "http"
	default:
		name := strings.TrimSpace(strings.ToLower(string(t)))
		if name == "" {
			return "unknown"
		}
		return name
	}
}
