package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/llm/tools"
)

// CLIToolSkill wraps a CLI executable as a BaseTool.
type CLIToolSkill struct {
	name        string
	description string
	execPath    string
	schema      map[string]any
	required    []string
}

type cliToolMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// DiscoverCLITools scans skill directories for CLI tool skills.
// A CLI tool skill is a directory containing an executable named "tool" and a README.md.
func DiscoverCLITools(paths []string) []CLIToolSkill {
	discovered := make([]CLIToolSkill, 0)

	for _, root := range paths {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			skillDir := filepath.Join(root, entry.Name())
			readmePath := filepath.Join(skillDir, "README.md")
			toolPath := filepath.Join(skillDir, "tool")
			toolInfo, err := os.Stat(toolPath)
			if err != nil || toolInfo.IsDir() || toolInfo.Mode()&0o111 == 0 {
				continue
			}
			readmeInfo, err := os.Stat(readmePath)
			if err != nil || readmeInfo.IsDir() {
				continue
			}

			skill := CLIToolSkill{
				name:     entry.Name(),
				execPath: toolPath,
				schema:   map[string]any{},
				required: []string{},
			}

			if description, err := readOptionalTrimmedFile(readmePath); err == nil && description != "" {
				skill.description = description
			}

			if schema, required, err := loadOptionalSchema(filepath.Join(skillDir, "schema.json")); err == nil {
				if schema != nil {
					skill.schema = schema
				}
				if required != nil {
					skill.required = required
				}
			}

			if metadata, err := loadOptionalMetadata(filepath.Join(skillDir, "tool.json")); err == nil {
				if metadata.Name != "" {
					skill.name = metadata.Name
				}
				if metadata.Description != "" {
					skill.description = metadata.Description
				}
			}

			discovered = append(discovered, skill)
		}
	}

	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].name < discovered[j].name
	})

	return discovered
}

// Info returns tool metadata.
func (t *CLIToolSkill) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        t.name,
		Description: t.description,
		Parameters:  cloneMap(t.schema),
		Required:    cloneStringSlice(t.required),
	}
}

// Run executes the CLI tool with the given parameters as JSON stdin.
func (t *CLIToolSkill) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	cmd := exec.CommandContext(ctx, t.execPath)
	cmd.Dir = filepath.Dir(t.execPath)
	cmd.Stdin = strings.NewReader(params.Input)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return tools.NewTextErrorResponse(message), nil
	}

	return tools.NewTextResponse(stdout.String()), nil
}

func readOptionalTrimmedFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(content)), nil
}

func loadOptionalMetadata(path string) (cliToolMetadata, error) {
	var metadata cliToolMetadata

	content, err := os.ReadFile(path)
	if err != nil {
		return metadata, err
	}

	if err := json.Unmarshal(content, &metadata); err != nil {
		return metadata, fmt.Errorf("parse %s: %w", path, err)
	}

	return metadata, nil
}

func loadOptionalSchema(path string) (map[string]any, []string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	var schema map[string]any
	if err := json.Unmarshal(content, &schema); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	parameters := map[string]any{}
	if properties, ok := schema["properties"].(map[string]any); ok {
		parameters = cloneMap(properties)
	} else {
		parameters = cloneMap(schema)
	}

	required := make([]string, 0)
	switch values := schema["required"].(type) {
	case []any:
		for _, value := range values {
			if name, ok := value.(string); ok {
				required = append(required, name)
			}
		}
	case []string:
		required = append(required, values...)
	}

	return parameters, required, nil
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}

	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneValue(value)
	}
	return cloned
}

func cloneSlice(input []any) []any {
	if input == nil {
		return []any{}
	}

	cloned := make([]any, len(input))
	for i, value := range input {
		cloned[i] = cloneValue(value)
	}
	return cloned
}

func cloneStringSlice(input []string) []string {
	if input == nil {
		return []string{}
	}

	cloned := make([]string, len(input))
	copy(cloned, input)
	return cloned
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	case []string:
		return cloneStringSlice(typed)
	default:
		return typed
	}
}
