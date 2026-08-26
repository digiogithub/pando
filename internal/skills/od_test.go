package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// The `od:` block is OpenDesign's namespace kept verbatim, so what matters is
// that a bundle written for either tool parses here with its nested fields
// intact.
func TestParseODFrontmatter(t *testing.T) {
	skill := parseSkill(t, `---
name: landing-page
description: One page, one promise.
od:
  mode: template
  surface: web
  category: marketing
  scenario: Launch page.
  example_prompt: Build a landing page for a backup tool.
  preview:
    type: page
    viewport:
      width: 1440
      height: 900
  design_system:
    requires: true
  craft:
    requires:
      - typography
      - anti-ai-slop
  critique:
    policy: strict
---

# Landing page
`)

	meta := skill.Metadata
	if meta.OD == nil {
		t.Fatal("the od: block was dropped")
	}
	if meta.OD.Surface != "web" || meta.OD.Mode != "template" {
		t.Errorf("mode/surface = %q/%q", meta.OD.Mode, meta.OD.Surface)
	}
	if meta.OD.Preview.Viewport.Width != 1440 || meta.OD.Preview.Viewport.Height != 900 {
		t.Errorf("viewport = %dx%d", meta.OD.Preview.Viewport.Width, meta.OD.Preview.Viewport.Height)
	}
	if !meta.OD.DesignSystem.Requires {
		t.Error("design_system.requires was lost")
	}
	if len(meta.OD.Craft.Requires) != 2 {
		t.Errorf("craft.requires = %v", meta.OD.Craft.Requires)
	}
	if meta.OD.Critique.Policy != "strict" {
		t.Errorf("critique.policy = %q", meta.OD.Critique.Policy)
	}
	if !meta.IsDesignTemplate() {
		t.Error("a bundle with a surface must count as a design template")
	}
}

// An ordinary Claude Code skill has no od: block and must keep working
// unchanged — zero configuration is the whole point of the namespace being
// optional.
func TestSkillWithoutODStillParses(t *testing.T) {
	skill := parseSkill(t, `---
name: format-commit-message
description: Write a conventional commit.
---

Body.
`)
	if skill.Metadata.OD != nil {
		t.Error("an od: block appeared out of nowhere")
	}
	if skill.Metadata.IsDesignTemplate() {
		t.Error("a plain skill must not be a design template")
	}
}

// A reference bundle is craft guidance, not something an artifact is started
// from, even though it carries an od: block.
func TestReferenceModeIsNotATemplate(t *testing.T) {
	skill := parseSkill(t, `---
name: typography
description: Type rules.
od:
  mode: reference
  surface: web
---

Body.
`)
	if skill.Metadata.IsDesignTemplate() {
		t.Error("mode: reference must not be startable")
	}
}

// A third-party od: block we cannot read must not make the whole skill
// invisible: the skill is still a skill, we simply know nothing about its
// design metadata.
func TestUnreadableODBlockKeepsTheSkill(t *testing.T) {
	skill := parseSkill(t, `---
name: exotic
description: From another tool.
od: "a scalar where we expect a mapping"
---

Body.
`)
	if skill.Metadata.Name != "exotic" {
		t.Fatalf("the skill was lost: %q", skill.Metadata.Name)
	}
	if skill.Metadata.OD != nil {
		t.Error("an unreadable od: block must be dropped, not guessed at")
	}
}

func parseSkill(t *testing.T, content string) *Skill {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, SkillFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	skill, err := ParseSkillFile(path)
	if err != nil {
		t.Fatalf("ParseSkillFile: %v", err)
	}
	return skill
}
