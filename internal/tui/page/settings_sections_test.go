package page

import (
	"strings"
	"testing"

	pandoapp "github.com/digiogithub/pando/internal/app"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/tui/components/settings"
	"github.com/digiogithub/pando/internal/tui/styles"
)

func withSettingsSectionsConfig(t *testing.T) *config.Config {
	t.Helper()
	config.IsolateForTests(t)

	cfg, err := config.Load(t.TempDir(), false)
	if err != nil {
		t.Fatalf("config.Load(): %v", err)
	}

	cfg.TUI.Theme = "pando-dark"
	cfg.Server.Enabled = true
	cfg.Server.BasicAuth.Enabled = true
	cfg.Server.BasicAuth.Users = []config.BasicAuthUser{
		{Username: "alice"},
		{Username: "bob"},
	}

	return cfg
}

func sectionFieldByKey(t *testing.T, section settings.Section, key string) settings.Field {
	t.Helper()
	for _, field := range section.Fields {
		if field.Key == key {
			return field
		}
	}
	t.Fatalf("%q not found in section %q", key, section.Title)
	return settings.Field{}
}

func sectionByTitle(t *testing.T, sections []settings.Section, title string) settings.Section {
	t.Helper()
	for _, section := range sections {
		if section.Title == title {
			return section
		}
	}
	t.Fatalf("section %q not found", title)
	return settings.Section{}
}

func assertSectionMissingKey(t *testing.T, section settings.Section, key string) {
	t.Helper()
	for _, field := range section.Fields {
		if field.Key == key {
			t.Fatalf("%q unexpectedly present in section %q", key, section.Title)
		}
	}
}

func TestBuildSectionsMatchesWebUILayout(t *testing.T) {
	withSettingsSectionsConfig(t)

	got := buildSections(&pandoapp.App{})
	want := []struct {
		title string
		group string
	}{
		{title: "General"},
		{title: "Appearance"},
		{title: "Providers"},
		{title: "Agents"},
		{title: "Persona Auto-Select"},
		{title: "Auto mode"},
		{title: "Decision model"},
		{title: "MCP Servers"},
		{title: "MCP Gateway"},
		{title: "LSP"},
		{title: "Tools"},
		{title: "Container Runtime"},
		{title: "Sandbox"},
		{title: "Bash"},
		{title: "Token Optimization"},
		{title: "Skills"},
		{title: "Skills Catalog"},
		{title: "Lua Engine"},
		{title: "Self-Improvement"},
		{title: styles.MesnadaIcon + " Mesnada", group: "Services"},
		{title: styles.RemembrancesIcon + " Remembrances", group: "Services"},
		{title: "Snapshots", group: "Services"},
		{title: "API Server", group: "Services"},
		{title: "WebUI Access", group: "Services"},
		{title: "OpenLit Observability", group: "Services"},
	}

	if len(got) != len(want) {
		t.Fatalf("buildSections() returned %d sections, want %d", len(got), len(want))
	}

	for i := range want {
		if got[i].Title != want[i].title || got[i].Group != want[i].group {
			t.Fatalf("section %d = {title:%q group:%q}, want {title:%q group:%q}", i, got[i].Title, got[i].Group, want[i].title, want[i].group)
		}
	}
}

func TestAppearanceSectionOwnsThemeField(t *testing.T) {
	cfg := withSettingsSectionsConfig(t)

	appearance := buildAppearanceSection(cfg)
	if appearance.Title != "Appearance" {
		t.Fatalf("appearance title = %q", appearance.Title)
	}

	theme := sectionFieldByKey(t, appearance, "tui.theme")
	if theme.Value != "pando-dark" {
		t.Fatalf("theme value = %q, want %q", theme.Value, "pando-dark")
	}

	assertSectionMissingKey(t, buildGeneralSection(cfg), "tui.theme")
}

func TestWebUIAccessSectionOwnsBasicAuthFields(t *testing.T) {
	cfg := withSettingsSectionsConfig(t)

	webUIAccess := buildWebUIAccessSection(cfg)
	if webUIAccess.Title != "WebUI Access" {
		t.Fatalf("WebUI Access title = %q", webUIAccess.Title)
	}
	if got := sectionFieldByKey(t, webUIAccess, "server.basicAuth.enabled").Value; got != "true" {
		t.Fatalf("server.basicAuth.enabled = %q, want true", got)
	}
	users := sectionFieldByKey(t, webUIAccess, "server.basicAuth.users")
	if users.Value != "alice, bob" {
		t.Fatalf("server.basicAuth.users = %q, want %q", users.Value, "alice, bob")
	}
	if !users.ReadOnly {
		t.Fatal("server.basicAuth.users must stay read-only")
	}

	apiServer := buildServerSection(cfg)
	assertSectionMissingKey(t, apiServer, "server.basicAuth.enabled")
	assertSectionMissingKey(t, apiServer, "server.basicAuth.users")
}

func TestWebUIAccessSectionDisablesFieldsWhenServerIsOff(t *testing.T) {
	cfg := withSettingsSectionsConfig(t)
	cfg.Server.Enabled = false

	section := buildWebUIAccessSection(cfg)
	for _, key := range []string{"server.basicAuth.enabled", "server.basicAuth.users"} {
		if !sectionFieldByKey(t, section, key).Disabled {
			t.Fatalf("%s must be disabled when the API server is off", key)
		}
	}
	info := sectionFieldByKey(t, section, "server.webuiAccess.info.disabled")
	if info.Type != settings.FieldNote || info.Value != "API server is disabled." {
		t.Fatalf("disabled info row = %+v", info)
	}
}

func TestBuildSectionsUsesHeadersNotesAndCards(t *testing.T) {
	cfg := withSettingsSectionsConfig(t)
	cfg.MCPServers = map[string]config.MCPServer{
		"docs": {
			Type:    config.MCPStdio,
			Command: "docs-mcp",
			Args:    []string{"serve"},
		},
	}
	if err := config.AddProviderAccount(config.ProviderAccount{
		ID:          "anthropic-main",
		DisplayName: "Anthropic",
		Type:        models.ProviderAnthropic,
		APIKey:      "sk-test",
	}); err != nil {
		t.Fatalf("AddProviderAccount(): %v", err)
	}

	sections := buildSections(&pandoapp.App{})
	for _, section := range sections {
		hasFocusable := false
		for _, field := range section.Fields {
			if field.Type == settings.FieldHeader || field.Type == settings.FieldNote {
				if field.Focusable() {
					t.Fatalf("%s/%s should not be focusable", section.Title, field.Key)
				}
			}
			if strings.HasPrefix(field.Value, "──") {
				t.Fatalf("%s/%s still uses a separator value: %q", section.Title, field.Key, field.Value)
			}
			if field.Focusable() {
				hasFocusable = true
			}
		}
		if len(section.Fields) > 0 && !hasFocusable {
			t.Fatalf("section %q has no focusable fields", section.Title)
		}
	}

	for _, field := range sectionByTitle(t, sections, "Providers").Fields {
		if field.Type == settings.FieldHeader || field.Key == "action:add_provider" {
			continue
		}
		if field.Card == "" {
			t.Fatalf("providers field %q is missing a card", field.Key)
		}
		if field.CardID != "anthropic-main" {
			t.Fatalf("providers field %q card id = %q, want anthropic-main", field.Key, field.CardID)
		}
	}

	for _, field := range sectionByTitle(t, sections, "MCP Servers").Fields {
		if field.Type == settings.FieldHeader || field.Key == "action:add_mcp_server" {
			continue
		}
		if field.Card == "" {
			t.Fatalf("MCP server field %q is missing a card", field.Key)
		}
		if field.CardID != "docs" {
			t.Fatalf("MCP server field %q card id = %q, want docs", field.Key, field.CardID)
		}
	}

	for _, field := range sectionByTitle(t, sections, "Agents").Fields {
		if field.Type == settings.FieldHeader {
			continue
		}
		if field.Card == "" {
			t.Fatalf("agent field %q is missing a card", field.Key)
		}
		if field.CardID == "" {
			t.Fatalf("agent field %q is missing a card id", field.Key)
		}
	}
}

func TestConfiguredLSPFieldsCarryStableCardIDs(t *testing.T) {
	cfg := withSettingsSectionsConfig(t)
	cfg.LSP = map[string]config.LSPConfig{
		"go": {
			Command:   "gopls",
			Args:      []string{"serve"},
			Languages: []string{"go"},
			Autostart: true,
		},
	}

	section := buildLSPSection(&pandoapp.App{}, cfg)
	found := false
	for _, field := range section.Fields {
		if field.Card == "" {
			continue
		}
		found = true
		if field.CardID != "go" {
			t.Fatalf("LSP field %q card id = %q, want go", field.Key, field.CardID)
		}
	}
	if !found {
		t.Fatal("expected at least one configured LSP card")
	}
}
