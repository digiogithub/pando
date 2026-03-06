package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextManagerActivateSkillEvictsLRU(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	alphaPath := writeSkillFixture(t, root, "alpha", strings.Repeat("a", 16), nil)
	betaPath := writeSkillFixture(t, root, "beta", strings.Repeat("b", 16), nil)
	gammaPath := writeSkillFixture(t, root, "gamma", strings.Repeat("c", 16), nil)

	manager := NewSkillManager(0)
	if err := manager.LoadAll([]string{alphaPath, betaPath, gammaPath}); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	cm := NewContextManager(manager, 10)

	if _, err := cm.ActivateSkill("alpha"); err != nil {
		t.Fatalf("ActivateSkill(alpha) error = %v", err)
	}
	if _, err := cm.ActivateSkill("beta"); err != nil {
		t.Fatalf("ActivateSkill(beta) error = %v", err)
	}
	if _, err := cm.ActivateSkill("gamma"); err != nil {
		t.Fatalf("ActivateSkill(gamma) error = %v", err)
	}

	if manager.skills["alpha"].LoadedLevel != LevelMetadata {
		t.Fatalf("expected alpha to be evicted, got level %v", manager.skills["alpha"].LoadedLevel)
	}
	if manager.skills["beta"].LoadedLevel != LevelInstructions {
		t.Fatalf("expected beta to remain active, got level %v", manager.skills["beta"].LoadedLevel)
	}
	if manager.skills["gamma"].LoadedLevel != LevelInstructions {
		t.Fatalf("expected gamma to remain active, got level %v", manager.skills["gamma"].LoadedLevel)
	}

	used, max := cm.TokenUsage()
	if used != 8 || max != 10 {
		t.Fatalf("unexpected token usage used=%d max=%d", used, max)
	}
}

func TestContextManagerMetadataPromptAndActivationHints(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sqlPath := writeSkillWithMetadataFixture(t, root, "sql", "Tune SQL queries safely.", "postgres tuning, query optimization, slow sql")
	docsPath := writeSkillWithMetadataFixture(t, root, "docs", "Write concise release notes.", "release notes, documentation updates")

	manager := NewSkillManager(0)
	if err := manager.LoadAll([]string{sqlPath, docsPath}); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	cm := NewContextManager(manager, 100)
	prompt := cm.GetMetadataPrompt()

	if !strings.Contains(prompt, "sql: Tune SQL queries safely. (when-to-use: postgres tuning, query optimization, slow sql)") {
		t.Fatalf("metadata prompt missing sql skill: %q", prompt)
	}
	if !strings.Contains(prompt, "docs: Write concise release notes. (when-to-use: release notes, documentation updates)") {
		t.Fatalf("metadata prompt missing docs skill: %q", prompt)
	}

	matches := cm.ShouldActivate("Please help optimize a slow SQL query in Postgres.")
	if len(matches) != 1 || matches[0] != "sql" {
		t.Fatalf("unexpected activation matches %v", matches)
	}
}

func writeSkillWithMetadataFixture(t *testing.T, root, name, description, whenToUse string) string {
	t.Helper()

	skillDir := filepath.Join(root, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	skillPath := filepath.Join(skillDir, SkillFileName)
	content := strings.Join([]string{
		"---",
		"name: " + name,
		"description: " + description,
		"when-to-use: " + whenToUse,
		"---",
		description,
		"",
	}, "\n")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	return skillPath
}
