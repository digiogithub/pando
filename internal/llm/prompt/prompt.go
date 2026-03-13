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
