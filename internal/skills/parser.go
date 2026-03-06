package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const SkillFileName = "SKILL.md"

func ParseSkillFile(path string) (*Skill, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file %q: %w", path, err)
	}

	frontmatter, body, err := splitFrontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("parse skill file %q: %w", path, err)
	}

	var metadata SkillMetadata
	if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
		return nil, fmt.Errorf("unmarshal skill metadata %q: %w", path, err)
	}

	instructions := strings.TrimSpace(body)
	if metadata.Name == "" {
		metadata.Name = filepath.Base(filepath.Dir(path))
	}
	if metadata.Description == "" {
		metadata.Description = firstParagraph(instructions)
	}

	return &Skill{
		Metadata:     metadata,
		Instructions: instructions,
		SourcePath:   path,
		LoadedLevel:  LevelMetadata,
		LastAccessed: time.Now(),
	}, nil
}

func splitFrontmatter(content string) (string, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimPrefix(normalized, "\ufeff")

	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", fmt.Errorf("missing YAML frontmatter opening delimiter")
	}

	closingIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closingIndex = i
			break
		}
	}
	if closingIndex == -1 {
		return "", "", fmt.Errorf("missing YAML frontmatter closing delimiter")
	}

	frontmatter := strings.Join(lines[1:closingIndex], "\n")
	body := strings.Join(lines[closingIndex+1:], "\n")
	return frontmatter, body, nil
}

func firstParagraph(body string) string {
	for _, paragraph := range strings.Split(strings.TrimSpace(body), "\n\n") {
		trimmed := strings.TrimSpace(paragraph)
		if trimmed == "" {
			continue
		}
		return strings.Join(strings.Fields(trimmed), " ")
	}
	return ""
}
