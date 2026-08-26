package design

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPickATemplateAndGetTheArtifact is the P7 exit criterion: a user picks a
// template, says what they want, and gets an artifact.
//
// The brief itself is prose the agent turns into content, which no unit test
// can assert. What is asserted is everything the template is responsible for:
// the artifact comes out as the kind the template declares, seeded with the
// template's files, wired to the shared design system, and recorded as version
// 1 — so the agent's first edit already has somewhere to land.
func TestPickATemplateAndGetTheArtifact(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)

	if _, _, err := svc.SaveSystem(DefaultDesignSystem()); err != nil {
		t.Fatalf("save system: %v", err)
	}

	// The user picked "deck-basic" and never said "deck": the template knows.
	artifact, err := svc.Create(ctx, CreateParams{
		Title:   "Incident Review",
		SkillID: "deck-basic",
	})
	if err != nil {
		t.Fatalf("create from template: %v", err)
	}
	if artifact.Kind != KindDeck {
		t.Fatalf("template surface was ignored: kind = %q", artifact.Kind)
	}
	if artifact.SkillID != "deck-basic" {
		t.Errorf("the artifact does not record the template it came from: %q", artifact.SkillID)
	}
	if artifact.CurrentVersion != 1 {
		t.Errorf("version 1 was not recorded, got %d", artifact.CurrentVersion)
	}

	absDir, err := svc.AbsDir(artifact)
	if err != nil {
		t.Fatalf("abs dir: %v", err)
	}
	entry, err := os.ReadFile(filepath.Join(absDir, "index.html"))
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	body := string(entry)
	if !strings.Contains(body, "Incident Review") {
		t.Error("the title never reached the scaffold")
	}
	if strings.Contains(body, "Empty deck") {
		t.Error("the placeholder was written instead of the template scaffold")
	}
	if _, err := os.Stat(filepath.Join(absDir, "style.css")); err != nil {
		t.Errorf("the template stylesheet was not seeded: %v", err)
	}

	// The manifest carries the template's preview viewport, which is what the
	// renderer and the PDF export read.
	manifest, err := ReadManifest(absDir)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if manifest.Skill != "deck-basic" {
		t.Errorf("manifest skill = %q", manifest.Skill)
	}
	tpl, _ := BundledTemplate("deck-basic")
	if manifest.Preview.Viewport != tpl.Viewport {
		t.Errorf("manifest viewport = %v, template declares %v", manifest.Preview.Viewport, tpl.Viewport)
	}

	// The scaffold links the shared system, so applying it is a no-op rather
	// than a second link: a template that produced a duplicate <link> would
	// grow one on every apply.
	result, err := svc.ApplySystem(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("apply system: %v", err)
	}
	if result.Linked {
		t.Error("the template scaffold should already link the design system")
	}
	if strings.Count(body, "_system/system.css") != 1 {
		t.Errorf("expected exactly one stylesheet link, found %d", strings.Count(body, "_system/system.css"))
	}
	if len(result.Findings) > 0 {
		t.Errorf("the template scaffold contradicts the design system: %v", result.Findings)
	}
}

// A template name nobody ships must not silently produce a placeholder that
// looks like a scaffold: the artifact is still created (a third-party bundle is
// legitimate) but it records the name it was given.
func TestCreateWithAnUnknownTemplateStillWorks(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)

	artifact, err := svc.Create(ctx, CreateParams{Title: "Something", SkillID: "not-a-bundle"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if artifact.Kind != KindWeb {
		t.Errorf("expected the default kind, got %q", artifact.Kind)
	}
	if artifact.SkillID != "not-a-bundle" {
		t.Errorf("the skill id was dropped: %q", artifact.SkillID)
	}
}
