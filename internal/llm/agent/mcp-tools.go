package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/mcpauth"
	"github.com/digiogithub/pando/internal/mcpclient"
	"github.com/digiogithub/pando/internal/mcpgateway"
	"github.com/digiogithub/pando/internal/notify"
	"github.com/digiogithub/pando/internal/permission"

	"github.com/mark3labs/mcp-go/mcp"
)

// globalLuaManager is the package-level Lua filter manager, set during app initialization.
var globalLuaManager *luaengine.FilterManager

// SetLuaManager sets the global Lua filter manager used for MCP tool input/output filtering.
func SetLuaManager(fm *luaengine.FilterManager) {
	globalLuaManager = fm
}

type mcpTool struct {
	mcpName     string
	tool        mcp.Tool
	mcpConfig   config.MCPServer
	permissions permission.Service
}

type MCPClient interface {
	Initialize(
		ctx context.Context,
		request mcp.InitializeRequest,
	) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

func mcpToolErrorResponse(serverName, message string) tools.ToolResponse {
	wrapped := fmt.Sprintf("MCP server %q: %s", serverName, message)
	notify.Error(notify.SourceTool, wrapped)
	return tools.NewTextErrorResponse(wrapped)
}

func mcpOperationError(serverName, toolName, operation string, err error, timeout time.Duration) tools.ToolResponse {
	if errors.Is(err, context.DeadlineExceeded) {
		msg := mcpclient.BuildTimeoutError(serverName, operation, timeout).Error()
		mcpclient.PublishError(serverName, msg)
		return tools.NewTextErrorResponse(msg)
	}
	if scopeErr, ok := mcpauth.IsInsufficientScope(err); ok {
		mcpclient.PublishWarn(serverName, scopeErr.Error())
		return tools.NewTextErrorResponse(scopeErr.Error())
	}
	msg := mcpclient.BuildCallError(serverName, toolName, operation, err).Error()
	mcpclient.PublishError(serverName, msg)
	return tools.NewTextErrorResponse(msg)
}

func (b *mcpTool) Info() tools.ToolInfo {
	required := b.tool.InputSchema.Required
	if required == nil {
		required = make([]string, 0)
	}
	return tools.ToolInfo{
		Name:        fmt.Sprintf("%s_%s", b.mcpName, b.tool.Name),
		Description: b.tool.Description,
		Parameters:  b.tool.InputSchema.Properties,
		Required:    required,
	}
}

func runTool(ctx context.Context, c MCPClient, serverName string, timeout time.Duration, toolName string, input string) (tools.ToolResponse, error) {
	logging.Debug("runTool", "server", serverName, "toolName", toolName, "inputLength", len(input), "timeout", timeout)
	defer c.Close()
	initRequest := mcpclient.BuildInitializeRequest("OpenCode")

	initCtx, initCancel := mcpclient.WithTimeout(ctx, timeout)
	_, err := c.Initialize(initCtx, initRequest)
	initCancel()
	if err != nil {
		return mcpOperationError(serverName, toolName, "initialize", err, timeout), nil
	}

	toolRequest := mcp.CallToolRequest{}
	toolRequest.Params.Name = toolName

	normalizedInput, err := tools.NormalizeJSONInput(input)
	if err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	var args map[string]any
	if err = json.Unmarshal([]byte(normalizedInput), &args); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	// Apply Lua input filter if manager is available
	if globalLuaManager != nil && globalLuaManager.IsEnabled() {
		hookCtx := luaengine.NewInputContext(serverName, toolName, args, "")
		if filtered, ferr := globalLuaManager.ApplyInputFilter(ctx, hookCtx); ferr == nil && filtered.Modified {
			args = filtered.Data
			logging.Debug("Lua input filter applied", "toolName", toolName, "serverName", serverName)
		}
	}

	toolRequest.Params.Arguments = args
	callCtx, callCancel := mcpclient.WithTimeout(ctx, timeout)
	result, err := c.CallTool(callCtx, toolRequest)
	callCancel()
	if err != nil {
		return mcpOperationError(serverName, toolName, "call", err, timeout), nil
	}

	output := ""
	for _, v := range result.Content {
		if v, ok := v.(mcp.TextContent); ok {
			output = v.Text
		} else {
			output = fmt.Sprintf("%v", v)
		}
	}

	// Apply Lua output filter if manager is available
	if globalLuaManager != nil && globalLuaManager.IsEnabled() {
		resultData := map[string]interface{}{"output": output}
		hookCtx := luaengine.NewOutputContext(serverName, toolName, resultData, "", 0)
		if filtered, ferr := globalLuaManager.ApplyOutputFilter(ctx, hookCtx); ferr == nil && filtered.Modified {
			if out, ok := filtered.Data["output"].(string); ok {
				output = out
			}
			logging.Debug("Lua output filter applied", "toolName", toolName, "serverName", serverName)
		}
	}

	logging.Debug("runTool completed", "toolName", toolName, "outputLength", len(output))
	return tools.NewTextResponse(tools.FormatJSONLikeContent(output)), nil
}

func (b *mcpTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	logging.Debug("mcpTool.Run", "toolName", b.Info().Name, "type", string(b.mcpConfig.Type))
	sessionID, messageID := tools.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return tools.ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}
	permissionDescription := fmt.Sprintf("execute %s with the following parameters: %s", b.Info().Name, params.Input)
	// The tool's own context is passed so an abandoned prompt cannot outlive
	// the run that raised it: RequestWithContext fails closed when ctx ends.
	p := b.permissions.RequestWithContext(
		ctx,
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        config.WorkingDirectory(),
			ToolName:    b.Info().Name,
			Action:      "execute",
			Description: permissionDescription,
			Params:      params.Input,
		},
	)
	if !p {
		return tools.NewTextErrorResponse("permission denied"), nil
	}

	var response tools.ToolResponse
	var err error

	timeout := mcpclient.ResolveTimeout(b.mcpConfig.Timeout, mcpclient.DefaultOperationTimeout)
	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()

	c, cerr := mcpclient.New(clientCtx, b.mcpName, b.mcpConfig)
	if cerr != nil {
		if mcpclient.IsAuthorizationRequired(cerr) {
			msg := fmt.Sprintf("MCP server %q requires authorization. Run: pando mcp login %s", b.mcpName, b.mcpName)
			mcpclient.PublishWarn(b.mcpName, msg)
			return tools.NewTextErrorResponse(msg), nil
		}
		return mcpToolErrorResponse(b.mcpName, cerr.Error()), nil
	}
	response, err = runTool(ctx, c, b.mcpName, timeout, b.tool.Name, params.Input)

	if err != nil {
		return response, err
	}

	// Auto-cache large MCP tool responses (interception runs after Lua filters applied in runTool).
	if cache := tools.GetSessionCache(ctx); cache != nil {
		response = tools.InterceptToolResponse(cache, params.ID, b.Info().Name, response)
	}
	return response, nil
}

func NewMcpTool(name string, tool mcp.Tool, permissions permission.Service, mcpConfig config.MCPServer) tools.BaseTool {
	return &mcpTool{
		mcpName:     name,
		tool:        tool,
		mcpConfig:   mcpConfig,
		permissions: permissions,
	}
}

// mcpToolDescriptor is what MCP discovery actually found: a server name, the
// tool descriptor it advertised and the server configuration needed to call it.
// It carries no permission.Service on purpose -- the cache below holds these,
// never permission-bound tools.BaseTool values, so every caller of GetMcpTools
// gets tools wired to its OWN approval service (PANDO-US-0031: the AG-UI
// adapter owns a separate service by invariant I3, and used to be handed the
// TUI's because the cache had baked it in at startup).
type mcpToolDescriptor struct {
	serverName string
	tool       mcp.Tool
	mcpConfig  config.MCPServer
}

var (
	// mcpToolsMu guards mcpToolDescriptors; mcpDiscoveryMu serializes the
	// discovery itself so two concurrent first callers do not both connect to
	// every configured server.
	mcpToolsMu         sync.RWMutex
	mcpToolDescriptors []mcpToolDescriptor
	mcpDiscoveryMu     sync.Mutex
)

// ResetMcpToolsCache drops the discovered catalog. Its meaning is unchanged:
// the next GetMcpTools re-runs discovery.
func ResetMcpToolsCache() {
	mcpToolsMu.Lock()
	defer mcpToolsMu.Unlock()
	mcpToolDescriptors = nil
}

func cachedMcpToolDescriptors() []mcpToolDescriptor {
	mcpToolsMu.RLock()
	defer mcpToolsMu.RUnlock()
	return mcpToolDescriptors
}

// CachedMcpToolNames returns the "<server>_<tool>" names of the MCP tools
// already discovered in this process, without connecting to anything and
// without constructing a permission service. It returns nil when discovery has
// not run yet: a name list for a prompt is not a reason to open MCP
// connections, and it is certainly not a reason to populate the shared cache
// (PANDO-US-0031, defect 2).
func CachedMcpToolNames() []string {
	descriptors := cachedMcpToolDescriptors()
	if len(descriptors) == 0 {
		return nil
	}
	names := make([]string, 0, len(descriptors))
	for _, d := range descriptors {
		names = append(names, fmt.Sprintf("%s_%s", d.serverName, d.tool.Name))
	}
	return names
}

func getToolDescriptors(ctx context.Context, name string, m config.MCPServer, c MCPClient) []mcpToolDescriptor {
	logging.Debug("getTools", "serverName", name, "type", string(m.Type))
	var stdioTools []mcpToolDescriptor
	timeout := mcpclient.ResolveTimeout(m.Timeout, mcpclient.DefaultDiscoveryTimeout)

	// Handshake also reports the outcome on the extension mcp topic.
	_, err := mcpclient.Handshake(ctx, c, name, m, "OpenCode")
	if err != nil {
		logging.Error("error initializing mcp client", "server", name, "error", err)
		if scopeErr, ok := mcpauth.IsInsufficientScope(err); ok {
			mcpclient.PublishWarn(name, scopeErr.Error())
		} else if mcpclient.IsAuthorizationRequired(err) {
			mcpclient.PublishWarn(name, fmt.Sprintf("MCP server %q requires authorization. Run: pando mcp login %s", name, name))
		} else {
			mcpclient.PublishWarn(name, mcpclient.BuildCallError(name, "", "initialize during discovery", err).Error())
		}
		return stdioTools
	}
	toolsRequest := mcp.ListToolsRequest{}
	listCtx, listCancel := mcpclient.WithTimeout(ctx, timeout)
	tools, err := c.ListTools(listCtx, toolsRequest)
	listCancel()
	if err != nil {
		logging.Error("error listing tools", "server", name, "error", err)
		if scopeErr, ok := mcpauth.IsInsufficientScope(err); ok {
			mcpclient.PublishWarn(name, scopeErr.Error())
		} else {
			mcpclient.PublishWarn(name, mcpclient.BuildCallError(name, "", "list tools during discovery", err).Error())
		}
		return stdioTools
	}
	logging.Debug("MCP server tools listed", "serverName", name, "toolCount", len(tools.Tools))
	for _, t := range tools.Tools {
		stdioTools = append(stdioTools, mcpToolDescriptor{serverName: name, tool: t, mcpConfig: m})
	}
	defer c.Close()
	return stdioTools
}

// GetMcpTools returns the MCP catalog as tools bound to the permission service
// the CALLER passed. The discovery result is cached and shared (one connection
// per server per process, as before); only the permission binding is per call.
func GetMcpTools(ctx context.Context, permissions permission.Service) []tools.BaseTool {
	descriptors := cachedMcpToolDescriptors()
	logging.Debug("GetMcpTools called", "existingToolCount", len(descriptors), "serverCount", len(config.Get().MCPServers))
	if len(descriptors) == 0 {
		descriptors = discoverMcpTools(ctx)
	}

	result := make([]tools.BaseTool, 0, len(descriptors))
	for _, d := range descriptors {
		result = append(result, NewMcpTool(d.serverName, d.tool, permissions, d.mcpConfig))
	}
	return result
}

// discoverMcpTools connects to every configured MCP server once and caches the
// tool descriptors it advertises. As before, an empty result is not cached, so
// a server that was down when the process started is retried on the next call.
func discoverMcpTools(ctx context.Context) []mcpToolDescriptor {
	mcpDiscoveryMu.Lock()
	defer mcpDiscoveryMu.Unlock()

	// Another caller may have completed discovery while this one waited.
	if cached := cachedMcpToolDescriptors(); len(cached) > 0 {
		return cached
	}

	var descriptors []mcpToolDescriptor
	for name, m := range config.Get().MCPServers {
		logging.Debug("Initializing MCP server", "name", name, "type", string(m.Type))
		clientCtx, clientCancel := context.WithCancel(ctx)
		c, err := mcpclient.New(clientCtx, name, m)
		if err != nil {
			clientCancel()
			logging.Error("error creating mcp client", "server", name, "error", err)
			if mcpclient.IsAuthorizationRequired(err) {
				mcpclient.PublishWarn(name, fmt.Sprintf("MCP server %q requires authorization. Run: pando mcp login %s", name, name))
			} else {
				mcpclient.PublishWarn(name, fmt.Sprintf("Failed to connect MCP server %q: %v", name, err))
			}
			continue
		}

		descriptors = append(descriptors, getToolDescriptors(ctx, name, m, c)...)
		clientCancel()
	}

	mcpToolsMu.Lock()
	mcpToolDescriptors = descriptors
	mcpToolsMu.Unlock()

	logging.Debug("MCP tools loaded", "totalToolCount", len(descriptors))
	return descriptors
}

// GetMcpToolsWithGateway returns MCP-backed tools for the LLM agent.
// When gw is non-nil (gateway mode), it exposes two proxy tools plus any
// favorite tools as direct wrappers. When gw is nil it falls back to the
// standard per-server tool list.
//
// NOTE: this is the legacy gateway exposure path, kept for the
// ToolDiscovery-disabled configuration and for the pando mcp-server mode.
// The unified discovery path uses GetMcpFavoriteTools plus the tool_search
// tool wired in ApplyToolDiscovery.
func GetMcpToolsWithGateway(ctx context.Context, permissions permission.Service, gw *mcpgateway.Gateway) []tools.BaseTool {
	if gw == nil {
		return GetMcpTools(ctx, permissions)
	}

	result := []tools.BaseTool{
		mcpgateway.NewCatalogTool(gw),
		mcpgateway.NewCallToolProxy(gw),
	}
	return append(result, GetMcpFavoriteTools(ctx, permissions, gw)...)
}

// GetMcpFavoriteTools returns the gateway's favorite MCP tools as direct
// wrappers so the LLM can call them without going through a proxy (lower
// latency, richer schema visibility). The rest of the MCP catalog stays
// reachable through the unified tool_search tool.
func GetMcpFavoriteTools(ctx context.Context, permissions permission.Service, gw *mcpgateway.Gateway) []tools.BaseTool {
	result := []tools.BaseTool{}

	favorites, err := gw.GetFavorites(ctx)
	if err != nil {
		logging.Error("MCP gateway: failed to load favorites", "error", err)
		return result
	}

	cfg := config.Get()
	if cfg == nil {
		return result
	}

	for _, fav := range favorites {
		srv, ok := cfg.MCPServers[fav.ServerName]
		if !ok {
			continue
		}
		// Reconstruct an mcp.Tool from the registry data so we can reuse the
		// existing NewMcpTool helper.
		t := mcp.Tool{
			Name:        fav.ToolName,
			Description: fav.Description,
		}
		t.InputSchema.Properties = fav.InputSchema
		result = append(result, NewMcpTool(fav.ServerName, t, permissions, srv))
	}

	logging.Debug("MCP gateway favorite tools assembled", "favorites", len(result))
	return result
}
