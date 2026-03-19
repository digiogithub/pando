package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallSkill(t *testing.T) {
	dir := t.TempDir()
	content := "# My Skill\nDoes something useful."

	if err := InstallSkill(content, "my-skill", dir); err != nil {
		t.Fatalf("InstallSkill: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed file: %v", err)
	}
	if string(data) != content {
		t.Errorf("content mismatch: got %q, want %q", string(data), content)
	}
}

func TestInstallSkillAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	content := "# Skill"

	if err := InstallSkill(content, "dup-skill", dir); err != nil {
		t.Fatalf("first install: %v", err)
	}

	err := InstallSkill(content, "dup-skill", dir)
	if !errors.Is(err, ErrSkillAlreadyInstalled) {
		t.Errorf("expected ErrSkillAlreadyInstalled, got %v", err)
	}
}

func TestUninstallSkill(t *testing.T) {
	dir := t.TempDir()
	if err := InstallSkill("content", "to-remove", dir); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := UninstallSkill("to-remove", dir); err != nil {
		t.Fatalf("UninstallSkill: %v", err)
	}

	if IsSkillInstalled("to-remove", dir) {
		t.Error("skill still reported as installed after uninstall")
	}
}

func TestUninstallSkillNotExist(t *testing.T) {
	dir := t.TempDir()
	// Should not return an error when the skill doesn't exist
	if err := UninstallSkill("ghost", dir); err != nil {
		t.Errorf("expected no error removing non-existent skill, got %v", err)
	}
}

func TestIsSkillInstalled(t *testing.T) {
	dir := t.TempDir()

	if IsSkillInstalled("absent", dir) {
		t.Error("absent skill should not be installed")
	}

	if err := InstallSkill("content", "present", dir); err != nil {
		t.Fatalf("install: %v", err)
	}

	if !IsSkillInstalled("present", dir) {
		t.Error("present skill should be installed")
	}
}

func TestResolveSkillsDir(t *testing.T) {
	global := ResolveSkillsDir(false)
	if global == "" {
		t.Error("global skills dir should not be empty")
	}

	local := ResolveSkillsDir(true)
	expected := filepath.Join(".pando", "skills")
	if local != expected {
		t.Errorf("local skills dir: got %q, want %q", local, expected)
	}
}
