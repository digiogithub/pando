package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrSkillAlreadyInstalled is returned when a skill is already installed at the target location.
var ErrSkillAlreadyInstalled = errors.New("skill already installed")

// validateSkillName rejects names that contain path separators or parent-directory references.
func validateSkillName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid skill name: %q", name)
	}
	return nil
}

// InstallSkill writes SKILL.md content to {targetDir}/{skillName}/SKILL.md.
// Returns ErrSkillAlreadyInstalled if the file already exists.
func InstallSkill(content, skillName, targetDir string) error {
	if err := validateSkillName(skillName); err != nil {
		return err
	}
	skillDir := filepath.Join(targetDir, skillName)
	skillFile := filepath.Join(skillDir, "SKILL.md")

	if _, err := os.Stat(skillFile); err == nil {
		return ErrSkillAlreadyInstalled
	}

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}

	return os.WriteFile(skillFile, []byte(content), 0o644)
}

// UninstallSkill removes the {targetDir}/{skillName}/ directory and all its contents.
func UninstallSkill(skillName, targetDir string) error {
	if err := validateSkillName(skillName); err != nil {
		return err
	}
	skillDir := filepath.Join(targetDir, skillName)
	if err := os.RemoveAll(skillDir); err != nil {
		return err
	}
	return nil
}

// IsSkillInstalled reports whether {targetDir}/{skillName}/SKILL.md exists.
func IsSkillInstalled(skillName, targetDir string) bool {
	if err := validateSkillName(skillName); err != nil {
		return false
	}
	skillFile := filepath.Join(targetDir, skillName, "SKILL.md")
	_, err := os.Stat(skillFile)
	return err == nil
}

// ResolveSkillsDir returns the skills directory path.
// If projectLocal is true, returns ".pando/skills/" relative to the working directory.
// Otherwise returns ~/.pando/skills/.
func ResolveSkillsDir(projectLocal bool) string {
	if projectLocal {
		return filepath.Join(".pando", "skills")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".pando", "skills")
	}
	return filepath.Join(homeDir, ".pando", "skills")
}
