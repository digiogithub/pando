package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mcpauth"
)

// mcpLoginStartResponse is returned by POST /api/v1/mcp/{name}/login. Because
// the OAuth authorization-code flow is interactive (it needs the user's
// browser), the handler only waits long enough to learn the authorization
// URL, then lets the rest of the flow (browser round trip + local callback)
// run in the background; the caller polls GET /api/v1/mcp/{name}/status to
// learn when it completes. This mirrors handleCopilotLoginStart's
// start-then-poll pattern in handlers_auth.go.
type mcpLoginStartResponse struct {
	AuthorizationURL string `json:"authorizationUrl"`
	Message          string `json:"message"`
}

// mcpAuthMessageResponse is a generic {message} response, matching
// authProviderMessageResponse's shape for the other auth surfaces.
type mcpAuthMessageResponse struct {
	Message string `json:"message"`
}

// mcpLoginWaitTimeout bounds how long handleMCPLogin waits to learn the
// authorization URL before giving up and reporting an error; it does not
// bound the whole login flow, which continues in the background regardless
// (mcpauth.Login owns its own, longer, callback timeout).
const mcpLoginWaitTimeout = 20 * time.Second

// handleMCPLogin handles POST /api/v1/mcp/{name}/login: starts the OAuth 2.1
// authorization-code + PKCE flow for the named server.
func (s *Server) handleMCPLogin(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	srv, ok := cfg.MCPServers[name]
	if !ok {
		writeError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}
	if !srv.Auth.IsOAuth() {
		writeError(w, http.StatusBadRequest, "MCP server "+name+" auth type is not oauth; nothing to log in to")
		return
	}

	urlCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		// This flow is interactive by nature (it waits for the user's
		// browser to complete the authorization and hit the local callback
		// server), so it deliberately runs detached from the request
		// context/lifetime rather than r.Context(), which is canceled the
		// moment this handler returns.
		ctx, cancel := context.WithTimeout(context.Background(), mcpauth.DefaultCallbackTimeout+30*time.Second)
		defer cancel()

		prompt := mcpauth.LoginPrompt{
			OnAuthorizationURL: func(u string) {
				select {
				case urlCh <- u:
				default:
				}
			},
			OpenBrowser: true,
			OnBrowserError: func(err error) {
				logging.Warn("mcp login: failed to open browser automatically", "server", name, "error", err)
			},
		}

		err := mcpauth.Default().Login(ctx, name, srv, prompt)
		if err != nil {
			logging.Warn("mcp login: flow failed", "server", name, "error", err)
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	select {
	case authURL := <-urlCh:
		writeJSON(w, http.StatusOK, mcpLoginStartResponse{
			AuthorizationURL: authURL,
			Message:          "Open the authorization URL to finish logging in to MCP server " + name + "; poll GET /api/v1/mcp/" + name + "/status for completion.",
		})
	case err := <-errCh:
		writeError(w, http.StatusBadGateway, "start MCP login for "+name+": "+err.Error())
	case <-time.After(mcpLoginWaitTimeout):
		writeError(w, http.StatusGatewayTimeout, "timed out waiting for the authorization URL for MCP server "+name)
	}
}

// handleMCPLogout handles POST /api/v1/mcp/{name}/logout: removes stored
// OAuth tokens and client registration for the named server.
func (s *Server) handleMCPLogout(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := mcpauth.Default().Logout(name); err != nil {
		writeError(w, http.StatusInternalServerError, "logout MCP server "+name+": "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mcpAuthMessageResponse{Message: "Logged out of MCP server " + name})
}

// handleMCPAuthStatus handles GET /api/v1/mcp/{name}/status: reports the
// current OAuth state for the named server, for polling after login and for
// any status badge in the UI.
func (s *Server) handleMCPAuthStatus(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}
	srv, ok := cfg.MCPServers[name]
	if !ok {
		writeError(w, http.StatusNotFound, "MCP server not found: "+name)
		return
	}

	info := mcpauth.Default().Status(name, srv)
	status := buildMCPServerAuthStatusItem(info)
	if status == nil {
		// Non-OAuth server: still report a minimal, well-formed status
		// rather than an empty body, so the WebUI doesn't need a special
		// case for "no status yet" vs "not oauth".
		status = &MCPServerAuthStatusItem{Type: info.AuthType}
	}
	writeJSON(w, http.StatusOK, status)
}
