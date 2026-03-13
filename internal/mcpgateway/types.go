package mcpgateway

import "time"

// RegisteredTool represents a tool stored in the MCP tool registry.
type RegisteredTool struct {
	ID             string
	ServerName     string
	ToolName       string
	Description    string
	InputSchema    map[string]interface{}
	LastDiscovered time.Time
}

// UsageStat holds data for a single tool invocation record.
type UsageStat struct {
	ToolID     string
	SessionID  string
	DurationMs int64
	Success    bool
}

// FavoriteConfig controls how favorites are computed from usage statistics.
type FavoriteConfig struct {
	Threshold    int // minimum calls to become a favorite (default 5)
	MaxFavorites int // maximum directly-exposed tools (default 15)
	WindowDays   int // look-back window in days (default 30)
	DecayDays    int // inactivity days before removal from favorites (default 14)
}

// DefaultFavoriteConfig returns FavoriteConfig with sensible defaults.
func DefaultFavoriteConfig() FavoriteConfig {
	return FavoriteConfig{
		Threshold:    5,
		MaxFavorites: 15,
		WindowDays:   30,
		DecayDays:    14,
	}
}
