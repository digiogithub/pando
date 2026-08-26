package design

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/skills/od"
)

// Every bundled template is shipped content, so the things that make it usable
// — a description, a scenario, a starter brief, craft references that exist —
// are asserted rather than assumed. A template missing its example prompt shows
// up in the gallery as a card with nothing to try.
func TestBundledTemplatesAreComplete(t *testing.T) {
	templates, err := BundledTemplates()
	if err != nil {
		t.Fatalf("BundledTemplates: %v", err)
	}
	if len(templates) < 6 {
		t.Fatalf("expected the v1 bundle set, got %d templates", len(templates))
	}

	craft := map[string]bool{}
	for _, name := range CraftReferenceNames() {
		craft[name] = true
	}
	if len(craft) == 0 {
		t.Fatal("no craft references are bundled")
	}

	seen := map[string]bool{}
	for _, tpl := range templates {
		if seen[tpl.Name] {
			t.Errorf("duplicate template name %q", tpl.Name)
		}
		seen[tpl.Name] = true

		if strings.TrimSpace(tpl.Description) == "" {
			t.Errorf("template %q has no description", tpl.Name)
		}
		if strings.TrimSpace(tpl.Scenario) == "" {
			t.Errorf("template %q has no scenario", tpl.Name)
		}
		if tpl.Source != SourceBundled {
			t.Errorf("template %q reports source %q", tpl.Name, tpl.Source)
		}
		for _, ref := range tpl.Craft {
			if !craft[ref] {
				t.Errorf("template %q requires craft reference %q, which is not bundled", tpl.Name, ref)
			}
		}
		if !tpl.Startable {
			continue
		}
		if !ValidKind(tpl.Kind) {
			t.Errorf("startable template %q has kind %q", tpl.Name, tpl.Kind)
		}
		if strings.TrimSpace(tpl.ExamplePrompt) == "" {
			t.Errorf("startable template %q has no example prompt to offer", tpl.Name)
		}
	}

	for _, want := range []string{"landing-page", "web-prototype", "dashboard-page", "deck-basic", "magazine-deck", "design-system-extract"} {
		if !seen[want] {
			t.Errorf("bundle %q is missing", want)
		}
	}
}

// A workflow bundle is real and useful and must not be offered as something an
// artifact can be scaffolded from: there is nothing to scaffold.
func TestWorkflowBundleIsNotStartable(t *testing.T) {
	tpl, ok := BundledTemplate("design-system-extract")
	if !ok {
		t.Fatal("design-system-extract is not bundled")
	}
	if tpl.Startable {
		t.Error("a workflow bundle must not be startable")
	}
	if tpl.Kind != "" {
		t.Errorf("a workflow bundle must not claim a kind, got %q", tpl.Kind)
	}
}

// The scaffold is what the user sees first, so the substitution has to happen
// and the deck print styles — which PDF export depends on — have to survive.
func TestScaffoldSubstitutesTitleAndKeepsPrintStyles(t *testing.T) {
	files, err := Scaffold("deck-basic", "Q4 Review")
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	entry, ok := files["index.html"]
	if !ok {
		t.Fatalf("deck-basic ships no index.html, got %v", keysOf(files))
	}
	if strings.Contains(entry, titlePlaceholder) {
		t.Error("the title placeholder survived into the scaffold")
	}
	if !strings.Contains(entry, "Q4 Review") {
		t.Error("the title was not substituted into the scaffold")
	}
	if !strings.Contains(entry, `data-slide="0"`) {
		t.Error("a deck scaffold must index its slides: the renderer and the PDF export both read data-slide")
	}
	css, ok := files["style.css"]
	if !ok {
		t.Fatal("deck-basic ships no style.css")
	}
	if !strings.Contains(css, "@page") || !strings.Contains(css, "break-after: page") {
		t.Error("the deck print styles are missing: PDF export prints one slide per page and only works with them")
	}
}

// Both deck templates must carry print styles, not just the one that happened
// to be checked above.
func TestEveryDeckScaffoldPrintsOneSlidePerPage(t *testing.T) {
	templates, err := BundledTemplates()
	if err != nil {
		t.Fatalf("BundledTemplates: %v", err)
	}
	for _, tpl := range templates {
		if tpl.Kind != KindDeck {
			continue
		}
		files, err := Scaffold(tpl.Name, "Title")
		if err != nil {
			t.Fatalf("Scaffold(%s): %v", tpl.Name, err)
		}
		joined := strings.Join(valuesOf(files), "\n")
		if !strings.Contains(joined, "@page") {
			t.Errorf("deck template %q ships no @page rule", tpl.Name)
		}
	}
}

// A scaffold that hardcodes a colour defeats the design system every template
// claims to require, so it is checked mechanically rather than by review.
func TestScaffoldsStyleThroughTheDesignSystem(t *testing.T) {
	templates, err := BundledTemplates()
	if err != nil {
		t.Fatalf("BundledTemplates: %v", err)
	}
	for _, tpl := range templates {
		if !tpl.RequiresSystem {
			continue
		}
		files, err := Scaffold(tpl.Name, "Title")
		if err != nil {
			t.Fatalf("Scaffold(%s): %v", tpl.Name, err)
		}
		for name, body := range files {
			if !strings.HasSuffix(name, ".css") {
				continue
			}
			for _, line := range strings.Split(body, "\n") {
				// A fallback inside var() is a token that has not been defined
				// yet, not a hardcoded value: the token still wins when it
				// exists. A bare hex outside var() is the failure this catches.
				if strings.Contains(line, "var(--") {
					continue
				}
				if strings.Contains(line, "#") && !strings.Contains(line, "/*") {
					t.Errorf("%s/%s hardcodes a colour: %s", tpl.Name, name, strings.TrimSpace(line))
				}
			}
		}
	}
}

// Installing writes a self-contained skill directory: the craft references are
// copied in rather than shared, because the skill manager refuses to read a
// resource outside the skill directory.
func TestInstallBundleWritesASelfContainedSkill(t *testing.T) {
	dir := t.TempDir()
	written, err := InstallBundle("landing-page", dir, false)
	if err != nil {
		t.Fatalf("InstallBundle: %v", err)
	}
	if len(written) < 2 {
		t.Fatalf("expected the skill plus its craft references, got %v", written)
	}

	body, err := os.ReadFile(filepath.Join(dir, "landing-page", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if !strings.Contains(string(body), "od:") {
		t.Error("the installed skill lost its od: block")
	}

	// A second install must not silently replace a copy the user may have
	// edited.
	if _, err := InstallBundle("landing-page", dir, false); !errors.Is(err, ErrBundleInstalled) {
		t.Errorf("re-install error = %v, want ErrBundleInstalled", err)
	}
	if _, err := InstallBundle("landing-page", dir, true); err != nil {
		t.Errorf("forced re-install: %v", err)
	}

	tpl, _ := BundledTemplate("landing-page")
	for _, ref := range tpl.Craft {
		path := filepath.Join(dir, "landing-page", craftDirName, ref+".md")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("craft reference %q was not installed: %v", ref, err)
		}
	}
}

// An install target derived from a name is a path, so the name must never be
// able to address anything outside the skills root.
func TestInstallBundleRejectsEscapingNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../escape", "nested/name", "..", ""} {
		if _, err := InstallBundle(name, dir, false); err == nil {
			t.Errorf("InstallBundle(%q) was accepted", name)
		}
	}
}

// The gallery is the union of what we ship and what the project has. A skill
// with no od: block is an ordinary skill and must not be listed as a template.
func TestGalleryPrefersTheInstalledCopyAndIgnoresPlainSkills(t *testing.T) {
	installed, ok := TemplateFromSkill("landing-page", "The project's own landing page rules", &od.Metadata{
		Mode:     "template",
		Surface:  "web",
		Category: "marketing",
	})
	if !ok {
		t.Fatal("a skill with an od: block must convert to a template")
	}
	installed.SourcePath = "/project/.pando/skills/design/landing-page/SKILL.md"

	if _, ok := TemplateFromSkill("format-commit-message", "Nothing to do with design", nil); ok {
		t.Fatal("a skill with no od: block must not become a template")
	}

	gallery := Gallery([]Template{installed})
	var found Template
	for _, tpl := range gallery {
		if tpl.Name == "landing-page" {
			found = tpl
		}
	}
	if found.Name == "" {
		t.Fatal("landing-page is missing from the gallery")
	}
	if found.Description != "The project's own landing page rules" {
		t.Errorf("the bundled copy overrode the installed one: %q", found.Description)
	}
	if !found.Installed {
		t.Error("an installed template must report itself as installed")
	}
	if len(gallery) < len(mustBundled(t)) {
		t.Errorf("gallery dropped bundled entries: %d < %d", len(gallery), len(mustBundled(t)))
	}
}

// A template declaring a surface this build does not support stays listed but
// must not be startable: scaffolding it as "web" would silently build the
// wrong thing.
func TestUnsupportedSurfaceIsListedButNotStartable(t *testing.T) {
	tpl, ok := TemplateFromSkill("mobile-app", "A surface v1 does not build", &od.Metadata{
		Mode:    "template",
		Surface: "mobile",
	})
	if !ok {
		t.Fatal("expected a template")
	}
	if tpl.Startable {
		t.Error("an unsupported surface must not be startable")
	}
	if tpl.Kind != "" {
		t.Errorf("kind should stay empty, got %q", tpl.Kind)
	}
}

func mustBundled(t *testing.T) []Template {
	t.Helper()
	templates, err := BundledTemplates()
	if err != nil {
		t.Fatalf("BundledTemplates: %v", err)
	}
	return templates
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func valuesOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
