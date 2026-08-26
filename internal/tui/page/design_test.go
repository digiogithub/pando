package page

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/design"
)

// The TUI is often started in a process that never wired the design subsystem
// (a stripped entry point, an old config). Opening the page must then say so,
// not panic or show an empty list that reads like "you have no artifacts".
func TestDesignPageReportsAMissingSubsystem(t *testing.T) {
	p := NewDesignPage("").(*designPage)

	msg := p.loadArtifacts()()
	loaded, ok := msg.(designArtifactsMsg)
	if !ok {
		t.Fatalf("expected designArtifactsMsg, got %T", msg)
	}
	if loaded.err == nil {
		t.Fatal("a process with no design provider must produce an error")
	}

	model, _ := p.Update(loaded)
	page := model.(*designPage)
	page.SetSize(80, 24)
	view := page.View()
	if !strings.Contains(view, "not available") {
		t.Fatalf("the view should explain the missing subsystem:\n%s", view)
	}
}

// An empty project must not look like a broken one.
func TestDesignPageEmptyStateInvitesTheAgent(t *testing.T) {
	p := NewDesignPage("").(*designPage)
	model, _ := p.Update(designArtifactsMsg{artifacts: nil})
	page := model.(*designPage)
	page.SetSize(80, 24)

	view := page.View()
	if !strings.Contains(view, "No design artifacts") || !strings.Contains(view, "chat tab") {
		t.Fatalf("empty state should point at the chat tab:\n%s", view)
	}
}

// Selection drives which artifact the detail panel, `o`, `s` and `d` act on, so
// the bounds have to hold at both ends of the list.
func TestDesignPageSelectionStaysInBounds(t *testing.T) {
	p := NewDesignPage("").(*designPage)
	p.Update(designArtifactsMsg{artifacts: []design.Artifact{
		{ID: "dsg_a", Slug: "a", Kind: design.KindWeb, CurrentVersion: 1},
		{ID: "dsg_b", Slug: "b", Kind: design.KindDeck, CurrentVersion: 3},
	}})

	p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if p.selected != 0 {
		t.Fatalf("up at the top moved to %d", p.selected)
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if p.selected != 1 {
		t.Fatalf("down moved to %d, want 1", p.selected)
	}
	p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if p.selected != 1 {
		t.Fatalf("down at the bottom moved to %d", p.selected)
	}
	if current, ok := p.current(); !ok || current.ID != "dsg_b" {
		t.Fatalf("current() = %+v", current)
	}
}

// A detail reply that arrives after the user has moved on belongs to a
// different artifact and must be dropped, or the panel shows one artifact's
// versions under another's title.
func TestDesignPageDropsStaleDetailReplies(t *testing.T) {
	p := NewDesignPage("").(*designPage)
	p.Update(designArtifactsMsg{artifacts: []design.Artifact{
		{ID: "dsg_a", Slug: "a", CurrentVersion: 1},
		{ID: "dsg_b", Slug: "b", CurrentVersion: 1},
	}})
	p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}) // now on dsg_b

	p.Update(designDetailMsg{artifactID: "dsg_a", versions: []design.Version{{Number: 1}}})
	if len(p.versions) != 0 {
		t.Fatalf("a reply for dsg_a was applied while dsg_b is selected: %+v", p.versions)
	}

	p.Update(designDetailMsg{artifactID: "dsg_b", versions: []design.Version{{Number: 1}}})
	if len(p.versions) != 1 {
		t.Fatal("the reply for the selected artifact was dropped")
	}
}

// The page is Refreshable so navigating back to it re-reads the list: artifacts
// are created from the chat page, while this page is not on screen.
func TestDesignPageIsRefreshable(t *testing.T) {
	var p any = NewDesignPage("")
	refreshable, ok := p.(Refreshable)
	if !ok {
		t.Fatal("the design page must implement Refreshable")
	}
	if refreshable.Refresh() == nil {
		t.Fatal("Refresh must return a command that reloads the list")
	}
}

// The key help is the only place the page's verbs are documented.
func TestDesignPageAdvertisesItsKeys(t *testing.T) {
	bindings := NewDesignPage("").BindingKeys()
	want := map[string]bool{"o": false, "s": false, "c": false, "d": false, "r": false}
	for _, binding := range bindings {
		for _, k := range binding.Keys() {
			if _, ok := want[k]; ok {
				want[k] = true
			}
		}
	}
	for k, found := range want {
		if !found {
			t.Errorf("key %q is not advertised in the page help", k)
		}
	}
}

func TestDesignPageErrorTextUsesTheSentinel(t *testing.T) {
	p := NewDesignPage("").(*designPage)
	p.err = design.ErrNoProvider
	if got := p.designErrorText(); !strings.Contains(got, "not available") {
		t.Fatalf("designErrorText = %q", got)
	}
}

// The design system is a project-wide setting, so the page shows it in a panel
// of its own rather than as a property of whichever artifact is selected.
func TestDesignPageSystemPanelToggles(t *testing.T) {
	p := NewDesignPage("").(*designPage)
	p.SetSize(120, 40)

	if p.systemView {
		t.Fatal("the system panel must start closed")
	}
	if _, cmd := p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); cmd == nil {
		t.Fatal("opening the system panel must load the system")
	}
	if !p.systemView {
		t.Fatal("y did not open the system panel")
	}
	// Closing it must not re-read anything.
	if _, cmd := p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); cmd != nil {
		t.Error("closing the panel should issue no command")
	}
	if p.systemView {
		t.Error("y did not close the system panel")
	}
}

// Digits adopt a bundled guide, which writes to the project. They must do
// nothing at all while the panel is closed, or an artifact list keystroke would
// silently replace the design system.
func TestDesignPageDigitsOnlyActWithTheSystemPanelOpen(t *testing.T) {
	p := NewDesignPage("").(*designPage)
	if _, cmd := p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")}); cmd != nil {
		t.Fatal("a digit outside the system panel must not act")
	}
	p.systemView = true
	if _, cmd := p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")}); cmd == nil {
		t.Fatal("a digit inside the system panel must adopt a bundled guide")
	}
	// There are fewer than nine bundled guides, so a high digit is a no-op
	// rather than an index out of range.
	if _, cmd := p.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")}); cmd != nil {
		t.Error("a digit past the last bundled guide must do nothing")
	}
}

func TestDesignPageSystemPanelRendersTokensAndGuides(t *testing.T) {
	p := NewDesignPage("").(*designPage)
	p.SetSize(120, 40)
	p.systemView = true
	system := design.DefaultDesignSystem()
	p.system, p.systemExists = &system, true

	view := p.renderSystem(80)
	for _, want := range []string{"DESIGN SYSTEM", "--color-accent", "REPLACE WITH A BUNDLED GUIDE"} {
		if !strings.Contains(view, want) {
			t.Errorf("system panel is missing %q:\n%s", want, view)
		}
	}
	for _, name := range design.ExampleSystemNames() {
		if !strings.Contains(view, name) {
			t.Errorf("system panel omits the bundled guide %q", name)
		}
	}
}

// The template panel is the TUI's half of the gallery. It must open on its own
// key, close the system panel (they share the detail column), and list the
// bundled templates with the brief each one expects.
func TestDesignPageTemplatePanel(t *testing.T) {
	page := &designPage{width: 100, height: 30}

	page.systemView = true
	if _, _ = page.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}); !page.galleryView {
		t.Fatal("g did not open the template panel")
	}
	if page.systemView {
		t.Error("the system panel must close: both panels own the same column")
	}
	if len(page.templates) == 0 {
		t.Fatal("the panel opened with no templates")
	}

	rendered := page.renderGallery(80)
	for _, want := range []string{"landing-page", "deck-basic", "try:"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the panel never mentions %q", want)
		}
	}

	if _, _ = page.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}); page.galleryView {
		t.Error("g did not close the panel again")
	}
}

// The findings of the last critic pass travel with the version list, so the
// page can show them without a browser and without another query. What it must
// never do is imply a clean artifact when nobody has critiqued it.
func TestDesignPageFindingsPanel(t *testing.T) {
	page := &designPage{width: 100, height: 30}
	artifact := design.Artifact{ID: "dsg_1", Title: "Landing", Kind: design.KindWeb, CurrentVersion: 2}
	page.artifacts = []design.Artifact{artifact}

	page.galleryView = true
	if _, _ = page.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}); !page.issuesView {
		t.Fatal("c did not open the findings panel")
	}
	if page.galleryView {
		t.Error("the template panel must close: both own the same column")
	}

	page.versions = []design.Version{{Number: 2}, {Number: 1}}
	if got := page.renderIssues(80, artifact); !strings.Contains(got, "not been critiqued") {
		t.Errorf("an uncritiqued version reads as %q", got)
	}

	page.versions = []design.Version{
		{Number: 2, Critique: &design.Critique{Version: 2, Score: 6.5, Issues: []design.Issue{
			{Code: design.RuleContrast, Severity: design.SeverityError, NodeID: "n7",
				Message: "text contrast is 2.10:1 against its background"},
		}}},
		{Number: 1},
	}
	rendered := page.renderIssues(80, artifact)
	for _, want := range []string{"6.5/10", "error", "n7", "contrast"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the findings panel never mentions %q:\n%s", want, rendered)
		}
	}

	if _, _ = page.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}); page.issuesView {
		t.Error("c did not close the panel again")
	}
}
