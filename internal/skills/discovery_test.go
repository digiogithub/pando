package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoveryPaths(t *testing.T) {
	t.Parallel()

	workDir := "/tmp/project"
	home, _ := os.UserHomeDir()

	got := DiscoveryPaths(workDir)
	want := []string{
		filepath.Join(home, ".pando", "skills"),
		filepath.Join(workDir, ".pando", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(workDir, ".claude", "skills"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoveryPaths() = %v, want %v", got, want)
	}
}

func TestDiscoverSkillsPrecedenceAndSorting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	highPrecedence := filepath.Join(root, "high")
	lowPrecedence := filepath.Join(root, "low")

	writeSkillFixture(t, filepath.Join(highPrecedence, "nested"), "alpha", "high alpha", nil)
	writeSkillFixture(t, lowPrecedence, "alpha", "low alpha", nil)
	writeSkillFixture(t, lowPrecedence, "beta", "beta instructions", nil)

	skills, err := DiscoverSkills([]string{highPrecedence, lowPrecedence})
	if err != nil {
		t.Fatalf("DiscoverSkills() error = %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	if skills[0].Metadata.Name != "alpha" || skills[1].Metadata.Name != "beta" {
		t.Fatalf("unexpected skill order: got %q, %q", skills[0].Metadata.Name, skills[1].Metadata.Name)
	}
	if skills[0].Instructions != "high alpha" {
		t.Fatalf("expected higher-precedence alpha instructions, got %q", skills[0].Instructions)
	}
}

func TestDiscoverSkillsSkipsMissingAndInvalidFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	validRoot := filepath.Join(root, "valid")
	invalidDir := filepath.Join(root, "invalid", "broken")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatalf("mkdir invalid dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, SkillFileName), []byte("not frontmatter"), 0o644); err != nil {
		t.Fatalf("write invalid skill file: %v", err)
	}

	writeSkillFixture(t, validRoot, "gamma", "gamma instructions", nil)

	skills, err := DiscoverSkills([]string{
		filepath.Join(root, "missing"),
		filepath.Join(root, "invalid"),
		validRoot,
	})
	if err != nil {
		t.Fatalf("DiscoverSkills() error = %v", err)
	}

	if len(skills) != 1 || skills[0].Metadata.Name != "gamma" {
		t.Fatalf("expected only gamma skill, got %+v", skills)
	}
}
