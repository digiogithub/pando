package acp

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// personaAutoMock is an AgentService with no manual persona and a persona
// applied by auto-selection.
type personaAutoMock struct {
	*mockAgentService
	applied string
}

func (m *personaAutoMock) GetActivePersona() string { return "" }
func (m *personaAutoMock) AppliedAutoPersona(string) (string, string) {
	return m.applied, "decision"
}

func withPersonaAutoSelect(t *testing.T, enabled bool) {
	t.Helper()
	prev := config.Get()
	config.SetForTests(&config.Config{PersonaAutoSelect: config.PersonaAutoSelectConfig{Enabled: enabled}})
	t.Cleanup(func() { config.SetForTests(prev) })
}

func TestPersonaOptionAutoEntryShowsAppliedPersona(t *testing.T) {
	withPersonaAutoSelect(t, true)
	svc := &personaAutoMock{mockAgentService: &mockAgentService{}, applied: "software-engineer"}
	session := &ACPServerSession{}

	opt := buildPersonaConfigOption(svc, "", session)
	if opt == nil || opt.Select == nil {
		t.Fatal("no persona option")
	}
	if string(opt.Select.CurrentValue) != personaAutoValue {
		t.Fatalf("current = %q, want auto", opt.Select.CurrentValue)
	}
	var autoName string
	for _, g := range *opt.Select.Options.Ungrouped {
		if string(g.Value) == personaAutoValue {
			autoName = g.Name
		}
	}
	if autoName != "Auto (software-engineer)" {
		t.Fatalf("auto entry name = %q", autoName)
	}

	// Nothing applied yet: plain Auto.
	svc.applied = ""
	opt = buildPersonaConfigOption(svc, "", session)
	for _, g := range *opt.Select.Options.Ungrouped {
		if string(g.Value) == personaAutoValue && g.Name != "Auto" {
			t.Fatalf("auto entry name = %q, want Auto", g.Name)
		}
	}

	// An explicit session persona wins over Auto.
	opt = buildPersonaConfigOption(svc, "assistant", session)
	if string(opt.Select.CurrentValue) != "assistant" {
		t.Fatalf("explicit persona lost: %q", opt.Select.CurrentValue)
	}
}

func TestPersonaOptionHasNoAutoEntryWhenDisabled(t *testing.T) {
	withPersonaAutoSelect(t, false)
	svc := &personaAutoMock{mockAgentService: &mockAgentService{}, applied: "qa"}
	opt := buildPersonaConfigOption(svc, "", &ACPServerSession{})
	for _, g := range *opt.Select.Options.Ungrouped {
		if string(g.Value) == personaAutoValue {
			t.Fatal("auto entry must not exist when auto-select is off")
		}
	}
	if got := normalizePersonaValue("auto"); got != "auto" {
		t.Fatalf("normalize with auto-select off = %q", got)
	}
}

func TestNormalizePersonaValueMapsAutoToNoPersona(t *testing.T) {
	withPersonaAutoSelect(t, true)
	if got := normalizePersonaValue("auto"); got != "" {
		t.Fatalf("normalize(auto) = %q", got)
	}
	if got := normalizePersonaValue("qa"); got != "qa" {
		t.Fatalf("normalize(qa) = %q", got)
	}
	if !isPersonaNoticeText("Persona: qa (p=0.90, 12 ms)") || isPersonaNoticeText("Persona auto-select: decision model unavailable") {
		t.Fatal("persona notice detection wrong")
	}
}
