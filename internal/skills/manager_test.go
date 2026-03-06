package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillManagerLoadCacheAndRecall(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	alphaPath := writeSkillFixture(t, root, "alpha", "Alpha instructions.", map[string]string{"guide.txt": "alpha resource"})
	betaPath := writeSkillFixture(t, root, "beta", "Beta instructions.", nil)

	manager := NewSkillManager(1)
	if err := manager.LoadAll([]string{filepath.Dir(alphaPath), filepath.Dir(betaPath)}); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	metadata := manager.GetAllMetadata()
	if len(metadata) != 2 {
		t.Fatalf("expected 2 metadata entries, got %d", len(metadata))
	}

	alphaInstructions, err := manager.GetInstructions("alpha")
	if err != nil {
		t.Fatalf("GetInstructions(alpha) error = %v", err)
	}
	if alphaInstructions != "Alpha instructions." {
		t.Fatalf("unexpected alpha instructions %q", alphaInstructions)
	}

	resource, err := manager.GetResource("alpha", "guide.txt")
	if err != nil {
		t.Fatalf("GetResource(alpha) error = %v", err)
	}
	if string(resource) != "alpha resource" {
		t.Fatalf("unexpected resource content %q", string(resource))
	}

	if _, err := manager.GetInstructions("beta"); err != nil {
		t.Fatalf("GetInstructions(beta) error = %v", err)
	}
	if manager.skills["alpha"].LoadedLevel != LevelMetadata {
		t.Fatalf("expected alpha to be evicted, got level %v", manager.skills["alpha"].LoadedLevel)
	}

	if err := manager.Recall("alpha"); err != nil {
		t.Fatalf("Recall(alpha) error = %v", err)
	}
	if manager.skills["alpha"].Instructions != "Alpha instructions." {
		t.Fatalf("expected alpha instructions to reload, got %q", manager.skills["alpha"].Instructions)
	}
}

func TestSkillManagerRejectsEscapingResources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillPath := writeSkillFixture(t, root, "alpha", "Alpha instructions.", nil)
	outsidePath := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	manager := NewSkillManager(1)
	if err := manager.LoadAll([]string{skillPath}); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	if _, err := manager.GetResource("alpha", "../outside.txt"); err == nil {
		t.Fatalf("expected escaping resource path to fail")
	}
}

func TestSkillManagerSetLoaded(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillPath := writeSkillFixture(t, root, "alpha", "Alpha instructions.", nil)

	manager := NewSkillManager(1)
	if err := manager.LoadAll([]string{skillPath}); err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}

	if manager.IsLoaded("alpha") {
		t.Fatalf("expected alpha to start unloaded")
	}

	if err := manager.SetLoaded("alpha", true); err != nil {
		t.Fatalf("SetLoaded(alpha, true) error = %v", err)
	}
	if !manager.IsLoaded("alpha") {
		t.Fatalf("expected alpha to be loaded")
	}

	if err := manager.SetLoaded("alpha", false); err != nil {
		t.Fatalf("SetLoaded(alpha, false) error = %v", err)
	}
	if manager.IsLoaded("alpha") {
		t.Fatalf("expected alpha to be unloaded")
	}
	if manager.skills["alpha"].LoadedLevel != LevelMetadata {
		t.Fatalf("expected alpha level to reset to metadata, got %v", manager.skills["alpha"].LoadedLevel)
	}
}

func writeSkillFixture(t *testing.T, root, name, instructions string, resources map[string]string) string {
	t.Helper()

	skillDir := filepath.Join(root, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	skillPath := filepath.Join(skillDir, SkillFileName)
	content := "---\nname: " + name + "\n---\n" + instructions + "\n"
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	for relativePath, resourceContent := range resources {
		resourcePath := filepath.Join(skillDir, relativePath)
		if err := os.MkdirAll(filepath.Dir(resourcePath), 0o755); err != nil {
			t.Fatalf("mkdir resource dir: %v", err)
		}
		if err := os.WriteFile(resourcePath, []byte(resourceContent), 0o644); err != nil {
			t.Fatalf("write resource file: %v", err)
		}
	}

	return skillPath
}
