package page

import (
	"context"
	"testing"

	pandoapp "github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extensions"
	"github.com/digiogithub/pando/internal/tui/components/settings"
)

func policySections() []settings.Section {
	return []settings.Section{
		{Title: "General", Fields: []settings.Field{
			{Label: "Theme", Key: "tui.theme", Value: "pando-light", Type: settings.FieldText},
			{Label: "Nerd Fonts", Key: "tui.nerdFonts", Value: "true", Type: settings.FieldToggle},
		}},
		{Title: "Provider Accounts", Fields: []settings.Field{
			{Label: "Anthropic key", Key: "providerAccounts.anthropic.apiKey", Value: "sk-x", Type: settings.FieldText},
		}},
	}
}

// With no policy declared, the page must be exactly what it was before the
// capability existed: every section, every field, nothing marked.
func TestSettingsWithoutUIPolicy(t *testing.T) {
	config.ClearOverlayProviders()
	t.Cleanup(config.ClearOverlayProviders)
	extensions.SetUIPolicyResolver(nil)

	got := applyFieldPolicy(nil, policySections())
	if len(got) != 2 || len(got[0].Fields) != 2 || len(got[1].Fields) != 1 {
		t.Fatalf("sections changed with no policy: %+v", got)
	}
	for _, section := range got {
		for _, field := range section.Fields {
			if field.Locked || field.ManagedNote != "" || !field.Editable() {
				t.Fatalf("%q was marked with no policy declared: %+v", field.Label, field)
			}
		}
	}
	if managedBanner(nil, 40) != "" {
		t.Fatal("a banner was drawn with no policy declared")
	}
}

// A hidden section is dropped, a read-only one is marked with the caller's
// label and stops being editable, and a section left with no fields goes away.
func TestSettingsAppliesUIPolicy(t *testing.T) {
	config.ClearOverlayProviders()
	t.Cleanup(config.ClearOverlayProviders)
	t.Cleanup(func() { extensions.SetUIPolicyResolver(nil) })

	extensions.SetUIPolicyResolver(func(context.Context) extensions.UIPolicy {
		return extensions.UIPolicy{
			HiddenSections:   []string{"providerAccounts"},
			ReadOnlySections: []string{"tui.theme"},
			ReadOnlyLabel:    "Managed by the operator",
			Banner:           extensions.UIPolicyBanner{Text: "This device is managed", Link: "https://example.test"},
		}
	})
	app := &pandoapp.App{UIPolicy: extensions.CurrentUIPolicy}

	got := applyFieldPolicy(app, policySections())
	if len(got) != 1 || got[0].Title != "General" {
		t.Fatalf("hidden section still rendered: %+v", got)
	}

	theme, fonts := got[0].Fields[0], got[0].Fields[1]
	if !theme.Locked || theme.Editable() {
		t.Fatalf("a read-only key is still editable: %+v", theme)
	}
	if theme.ManagedNote != "Managed by the operator" || theme.LockMarker() != "[Managed by the operator]" {
		t.Fatalf("the caller's label is not rendered: note=%q marker=%q", theme.ManagedNote, theme.LockMarker())
	}
	if theme.Value != "pando-light" {
		t.Fatalf("a read-only field lost its value: %q", theme.Value)
	}
	if fonts.Locked || !fonts.Editable() {
		t.Fatalf("a field the policy says nothing about was marked: %+v", fonts)
	}

	// The same paths reach the write path, so the refusal does not depend on
	// the field having been drawn.
	if err := config.ErrIfLocked("providerAccounts.anthropic.apiKey"); err == nil {
		t.Fatal("a write to a hidden section was not refused")
	}
	if err := config.ErrIfLocked("tui.theme"); err == nil {
		t.Fatal("a write to a read-only section was not refused")
	}

	if banner := managedBanner(app, 60); banner == "" {
		t.Fatal("no banner was drawn for a policy that declares one")
	}
}
