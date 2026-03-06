package skills

import "time"

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
