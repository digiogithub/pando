package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillFileDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	skillDir := filepath.Join(dir, "example-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	skillPath := filepath.Join(skillDir, SkillFileName)
	content := `---
version: 1.0.0
user-invocable: true
---
First paragraph for description.

Second paragraph here.
`
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	skill, err := ParseSkillFile(skillPath)
	if err != nil {
		t.Fatalf("ParseSkillFile() error = %v", err)
	}

	if skill.Metadata.Name != "example-skill" {
		t.Fatalf("expected default name %q, got %q", "example-skill", skill.Metadata.Name)
	}
	if skill.Metadata.Description != "First paragraph for description." {
		t.Fatalf("expected derived description, got %q", skill.Metadata.Description)
	}
	if skill.LoadedLevel != LevelMetadata {
		t.Fatalf("expected loaded level %v, got %v", LevelMetadata, skill.LoadedLevel)
	}
	if skill.Instructions != "First paragraph for description.\n\nSecond paragraph here." {
		t.Fatalf("unexpected instructions %q", skill.Instructions)
	}
}
