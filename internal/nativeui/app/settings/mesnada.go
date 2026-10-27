package settings

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const (
	mnNesting  = "settings.mesnada.nesting."
	mnEngines  = "settings.mesnada.engines."
	mnDisabled = "orchestrator.disabledEngines"
)

// nativeEngines are Pando's own engines: always enabled, never disabled.
var nativeEngines = map[string]bool{"": true, "pando": true, "warm-acp": true, "acp-server": true}

func normalizeEngine(n string) string {
	n = strings.ToLower(strings.TrimSpace(n))
	switch n {
	case "claude-code":
		return "claude"
	case "gemini-cli":
		return "gemini"
	}
	return n
}

// setEngineDisabled returns the deduplicated, sorted list with name disabled
// or re-enabled (port of mesnadaEnginesStore.setEngineDisabled).
func setEngineDisabled(list []string, name string, disabled bool) []string {
	target := normalizeEngine(name)
	next := map[string]bool{}
	for _, e := range list {
		if e = normalizeEngine(e); !nativeEngines[e] {
			next[e] = true
		}
	}
	if disabled && !nativeEngines[target] {
		next[target] = true
	} else {
		delete(next, target)
	}
	out := make([]string, 0, len(next))
	for e := range next {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

type mesnadaSection struct {
	*Doc
	engines    []client.MesnadaEngine
	enginesErr string
}

func newMesnada(s *Settings) section {
	engineOpts := opts("pando", "claude", "copilot", "openai", "google", "ollama")
	hidden := func(*Doc) bool { return false }
	d := newDoc(s, client.PathConfigServices,
		Group{Fields: []Field{sw("enabled", "Enabled", "Enable Mesnada integration")}},
		Group{Title: "Server", Fields: []Field{
			{Path: "server.host", Label: "Host", Kind: KText, Placeholder: "localhost"},
			{Path: "server.port", Label: "Port", Kind: KInt, Width: 120},
		}},
		Group{Title: "Orchestrator", Fields: []Field{
			txt("orchestrator.storePath", "Store path", ""),
			txt("orchestrator.logDir", "Log directory", ""),
			{Path: "orchestrator.maxParallel", Label: "Max parallel", Desc: "1–20", Kind: KInt, Width: 120},
			{Path: "orchestrator.defaultEngine", Label: "Default engine", Kind: KSelect, Opts: engineOpts},
			{Path: "orchestrator.defaultModel", Label: "Default model", Kind: KText, Placeholder: "(empty = engine default)"},
			txt("orchestrator.personaPath", "Persona path", ""),
			{Path: mnDisabled, Label: "Disabled engines", Kind: KList, Visible: hidden},
		}},
		Group{Title: mnNesting + "title", Desc: mnNesting + "description", Fields: []Field{
			sw("delegation.allowNestedSubagents", mnNesting+"allowNested", mnNesting+"allowNestedDescription"),
			when(num("delegation.maxDepth", mnNesting+"maxDepth", mnNesting+"maxDepthHint"), on("delegation.allowNestedSubagents")),
		}},
		Group{Title: "ACP", Desc: "Agent Communication Protocol", Fields: []Field{
			sw("acp.enabled", "ACP enabled", ""),
			txt("acp.defaultAgent", "Default agent", ""),
			sw("acp.autoPermission", "Auto permission", "Automatically grant permissions to agents"),
		}},
		Group{Title: "ACP server", Fields: []Field{
			sw("acp.server.enabled", "Enabled", ""),
			txt("acp.server.host", "Host", ""),
			num("acp.server.port", "Port", ""),
			sw("acp.server.requireAuth", "Require auth", ""),
		}},
		Group{Title: "TUI", Fields: []Field{
			sw("tui.enabled", "TUI enabled", "Enable the Terminal User Interface"),
			sw("tui.webui", "Web UI enabled", "Enable the Web User Interface"),
		}},
	)
	d.Sub, d.PutWhole = "mesnada", true
	d.SavedMsg = "Services settings saved"
	m := &mesnadaSection{Doc: d}
	d.Loaded = func(d *Doc, root client.Obj) {
		// Unset means allowed (only an explicit false blocks nesting), and the
		// depth defaults to 3: show those, without making the page dirty.
		if v := d.vals["delegation.allowNestedSubagents"]; v != nil && !v.seen {
			v.b = true
			d.orig["delegation.allowNestedSubagents"] = "true"
		}
		if v := d.vals["delegation.maxDepth"]; v != nil {
			if n, _ := strconv.Atoi(v.s); n <= 0 {
				v.s = "3"
				d.orig["delegation.maxDepth"] = "3"
			}
		}
	}
	d.Tail = m.tail
	return m
}

func (m *mesnadaSection) Load() {
	m.Doc.Load()
	a := m.s.app
	a.Go(func(ctx context.Context) {
		eng, err := a.Client.MesnadaEngines(ctx)
		a.Update(func() {
			m.engines, m.enginesErr = eng, ""
			if err != nil {
				m.engines, m.enginesErr = nil, err.Error()
			}
		})
	})
}

func (m *mesnadaSection) Build(c *ui.Context) {
	m.s.pageHeader(c, m.s.app.T("settings.mesnada.hero.title"), m.s.app.T("settings.mesnada.hero.tagline"))
	m.Doc.Build(c)
}

// tail draws the engine toggles, edited through orchestrator.disabledEngines
// so they are saved with the rest of the page.
func (m *mesnadaSection) tail(c *ui.Context, d *Doc) {
	a, t := m.s.app, kit.Current()
	off := d.vals[mnDisabled]
	if off == nil {
		return
	}
	disabled := map[string]bool{}
	for _, e := range off.l {
		disabled[normalizeEngine(e)] = true
	}
	kit.SettingsSection(c.Key("engines"), a.T(mnEngines+"title"), a.T(mnEngines+"description"), func() {
		if m.enginesErr != "" {
			m.s.errorBanner(c, m.enginesErr)
		}
		for _, e := range m.engines {
			cc := c.Key("eng:" + e.Name)
			status := a.T(mnEngines + "notDetected")
			switch {
			case e.Locked:
				status = a.T(mnEngines + "builtIn")
			case e.Installed:
				status = a.T(mnEngines+"installedAt", "path", e.Path)
			case e.Binary != "":
				status = a.T(mnEngines+"notInstalled", "binary", e.Binary)
			}
			label := e.Label + " (" + e.Name + ")"
			if e.Kind == "custom" {
				label += " · " + a.T(mnEngines+"custom")
			}
			on := e.Locked || !disabled[normalizeEngine(e.Name)]
			kit.SettingsRow(cc, label, status, func() {
				if e.Locked {
					kit.Badge(cc, a.T(mnEngines+"builtIn"), kit.ToneAccent)
				}
				v := on
				if kit.Switch(cc, &v, "").Disabled(e.Locked).Changed() {
					off.l = setEngineDisabled(off.l, e.Name, !v)
				}
			})
		}
		if len(m.engines) == 0 && m.enginesErr == "" {
			ui.Text(c, "—").FontSize(t.TextSM).Padding(12, 16)
		}
	})
}
