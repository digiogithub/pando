package skills

import (
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/skills/od"
)

type SkillMetadata struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	Version                string `yaml:"version"`
	Author                 string `yaml:"author"`
	License                string `yaml:"license"`
	Compatibility          string `yaml:"compatibility"`
	AllowedTools           string `yaml:"allowed-tools"`
	UserInvocable          bool   `yaml:"user-invocable"`
	WhenToUse              string `yaml:"when-to-use"`
	WhenNotToUse           string `yaml:"when-not-to-use"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
	Context                string `yaml:"context"`
	// OD is the optional OpenDesign `od:` block (see od.Metadata).
	OD *od.Metadata `yaml:"od"`
}

type SkillLevel int

const (
	LevelMetadata     SkillLevel = 1
	LevelInstructions SkillLevel = 2
	LevelResources    SkillLevel = 3
)

type Skill struct {
	Metadata     SkillMetadata
	Instructions string
	Resources    []SkillResource
	SourcePath   string
	LoadedLevel  SkillLevel
	LastAccessed time.Time
}

type SkillResource struct {
	Path    string
	Content []byte
}

// IsDesignTemplate reports whether the skill offers itself as a design
// template. A reference or workflow bundle is real and useful, and must not
// appear as something a user can start an artifact from.
func (m SkillMetadata) IsDesignTemplate() bool {
	if m.OD == nil {
		return false
	}
	if strings.EqualFold(m.OD.Mode, "reference") {
		return false
	}
	return strings.TrimSpace(m.OD.Surface) != ""
}
