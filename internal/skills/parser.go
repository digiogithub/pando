package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/skills/od"
	"gopkg.in/yaml.v3"
)

const SkillFileName = "SKILL.md"

func ParseSkillFile(path string) (*Skill, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file %q: %w", path, err)
	}

	skill, err := ParseSkillContent(string(content), filepath.Base(filepath.Dir(path)))
	if err != nil {
		return nil, fmt.Errorf("skill file %q: %w", path, err)
	}
	skill.SourcePath = path
	return skill, nil
}

// ParseSkillContent parses a SKILL.md document that is not necessarily on disk
// — an embedded bundle, or one fetched from the catalog. defaultName is used
// when the frontmatter declares no name; on disk that is the directory name.
func ParseSkillContent(content, defaultName string) (*Skill, error) {
	path := defaultName

	frontmatter, body, err := od.SplitFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse skill file %q: %w", path, err)
	}

	var metadata SkillMetadata
	if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
		// A third-party `od:` block whose shape we do not understand must not
		// make the whole skill invisible: drop the block and keep the skill.
		stripped, ok := od.Strip(frontmatter)
		if !ok {
			return nil, fmt.Errorf("unmarshal skill metadata %q: %w", path, err)
		}
		metadata = SkillMetadata{}
		if err := yaml.Unmarshal([]byte(stripped), &metadata); err != nil {
			return nil, fmt.Errorf("unmarshal skill metadata %q: %w", path, err)
		}
	}

	instructions := strings.TrimSpace(body)
	if metadata.Name == "" {
		metadata.Name = defaultName
	}
	if metadata.Description == "" {
		metadata.Description = firstParagraph(instructions)
	}

	return &Skill{
		Metadata:     metadata,
		Instructions: instructions,
		LoadedLevel:  LevelMetadata,
		LastAccessed: time.Now(),
	}, nil
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
