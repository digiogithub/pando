package tooldiscovery

// ToolSource identifies which subsystem provided a tool.
type ToolSource string

const (
	SourceCore     ToolSource = "core"
	SourceMCP      ToolSource = "mcp"
	SourceInternal ToolSource = "internal"
	SourceLua      ToolSource = "lua"
	SourceMesnada  ToolSource = "mesnada"
	SourceRAG      ToolSource = "rag"
	SourceGateway  ToolSource = "gateway"
)

// ToolMetadata carries optional discovery metadata for a BaseTool.
type ToolMetadata struct {
	CanonicalName string
	Aliases       []string
	ServerName    string
	ToolName      string
	Source        ToolSource
	Category      string
	// NonDeferred means this tool is always visible regardless of the threshold.
	NonDeferred bool
	// Priority controls ordering within a category (higher = more important).
	Priority int
}

// ToolMetadataProvider is an optional interface a BaseTool can implement
// to expose richer discovery metadata without changing the core BaseTool contract.
type ToolMetadataProvider interface {
	ToolMetadata() ToolMetadata
}
