package prompt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/skills"
)

// globalEvaluator is the package-level evaluator used for UCB template selection
// and skill injection in BuildPrompt.
var globalEvaluator promptEvaluator

// SetGlobalEvaluator sets the evaluator used by BuildPrompt for template selection
// and skill injection. Pass nil to disable.
func SetGlobalEvaluator(e promptEvaluator) {
	globalEvaluator = e
}

func GetAgentPrompt(agentName config.AgentName, provider models.ModelProvider, luaMgr *luaengine.FilterManager) string {
	basePrompt := ""
	switch agentName {
	case config.AgentCoder:
		basePrompt = CoderPrompt(provider)
	case config.AgentTitle:
		basePrompt = TitlePrompt(provider)
	case config.AgentTask:
		basePrompt = TaskPrompt(provider)
	case config.AgentSummarizer:
		basePrompt = SummarizerPrompt(provider)
	default:
		basePrompt = "You are a helpful assistant"
	}

	finalPrompt := basePrompt
	if agentName == config.AgentCoder || agentName == config.AgentTask {
		// Add context from project-specific instruction files if they exist
		contextContent := getContextFromPaths()
		logging.Debug("Context content", "Context", contextContent)
		if contextContent != "" {
			finalPrompt = fmt.Sprintf("%s\n\n# Project-Specific Context\n Make sure to follow the instructions in the context below\n%s", basePrompt, contextContent)
		}
	}

	if luaMgr != nil && luaMgr.IsEnabled() {
		hookData := map[string]interface{}{
			"system_prompt": finalPrompt,
			"agent_name":    string(agentName),
			"provider":      string(provider),
		}
		result, err := luaMgr.ExecuteHook(context.Background(), luaengine.HookSystemPrompt, hookData)
		if err == nil && result != nil && result.Modified {
			if modified, ok := result.Data["system_prompt"].(string); ok && modified != "" {
				finalPrompt = modified
			}
		}
	}

	return finalPrompt
}

func InjectSkillsMetadata(availableSkills []skills.SkillMetadata) string {
	if len(availableSkills) == 0 {
		return ""
	}

	var (
		builder   strings.Builder
		lineCount int
	)

	builder.WriteString("## Available Skills\n")
	for _, skill := range availableSkills {
		name := compactPromptText(skill.Name)
		if name == "" {
			continue
		}

		builder.WriteString("- **")
		builder.WriteString(name)
		builder.WriteString("**")

		description := compactPromptText(skill.Description)
		whenToUse := compactPromptText(skill.WhenToUse)

		if description != "" {
			builder.WriteString(": ")
			builder.WriteString(description)
		}
		if whenToUse != "" {
			builder.WriteString(" (use when: ")
			builder.WriteString(whenToUse)
			builder.WriteString(")")
		}

		builder.WriteByte('\n')
		lineCount++
	}

	if lineCount == 0 {
		return ""
	}

	return strings.TrimRight(builder.String(), "\n")
}

func InjectSkillInstructions(name string, instructions string) string {
	body := strings.TrimSpace(instructions)
	if body == "" {
		return ""
	}

	skillName := compactPromptText(name)
	if skillName == "" {
		return fmt.Sprintf("## Active Skill Instructions\n%s", body)
	}

	return fmt.Sprintf("## Active Skill: %s\n%s", skillName, body)
}

var (
	onceContext    sync.Once
	contextContent string
)

func getContextFromPaths() string {
	onceContext.Do(func() {
		var (
			cfg          = config.Get()
			workDir      = cfg.WorkingDir
			contextPaths = cfg.ContextPaths
		)

		contextContent = processContextPaths(workDir, contextPaths)
	})

	return contextContent
}

func processContextPaths(workDir string, paths []string) string {
	selectedProjectContextPath, hasSelectedProjectContextPath := config.DetectPreferredProjectContextPath(workDir)

	// Track processed files to avoid duplicates
	processedFiles := make(map[string]bool)
	var processedMutex sync.Mutex
	results := make([]string, 0)

	for _, path := range paths {
		if hasSelectedProjectContextPath && config.IsPrioritizedProjectContextPath(path) && filepath.Base(path) != selectedProjectContextPath {
			continue
		}

		if strings.HasSuffix(path, "/") {
			_ = filepath.WalkDir(filepath.Join(workDir, path), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}

				processedMutex.Lock()
				lowerPath := strings.ToLower(path)
				if !processedFiles[lowerPath] {
					processedFiles[lowerPath] = true
					processedMutex.Unlock()

					result := processFile(path)
					if result != "" {
						results = append(results, result)
					}
				} else {
					processedMutex.Unlock()
				}
				return nil
			})
			continue
		}

		fullPath := filepath.Join(workDir, path)

		processedMutex.Lock()
		lowerPath := strings.ToLower(fullPath)
		if !processedFiles[lowerPath] {
			processedFiles[lowerPath] = true
			processedMutex.Unlock()

			result := processFile(fullPath)
			if result != "" {
				results = append(results, result)
			}
		} else {
			processedMutex.Unlock()
		}
	}

	return strings.Join(results, "\n")
}

func processFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return "# From:" + filePath + "\n" + string(content)
}

func compactPromptText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

// BuildOption is a functional option for configuring BuildPrompt.
type BuildOption func(*buildOptions)

type buildOptions struct {
	mcpServers      []string
	tools           []string
	workingDir      string
	isGitRepo       bool
	platform        string
	date            string
	gitBranch       string
	gitStatus       string
	gitRecentCommits string
	projectListing  string
	skillsMetadata  string
	activeSkills    []string
	lspInfo         string
	mcpInstructions string
	version         string
	model           string
	contextFiles    []ContextFile
}

// WithMCPServers sets the list of MCP server names for capability detection.
func WithMCPServers(servers []string) BuildOption {
	return func(o *buildOptions) {
		o.mcpServers = servers
	}
}

// WithTools sets the list of available tool names for capability detection.
func WithTools(tools []string) BuildOption {
	return func(o *buildOptions) {
		o.tools = tools
	}
}

// WithEnvironment sets environment information for the prompt.
func WithEnvironment(workingDir string, isGitRepo bool, platform, date string) BuildOption {
	return func(o *buildOptions) {
		o.workingDir = workingDir
		o.isGitRepo = isGitRepo
		o.platform = platform
		o.date = date
	}
}

// WithGitInfo sets git-related information for the prompt.
func WithGitInfo(branch, status, recentCommits string) BuildOption {
	return func(o *buildOptions) {
		o.gitBranch = branch
		o.gitStatus = status
		o.gitRecentCommits = recentCommits
	}
}

// WithProjectListing sets the project directory listing for the prompt.
func WithProjectListing(listing string) BuildOption {
	return func(o *buildOptions) {
		o.projectListing = listing
	}
}

// WithSkills sets skills metadata and active skills for the prompt.
func WithSkills(metadata string, activeSkills []string) BuildOption {
	return func(o *buildOptions) {
		o.skillsMetadata = metadata
		o.activeSkills = activeSkills
	}
}

// WithLSPInfo sets LSP information for the prompt.
func WithLSPInfo(info string) BuildOption {
	return func(o *buildOptions) {
		o.lspInfo = info
	}
}

// WithMCPInstructions sets MCP instructions for the prompt.
func WithMCPInstructions(instructions string) BuildOption {
	return func(o *buildOptions) {
		o.mcpInstructions = instructions
	}
}

// WithVersion sets the application version for the prompt.
func WithVersion(version string) BuildOption {
	return func(o *buildOptions) {
		o.version = version
	}
}

// WithModel sets the model name for the prompt.
func WithModel(model string) BuildOption {
	return func(o *buildOptions) {
		o.model = model
	}
}

// WithContextFiles sets additional context files for the prompt.
func WithContextFiles(files []ContextFile) BuildOption {
	return func(o *buildOptions) {
		o.contextFiles = files
	}
}

// BuildPrompt constructs a system prompt using the template-based builder.
// This is the new entry point that uses the template infrastructure while
// keeping GetAgentPrompt available for backward compatibility.
func BuildPrompt(ctx context.Context, agentName config.AgentName, provider models.ModelProvider, luaMgr *luaengine.FilterManager, opts ...BuildOption) (string, error) {
	var o buildOptions
	for _, opt := range opts {
		opt(&o)
	}

	cfg := config.Get()

	// Detect capabilities
	detector := NewCapabilityDetector(cfg, o.mcpServers, o.tools)
	caps := detector.Detect()
	google, brave, perplexity := detector.DetectWebSearchDetails()

	hasSkills := o.skillsMetadata != "" || len(o.activeSkills) > 0

	data := &PromptData{
		AgentName:        string(agentName),
		Version:          o.version,
		WorkingDir:       o.workingDir,
		IsGitRepo:        o.isGitRepo,
		Platform:         o.platform,
		Date:             o.date,
		GitBranch:        o.gitBranch,
		GitStatus:        o.gitStatus,
		GitRecentCommits: o.gitRecentCommits,
		ProjectListing:   o.projectListing,
		Provider:         string(provider),
		Model:            o.model,
		HasRemembrances:  caps["remembrances"],
		HasOrchestration: caps["orchestration"],
		HasWebSearch:     caps["web_search"],
		HasCodeIndexing:  caps["code_indexing"],
		HasLSP:           caps["lsp"],
		HasSkills:        hasSkills,
		HasGoogleSearch:  google,
		HasBraveSearch:   brave,
		HasPerplexity:    perplexity,
		ContextFiles:     o.contextFiles,
		SkillsMetadata:   o.skillsMetadata,
		ActiveSkills:     o.activeSkills,
		LSPInfo:          o.lspInfo,
		MCPInstructions:  o.mcpInstructions,
		Config:           cfg,
	}

	builder := NewPromptBuilder(string(agentName), string(provider), data, luaMgr)
	if globalEvaluator != nil {
		builder.SetEvaluator(globalEvaluator)
	}
	return builder.Build(ctx)
}
