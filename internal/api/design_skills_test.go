package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/design"
	"github.com/digiogithub/pando/internal/skills"
)

// The WebUI gallery is driven entirely over HTTP, so the two calls it makes are
// the contract: list what can be built, and install one so the agent's skill
// loader can read it in later sessions.
func TestDesignSkillsGalleryHTTP(t *testing.T) {
	s, _ := designStudioServer(t)

	var listing struct {
		Skills []design.Template `json:"skills"`
		Craft  []string          `json:"craft"`
	}
	if code := call(t, s, http.MethodGet, "/api/v1/design/skills", "", &listing); code != http.StatusOK {
		t.Fatalf("GET skills = %d", code)
	}
	if len(listing.Skills) == 0 {
		t.Fatal("the gallery is empty")
	}
	if len(listing.Craft) == 0 {
		t.Fatal("no craft references were listed")
	}

	var landing design.Template
	for _, tpl := range listing.Skills {
		if tpl.Name == "landing-page" {
			landing = tpl
		}
	}
	if landing.Name == "" {
		t.Fatal("landing-page is missing from the gallery")
	}
	if !landing.Startable || landing.Kind != design.KindWeb {
		t.Errorf("landing-page: startable=%v kind=%q", landing.Startable, landing.Kind)
	}
	if landing.ExamplePrompt == "" {
		t.Error("the gallery has no brief to offer for landing-page")
	}
	if landing.Installed {
		t.Error("a fresh project has nothing installed yet")
	}

	// Installing writes into the project's own skills root, not the user's home.
	var installed struct {
		Name  string   `json:"name"`
		Scope string   `json:"scope"`
		Dir   string   `json:"dir"`
		Files []string `json:"files"`
	}
	if code := call(t, s, http.MethodPost, "/api/v1/design/skills/landing-page/install", `{"scope":"project"}`, &installed); code != http.StatusOK {
		t.Fatalf("POST install = %d", code)
	}
	if installed.Scope != "project" {
		t.Errorf("scope = %q", installed.Scope)
	}
	workDir := config.WorkingDirectory()
	if !strings.HasPrefix(installed.Dir, workDir) {
		t.Errorf("install dir %q escaped the project %q", installed.Dir, workDir)
	}
	if _, err := os.Stat(filepath.Join(installed.Dir, "landing-page", "SKILL.md")); err != nil {
		t.Fatalf("the skill was not written: %v", err)
	}
	if len(installed.Files) < 2 {
		t.Errorf("expected the skill plus its craft references, got %v", installed.Files)
	}

	// The point of installing is that the agent's own skill loader reads it,
	// so the seam between the two subsystems is checked end to end rather than
	// assumed: discovery must find the bundle and parse its od: block.
	discovered, err := skills.DiscoverSkills([]string{installed.Dir})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	var found *skills.Skill
	for _, sk := range discovered {
		if sk.Metadata.Name == "landing-page" {
			found = sk
		}
	}
	if found == nil {
		t.Fatal("the installed template is invisible to skill discovery")
	}
	if found.Metadata.OD == nil || found.Metadata.OD.Surface != "web" {
		t.Fatalf("the od: block did not survive the install: %+v", found.Metadata.OD)
	}
	if !found.Metadata.IsDesignTemplate() {
		t.Error("the installed bundle no longer reports itself as a design template")
	}

	// A second install must not quietly replace the copy the user now owns.
	var conflict map[string]any
	if code := call(t, s, http.MethodPost, "/api/v1/design/skills/landing-page/install", `{"scope":"project"}`, &conflict); code != http.StatusConflict {
		t.Errorf("re-install = %d, want 409", code)
	}

	// The second listing must show the project's copy, because that is the one
	// the agent actually reads.
	var second struct {
		Skills []design.Template `json:"skills"`
	}
	if code := call(t, s, http.MethodGet, "/api/v1/design/skills", "", &second); code != http.StatusOK {
		t.Fatalf("GET skills (2) = %d", code)
	}
	for _, tpl := range second.Skills {
		if tpl.Name != "landing-page" {
			continue
		}
		if !tpl.Installed {
			t.Error("landing-page is installed but does not say so")
		}
		if tpl.SourcePath == "" {
			t.Error("an installed template must say where it came from")
		}
	}
}

// A template name is used to build a path, so a name that tries to escape the
// skills root must be refused rather than sanitised into something plausible.
func TestDesignSkillInstallRejectsUnknownName(t *testing.T) {
	s, _ := designStudioServer(t)
	var ignored map[string]any
	if code := call(t, s, http.MethodPost, "/api/v1/design/skills/nope/install", "", &ignored); code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown template, got %d", code)
	}
}
