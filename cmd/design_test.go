package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// The design CLI is the surface a script talks to, so its shape is a contract:
// a renamed subcommand or a dropped --json flag breaks automation silently.
func TestDesignCommandSurface(t *testing.T) {
	expected := map[string]bool{
		"list": false, "create": false, "versions": false,
		"open": false, "export": false, "critique": false,
		"system": false, "skills": false,
	}
	for _, sub := range designCmd.Commands() {
		name := strings.Fields(sub.Use)[0]
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("pando design %s is missing", name)
		}
	}

	if designCmd.PersistentFlags().Lookup("json") == nil {
		t.Error("--json must be available to every design subcommand")
	}
	for _, name := range []string{"show", "init", "extract", "apply", "examples"} {
		if !hasSubcommand(designSystemCmd, name) {
			t.Errorf("pando design system %s is missing", name)
		}
	}
	for _, name := range []string{"show", "install"} {
		if !hasSubcommand(designSkillsCmd, name) {
			t.Errorf("pando design skills %s is missing", name)
		}
	}
	// A template picks the artifact kind, so --kind must stay empty by default:
	// forcing "web" would build a deck template as a page.
	if got := designCreateCmd.Flags().Lookup("kind").DefValue; got != "" {
		t.Errorf("--kind default = %q, want empty so the template decides", got)
	}
	if designCreateCmd.Flags().Lookup("skill") == nil {
		t.Error("pando design create needs --skill")
	}
	for _, flag := range []string{"version", "policy", "no-render", "no-record"} {
		if designCritiqueCmd.Flags().Lookup(flag) == nil {
			t.Errorf("pando design critique needs --%s", flag)
		}
	}
	for _, flag := range []string{"from", "target", "name", "dry-run"} {
		if designSystemExtractCmd.Flags().Lookup(flag) == nil {
			t.Errorf("pando design system extract needs --%s", flag)
		}
	}
}

// An extraction source that does not exist must be rejected by the service, not
// silently treated as a code scan: a typo in --from would otherwise scan the
// whole project and write a system nobody asked for.
func TestDesignSystemExtractRejectsUnknownSource(t *testing.T) {
	designExtractFrom = "vibes"
	t.Cleanup(func() { designExtractFrom = "code" })
	err := designSystemExtractCmd.RunE(designSystemExtractCmd, nil)
	if err == nil {
		t.Fatal("expected an unknown extraction source to fail")
	}
	if !strings.Contains(err.Error(), "vibes") {
		t.Errorf("error %q does not name the bad source", err)
	}
}

// Every design subcommand must be reachable from the root command; a command
// built but never registered is the failure mode this catches.
func TestDesignCommandIsRegistered(t *testing.T) {
	if !hasSubcommand(rootCmd, "design") {
		t.Fatal("pando design is not registered on the root command")
	}
}

func TestDesignExportRejectsUnknownFormat(t *testing.T) {
	designFormat = "docx"
	t.Cleanup(func() { designFormat = "html" })

	err := designExportCmd.RunE(designExportCmd, []string{"landing"})
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("expected an unsupported-format error before any database work, got %v", err)
	}
}

func TestDesignCreateRejectsUnknownKind(t *testing.T) {
	designKind = "mobile"
	t.Cleanup(func() { designKind = "web" })

	err := designCreateCmd.RunE(designCreateCmd, []string{"Phone app"})
	if err == nil || !strings.Contains(err.Error(), "unsupported kind") {
		t.Fatalf("expected an unsupported-kind error before any database work, got %v", err)
	}
}

// humanAge is what makes the list readable; the exact timestamp is behind
// --json. A zero time must not print as a date from 1970.
func TestHumanAgeHandlesZeroTime(t *testing.T) {
	if got := humanAge(time.Time{}); got != "-" {
		t.Fatalf("humanAge(zero) = %q, want %q", got, "-")
	}
}

func hasSubcommand(parent *cobra.Command, name string) bool {
	for _, sub := range parent.Commands() {
		if strings.Fields(sub.Use)[0] == name {
			return true
		}
	}
	return false
}

// The gallery is content shipped in the binary, so the CLI must list it without
// a project, a database or the design subsystem being enabled.
func TestDesignSkillsListsWithoutAProject(t *testing.T) {
	if err := designSkillsCmd.RunE(designSkillsCmd, nil); err != nil {
		t.Fatalf("pando design skills: %v", err)
	}
}

func TestDesignSkillsShowRejectsUnknownName(t *testing.T) {
	err := designSkillsShowCmd.RunE(designSkillsShowCmd, []string{"vibes"})
	if err == nil || !strings.Contains(err.Error(), "vibes") {
		t.Fatalf("expected an unknown-template error naming the input, got %v", err)
	}
}

// A policy typo must be caught before the command opens a database and renders
// a browser: running the wrong gate and reporting it as the right one is worse
// than refusing.
func TestDesignCritiqueRejectsUnknownPolicy(t *testing.T) {
	designCritiquePolicy = "brutal"
	t.Cleanup(func() { designCritiquePolicy = "" })

	err := designCritiqueCmd.RunE(designCritiqueCmd, []string{"landing"})
	if err == nil || !strings.Contains(err.Error(), "unknown critique policy") {
		t.Fatalf("expected an unknown-policy error before any database work, got %v", err)
	}
}
