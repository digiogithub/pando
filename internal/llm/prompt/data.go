package prompt

import "github.com/digiogithub/pando/internal/config"

// PromptData contains all data available for template rendering.
type PromptData struct {
	// Identity
	AgentName string
	AgentRole string
	Version   string

	// Environment
	WorkingDir       string
	IsGitRepo        bool
	Platform         string
	Date             string
	GitBranch        string
	GitStatus        string
	GitRecentCommits string
	ProjectListing   string

	// Provider
	Provider string
	Model    string

	// Capabilities (conditional section flags)
	HasRemembrances  bool
	HasOrchestration bool // mesnada sub-agents
	HasWebSearch     bool
	HasCodeIndexing  bool
	HasLSP           bool
	HasSkills        bool

	// Web search detail flags (for template conditionals)
	HasGoogleSearch bool
	HasBraveSearch  bool
	HasPerplexity   bool

	// Context
	ContextFiles    []ContextFile
	SkillsMetadata  string
	ActiveSkills    []string
	LSPInfo         string
	MCPInstructions string

	// Config reference
	Config *config.Config
}

// ContextFile represents a loaded project context file.
type ContextFile struct {
	Path    string
	Content string
}

// PromptSection represents a rendered template section.
type PromptSection struct {
	Name    string
	Content string
}
