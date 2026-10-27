package settings

import (
	"slices"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/i18n"
	"github.com/egoist/mygo/ui"
)

// A select keeps its stored value when the UI language changes: the labels
// change, the value must not (it was sent empty, and the server refused
// "telemetry_min_level").
func TestSelectSurvivesLanguageChange(t *testing.T) {
	h := newHarness(t)
	h.srv.docs["/api/v1/settings"] = `{"telemetry_available":true,"telemetry_enabled":true,"telemetry_min_level":"warn","debug":false}`
	prev := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(prev) })
	h.tt.Frame()
	sec := h.s.section("general").(*docPage)
	h.wait(func() bool { return sec.loaded })
	// The tester built the first frame before the document was replaced.
	sec.Load()
	h.wait(func() bool { return sec.raw["telemetry_min_level"] != nil })
	h.tt.Frame()
	for _, lang := range []string{"es", "en"} {
		i18n.SetLanguage(lang)
		h.tt.Frame()
		h.tt.Frame()
		if sec.Dirty() {
			t.Fatalf("language %s made the page dirty", lang)
		}
		if got := sec.Get("telemetry_min_level"); got != "warn" {
			t.Fatalf("language %s: min level = %q", lang, got)
		}
	}
	i18n.SetLanguage("es")
	h.tt.Frame()
	sec.vals["debug"].b = true
	ok := false
	sec.Save(func(b bool) { ok = b })
	h.wait(func() bool { return ok })
	if lvl := h.srv.put("/api/v1/settings")["telemetry_min_level"]; lvl != "warn" {
		t.Errorf("telemetry_min_level sent as %v", lvl)
	}
}

// The engine switches toggle (kit.Switch's Changed never reported, so a
// click did nothing); Pando's own engines show no switch.
func TestMesnadaEngineSwitchToggles(t *testing.T) {
	s, tt, q, _ := cmHarness(t)
	s.open("mesnada")
	m := s.section("mesnada").(*mesnadaSection)
	cmWait(t, tt, q, func() bool { return m.loaded && len(m.engines) == 2 })
	tt.SetSize(1100, 2600) // the engines card is below the fold
	tt.Frame()
	r, ok := tt.Find("Claude (claude)")
	if !ok {
		t.Fatalf("engine row missing: %q", tt.Texts())
	}
	// The switch sits at the right end of the row: click along it.
	has := func() bool { return slices.Contains(m.vals[mnDisabled].l, "claude") }
	for dx := float32(300); dx < 840 && !has(); dx += 8 {
		tt.ClickAt(r.X+dx, r.Y+r.H/2)
		tt.Frame()
	}
	got := m.vals[mnDisabled].l
	if !has() {
		t.Fatalf("disabled engines = %v", got)
	}
	if !m.Dirty() {
		t.Error("toggling an engine must make the page dirty")
	}
	if isPandoEngine("claude") || !isPandoEngine("") || !isPandoEngine("pando") {
		t.Error("isPandoEngine")
	}
}

// The default model is a model field only with the Pando engine.
func TestMesnadaDefaultModelPlainForExternalEngines(t *testing.T) {
	s, tt, q, _ := cmHarness(t)
	s.open("mesnada")
	m := s.section("mesnada").(*mesnadaSection)
	cmWait(t, tt, q, func() bool { return m.loaded })
	var f *Field
	m.fields(func(x *Field) {
		if x.Path == "orchestrator.defaultModel" {
			f = x
		}
	})
	m.Set("orchestrator.defaultEngine", "pando")
	if f.Plain(m.Doc) {
		t.Error("pando engine must use the model picker")
	}
	m.Set("orchestrator.defaultEngine", "claude")
	if !f.Plain(m.Doc) {
		t.Error("external engines take a plain model name")
	}
}

func TestModelComboSuggestsAndChooses(t *testing.T) {
	s, _, q, _ := cmHarness(t)
	value := ""
	tt := ui.NewTester(func(c *ui.Context) { s.modelCombo(c, &value, 300, "Model", nil) }, 800, 600)
	tt.Frame()
	cmWait(t, tt, q, func() bool { return s.catalog.loaded })
	if err := tt.Click("Model"); err != nil {
		t.Fatal(err)
	}
	tt.Type("m2")
	tt.Frame()
	if !tt.HasText("m2") {
		t.Fatalf("no suggestion: %q", tt.Texts())
	}
	for _, m := range s.catalog.models {
		if m.ID == autoModelID {
			t.Error("auto must not be offered")
		}
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if value != "m2" {
		t.Errorf("value = %q", value)
	}
}

func TestMatchSuggestionsProviderDotSyntax(t *testing.T) {
	items := []suggestion{{ID: "gpt-5", Group: "openai"}, {ID: "claude-x", Group: "anthropic"}, {ID: "x-gpt", Group: "copilot"}}
	if got := matchSuggestions(items, "gpt"); len(got) != 2 || got[0].ID != "gpt-5" {
		t.Errorf("gpt: %v", got)
	}
	if got := matchSuggestions(items, "copilot.gpt"); len(got) != 1 || got[0].ID != "x-gpt" {
		t.Errorf("copilot.gpt: %v", got)
	}
	if formatTokenLimit(200000) != "200K" || formatTokenLimit(1000000) != "1M" || formatTokenLimit(1500000) != "1.5M" {
		t.Error("formatTokenLimit")
	}
}

func TestRelativeTo(t *testing.T) {
	cases := map[string]string{"/w/p/.kb": "./.kb", "/w/shared/kb": "../shared/kb", "/w/p": ".", "rel/x": "rel/x"}
	for in, want := range cases {
		if got := relativeTo("/w/p", in); got != want {
			t.Errorf("relativeTo(%q) = %q, want %q", in, got, want)
		}
	}
}

// Escape leaves settings for the chat, asking first about unsaved edits.
func TestEscapeReturnsToChat(t *testing.T) {
	h := newHarness(t)
	h.s.app.Router.Push("/settings/general")
	h.tt.Frame()
	sec := h.s.section("general").(*docPage)
	h.wait(func() bool { return sec.loaded })
	sec.vals["debug"].b = true
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if !h.s.dialog {
		t.Fatal("unsaved edits must ask first")
	}
	if err := h.tt.Click("Discard changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if p := h.s.app.Router.Path(); p != "/chat" {
		t.Errorf("path = %s", p)
	}
	if sec.Dirty() {
		t.Error("edits must be discarded")
	}
}
