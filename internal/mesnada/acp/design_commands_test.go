package acp

import (
	"strings"
	"testing"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// The design commands must be advertised, not just handled: an ACP client
// builds its palette from available_commands, so a command missing there is
// invisible even though typing it works.
func TestDesignCommandsAreAdvertised(t *testing.T) {
	advertised := map[string]acpsdk.AvailableCommand{}
	for _, cmd := range availableCommands() {
		advertised[cmd.Name] = cmd
	}
	for _, name := range []string{"design", "design-open", "design-versions"} {
		cmd, ok := advertised[name]
		if !ok {
			t.Fatalf("/%s is not advertised to clients", name)
		}
		if strings.TrimSpace(cmd.Description) == "" {
			t.Fatalf("/%s has no description", name)
		}
		if cmd.Input == nil || cmd.Input.Unstructured == nil {
			t.Fatalf("/%s takes an artifact argument and should hint at it", name)
		}
	}
}

func TestParseDesignSlashCommands(t *testing.T) {
	cases := []struct {
		input     string
		kind      slashCommandKind
		objective string
	}{
		{"/design", slashCommandDesign, ""},
		{"/design landing", slashCommandDesign, "landing"},
		{"/design-open", slashCommandDesignOpen, ""},
		{"/design-open quarterly 3", slashCommandDesignOpen, "quarterly 3"},
		{"/design-versions dsg_abc", slashCommandDesignVersions, "dsg_abc"},
	}
	for _, tc := range cases {
		command, ok := parseSlashCommand(tc.input)
		if !ok {
			t.Fatalf("%q was not recognised as a slash command", tc.input)
		}
		if command.Kind != tc.kind {
			t.Fatalf("%q parsed as %q, want %q", tc.input, command.Kind, tc.kind)
		}
		if command.Objective != tc.objective {
			t.Fatalf("%q carried objective %q, want %q", tc.input, command.Objective, tc.objective)
		}
	}
}

// /design must not swallow the longer command names: the specs are scanned in
// order and a prefix match would make /design-open unreachable.
func TestDesignCommandDoesNotShadowItsSiblings(t *testing.T) {
	command, ok := parseSlashCommand("/design-open landing")
	if !ok || command.Kind != slashCommandDesignOpen {
		t.Fatalf("/design-open was shadowed: %+v (ok=%v)", command, ok)
	}
}

func TestSplitDesignSlide(t *testing.T) {
	cases := []struct {
		args  string
		ref   string
		slide int
	}{
		{"", "", 0},
		{"landing", "landing", 0},
		{"quarterly 3", "quarterly", 3},
		{"quarterly #3", "quarterly", 3},
		{"3", "", 3},
		{"quarterly review 12", "quarterly review", 12},
		// A slug that ends in a digit is a name, not a slide, only when it is
		// not a bare number; "v2" stays part of the reference.
		{"deck-v2", "deck-v2", 0},
		// Zero and negatives are not slide numbers, so they stay in the ref.
		{"landing 0", "landing 0", 0},
	}
	for _, tc := range cases {
		ref, slide := splitDesignSlide(tc.args)
		if ref != tc.ref || slide != tc.slide {
			t.Errorf("splitDesignSlide(%q) = (%q, %d), want (%q, %d)", tc.args, ref, slide, tc.ref, tc.slide)
		}
	}
}

// ACP's ToolKind is a closed protocol enum: there is no "design" member, and a
// client given an unknown value shows a generic icon at best. Each design tool
// must therefore land on the kind that matches what it does to the workspace.
func TestDesignToolsMapToProtocolKinds(t *testing.T) {
	writes := []string{"design_create", "design_patch", "design_export", "design_canvas", "design_system"}
	reads := []string{"design_render", "design_screenshot", "design_inspect", "design_versions"}

	for _, name := range writes {
		if got := mapToolKind(name); got != acpsdk.ToolKindEdit {
			t.Errorf("%s mapped to %q, want %q", name, got, acpsdk.ToolKindEdit)
		}
	}
	for _, name := range reads {
		if got := mapToolKind(name); got != acpsdk.ToolKindRead {
			t.Errorf("%s mapped to %q, want %q", name, got, acpsdk.ToolKindRead)
		}
	}
	if got := mapToolKind("design_present"); got != acpsdk.ToolKindFetch {
		t.Errorf("design_present mapped to %q, want %q", got, acpsdk.ToolKindFetch)
	}
	if got := mapToolKind("design_not_a_tool"); got != acpsdk.ToolKindOther {
		t.Errorf("an unknown design_-prefixed name should stay Other, got %q", got)
	}
}

// A tool list showing "design_patch" eight times tells the user nothing about
// which artifact is being changed.
func TestDesignToolTitlesNameTheArtifact(t *testing.T) {
	input := map[string]interface{}{"artifact_id": "dsg_abc", "format": "pdf"}

	if got := toolDisplayTitle("design_patch", input, ""); got != "Edit design dsg_abc" {
		t.Errorf("design_patch title = %q", got)
	}
	if got := toolDisplayTitle("design_export", input, ""); got != "Export PDF of dsg_abc" {
		t.Errorf("design_export title = %q", got)
	}
	if got := toolDisplayTitle("design_create", map[string]interface{}{"title": "Landing"}, ""); got != "Design Landing" {
		t.Errorf("design_create title = %q", got)
	}
	// With nothing to name, the verb alone still beats the raw tool name.
	if got := toolDisplayTitle("design_render", map[string]interface{}{}, ""); got != "Render design" {
		t.Errorf("design_render title = %q", got)
	}
}
