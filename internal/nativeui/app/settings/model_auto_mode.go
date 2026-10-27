package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const (
	amMaxRoutes      = 25
	amMaxDescription = 500
	amMaxFallbacks   = 2
	amNoModel        = "—"
)

var amStarterRoutes = []struct{ id, desc string }{
	{"quick_question", "A short factual or conceptual question that needs no code changes or tool use."},
	{"implementation", "Writing, modifying or refactoring code: implementing a feature, fixing a bug, editing files."},
	{"planning", "Designing an approach, architecture or multi-step plan before any code is written."},
	{"review", "Reviewing, auditing or explaining existing code, diffs or pull requests."},
}

// amRoute is the editable state of one route.
type amRoute struct {
	uid      int
	id, desc string
	model    string // selected label (amNoModel = none)
	fb       [amMaxFallbacks]string
	nfb      int // fallback fields shown
	disabled bool
}

func (r *amRoute) fallbacks() []string {
	out := []string{}
	for i := 0; i < r.nfb && i < amMaxFallbacks; i++ {
		if v := fromNone(r.fb[i]); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func fromNone(s string) string {
	if s == amNoModel {
		return ""
	}
	return s
}

// modelAutoMode is the "Auto mode" category.
type modelAutoMode struct {
	status
	s *Settings

	orig       client.ModelAutoMode
	enabled    bool
	defaultAut bool
	threshold  string
	minConf    string
	history    string
	routes     []*amRoute
	uid        int
	warnings   []string
	fieldErrs  []client.FieldError

	models  []string
	inUse   dmInUse
	delIdx  int
	delOpen bool

	prompt, pgHistory string
	pgRunning         bool
	pg                *client.PlaygroundResponse
	pgErr             string
}

func newModelAutoMode(s *Settings) section { return &modelAutoMode{s: s, delIdx: -1} }

func (m *modelAutoMode) Load() {
	if m.loading {
		return
	}
	m.loading = true
	a := m.s.app
	m.inUse.load(m.s)
	a.Go(func(ctx context.Context) {
		cfg, err := a.Client.GetModelAutoMode(ctx)
		ml, _ := a.Client.ListModels(ctx)
		a.Update(func() {
			m.loading = false
			m.models = m.models[:0]
			for _, mo := range ml.Models {
				if mo.ID != client.AutoModelID {
					m.models = append(m.models, mo.ID)
				}
			}
			sort.Strings(m.models)
			if err != nil {
				m.err, _ = client.ParseAPIError(err)
				return
			}
			m.err = ""
			m.seed(cfg)
		})
	})
}

func (m *modelAutoMode) seed(cfg client.ModelAutoMode) {
	if cfg.Threshold == 0 {
		cfg.Threshold = 0.6
	}
	m.orig = cfg
	m.enabled, m.defaultAut = cfg.Enabled, cfg.DefaultAuto
	m.threshold = strconv.FormatFloat(cfg.Threshold, 'f', -1, 64)
	m.minConf = strconv.FormatFloat(cfg.MinConfidence, 'f', -1, 64)
	m.history = strconv.Itoa(cfg.HistoryPrompts)
	m.routes = nil
	for _, r := range cfg.Routes {
		m.routes = append(m.routes, m.newRoute(r))
	}
	m.warnings, m.fieldErrs, m.loaded = cfg.Warnings, nil, true
}

func (m *modelAutoMode) newRoute(r client.ModelAutoRoute) *amRoute {
	m.uid++
	ar := &amRoute{uid: m.uid, id: r.ID, desc: r.Description, model: noneIfEmpty(r.Model), disabled: r.Disabled}
	for i := range ar.fb {
		ar.fb[i] = amNoModel
	}
	for i, f := range r.Fallbacks {
		if i < amMaxFallbacks {
			ar.fb[i] = f
			ar.nfb = i + 1
		}
	}
	return ar
}

func noneIfEmpty(s string) string {
	if s == "" {
		return amNoModel
	}
	return s
}

// draft converts the edits to the PUT body; num errors are reported.
func (m *modelAutoMode) draft() (client.ModelAutoMode, error) {
	th, err := strconv.ParseFloat(strings.TrimSpace(m.threshold), 64)
	if err != nil {
		return client.ModelAutoMode{}, fmt.Errorf("Threshold must be a number")
	}
	mc, err := strconv.ParseFloat(strings.TrimSpace(m.minConf), 64)
	if err != nil {
		return client.ModelAutoMode{}, fmt.Errorf("Minimum confidence must be a number")
	}
	h, err := strconv.Atoi(strings.TrimSpace(m.history))
	if err != nil {
		return client.ModelAutoMode{}, fmt.Errorf("History prompts must be a whole number")
	}
	out := client.ModelAutoMode{Enabled: m.enabled, DefaultAuto: m.defaultAut, Threshold: th, MinConfidence: mc, HistoryPrompts: h}
	for _, r := range m.routes {
		out.Routes = append(out.Routes, client.ModelAutoRoute{ID: strings.TrimSpace(r.id), Description: r.desc,
			Model: fromNone(r.model), Fallbacks: r.fallbacks(), Disabled: r.disabled})
	}
	return out, nil
}

func (m *modelAutoMode) Dirty() bool {
	if !m.loaded {
		return false
	}
	d, err := m.draft()
	if err != nil {
		return true
	}
	return jsonOf(cmpAM(d)) != jsonOf(cmpAM(m.orig))
}

func cmpAM(x client.ModelAutoMode) client.ModelAutoMode {
	x.Warnings, x.AutoSelected = nil, false
	if x.Routes == nil {
		x.Routes = []client.ModelAutoRoute{}
	}
	for i := range x.Routes {
		if x.Routes[i].Fallbacks == nil {
			x.Routes[i].Fallbacks = []string{}
		}
	}
	return x
}

func jsonOf(v any) string { b, _ := json.Marshal(v); return string(b) }

func (m *modelAutoMode) Discard() {
	if m.loaded {
		m.seed(m.orig)
	}
}

func (m *modelAutoMode) Save(done func(bool)) {
	if m.saving || !m.loaded {
		done(false)
		return
	}
	d, err := m.draft()
	if err != nil {
		m.err = err.Error()
		m.s.app.Toasts.Error(m.err, "")
		done(false)
		return
	}
	m.saving, m.err, m.fieldErrs = true, "", nil
	a := m.s.app
	a.Go(func(ctx context.Context) {
		r, err := a.Client.PutModelAutoMode(ctx, d)
		a.Update(func() {
			m.saving = false
			if err != nil {
				msg, fe := client.ParseAPIError(err)
				m.err, m.fieldErrs = msg, fe
				a.Toasts.Error(msg, "")
				done(false)
				return
			}
			m.seed(r)
			a.Toasts.Success("Auto mode settings saved", "")
			done(true)
		})
	})
}

func (m *modelAutoMode) errs(field string) []string {
	var out []string
	for _, e := range m.fieldErrs {
		if e.Field == "modelAutoMode."+field || e.Field == field {
			out = append(out, e.Message)
		}
	}
	return out
}

func (m *modelAutoMode) showErrs(c *ui.Context, field string) {
	t := kit.Current()
	for _, e := range m.errs(field) {
		ui.Text(c, e).FontSize(t.TextXS).TextColor(t.Danger)
	}
}

func (m *modelAutoMode) modelLabels(current ...string) []string {
	labels := []string{amNoModel}
	have := map[string]bool{amNoModel: true}
	for _, id := range m.models {
		labels = append(labels, id)
		have[id] = true
	}
	for _, id := range current {
		if !have[id] && id != "" {
			labels = append(labels, id)
			have[id] = true
		}
	}
	return labels
}

func (m *modelAutoMode) Build(c *ui.Context) {
	s, a, t := m.s, m.s.app, kit.Current()
	s.pageHeader(c, "Auto mode", "Let a small decision model pick the best configured model for each prompt. When routing is not possible, the turn falls back to the coder model.")
	if !m.loaded {
		s.loadingView(c, &m.status)
		return
	}
	if m.err != "" && len(m.fieldErrs) == 0 {
		s.errorBanner(c, m.err)
	}
	for _, w := range m.warnings {
		ui.Text(c, "⚠ "+w).FontSize(t.TextSM).TextColor(t.Warning)
	}
	kit.SettingsSection(c.Key("gen"), "General", "", func() {
		kit.SettingsRow(c.Key("en"), "Enable Auto mode", "Adds “Auto” as the first entry of every model selector.", func() { kit.Switch(c, &m.enabled, "") })
		kit.SettingsRow(c.Key("def"), "Use Auto by default", "New sessions start with Auto selected.", func() { kit.Switch(c, &m.defaultAut, "") })
	})
	kit.SettingsSection(c.Key("dm"), "Decision model", "Auto mode asks the shared decision model which route fits each prompt.", func() {
		ui.Column(c).Padding(12, 16).Children(func() {
			m.inUse.build(c, s, m.enabled, "Until one is configured, prompts use the coder model.")
			m.showErrs(c, "router.model")
		})
	})
	kit.SettingsSection(c.Key("tune"), "Routing tuning", "", func() {
		kit.SettingsRow(c.Key("th"), "Threshold", "Minimum probability of the chosen route (0.05–1, default 0.60).", func() {
			x, _ := strconv.ParseFloat(strings.TrimSpace(m.threshold), 64)
			if x < 0.05 {
				x = 0.05
			}
			if x > 1 {
				x = 1
			}
			if ui.Slider(c, &x, 0.05, 1).Width(140).Changed() {
				m.threshold = strconv.FormatFloat(float64(int(x*20+0.5))/20, 'f', -1, 64)
			}
			kit.Input(c.Key("thin"), &m.threshold, "0.6").Width(80)
		})
		m.showErrs(c, "threshold")
		kit.SettingsRow(c.Key("mc"), "Minimum confidence", "0 disables the extra confidence check.", func() { kit.Input(c, &m.minConf, "0").Width(100) })
		m.showErrs(c, "minConfidence")
		kit.SettingsRow(c.Key("hp"), "History prompts", "Previous user prompts included as context for the decision.", func() { kit.Input(c, &m.history, "0").Width(100) })
		m.showErrs(c, "historyPrompts")
	})
	m.routesSection(c)
	m.playground(c)
	m.deleteDialog(c)
	_ = a
}

func (m *modelAutoMode) routesSection(c *ui.Context) {
	t := kit.Current()
	title := fmt.Sprintf("Routes (%d/%d)", len(m.routes), amMaxRoutes)
	kit.SettingsSection(c.Key("routes"), title, "Each route describes a kind of prompt and the model that should handle it. Order matters only for ties.", func() {
		m.showErrs(c, "routes")
		if len(m.routes) == 0 {
			ui.Text(c, "No routes yet. Add your own or start from the suggested ones.").FontSize(t.TextSM).TextColor(t.FgMuted).Padding(12, 16)
		}
		for i := range m.routes {
			m.routeCard(c.Key(fmt.Sprintf("r%d", m.routes[i].uid)), i)
		}
		ui.Row(c).Gap(8).Padding(12, 16).Children(func() {
			full := len(m.routes) >= amMaxRoutes
			if kit.Button(c.Key("add"), kit.ButtonOpts{Label: "Add route", Icon: kit.IconPlus, Variant: kit.Secondary, Disabled: full}).Clicked() {
				m.routes = append(m.routes, m.newRoute(client.ModelAutoRoute{}))
			}
			if kit.Button(c.Key("starters"), kit.ButtonOpts{Label: "Add starter routes", Variant: kit.Secondary, Disabled: full}).Clicked() {
				have := map[string]bool{}
				for _, r := range m.routes {
					have[r.id] = true
				}
				for _, st := range amStarterRoutes {
					if !have[st.id] && len(m.routes) < amMaxRoutes {
						m.routes = append(m.routes, m.newRoute(client.ModelAutoRoute{ID: st.id, Description: st.desc}))
					}
				}
			}
		})
	})
}

func (m *modelAutoMode) routeCard(c *ui.Context, i int) {
	t := kit.Current()
	r := m.routes[i]
	ui.Column(c).Padding(12, 16).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			kit.Input(c.Key("id"), &r.id, "route_id").Width(200)
			kit.Switch(c.Key("dis"), &r.disabled, "Disabled")
			ui.Box(c).Grow(1)
			if kit.Button(c.Key("up"), kit.ButtonOpts{Icon: kit.IconArrowUp, Tooltip: "Move up", Variant: kit.Secondary, Disabled: i == 0}).Clicked() {
				m.routes[i], m.routes[i-1] = m.routes[i-1], m.routes[i]
			}
			if kit.Button(c.Key("down"), kit.ButtonOpts{Icon: kit.IconArrowDown, Tooltip: "Move down", Variant: kit.Secondary, Disabled: i == len(m.routes)-1}).Clicked() {
				m.routes[i], m.routes[i+1] = m.routes[i+1], m.routes[i]
			}
			if kit.Button(c.Key("del"), kit.ButtonOpts{Icon: kit.IconTrash, Tooltip: "Delete route", Variant: kit.Secondary}).Clicked() {
				m.delIdx, m.delOpen = i, true
			}
		})
		m.showErrs(c, fmt.Sprintf("routes[%d].id", i))
		ui.TextArea(c.Key("desc"), &r.desc).Placeholder("Describe when this route should be used").Lines(2, 4)
		col := t.FgMuted
		if len([]rune(r.desc)) > amMaxDescription {
			col = t.Danger
		}
		ui.Text(c, fmt.Sprintf("%d/%d", len([]rune(r.desc)), amMaxDescription)).FontSize(t.TextXS).TextColor(col)
		m.showErrs(c, fmt.Sprintf("routes[%d].description", i))

		labels := m.modelLabels(r.model, r.fb[0], r.fb[1])
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Primary model").FontSize(t.TextSM).Width(110)
			kit.Select(c.Key("model"), &r.model, labels).Width(300)
		})
		m.showErrs(c, fmt.Sprintf("routes[%d].model", i))
		for slot := 0; slot < r.nfb; slot++ {
			cc := c.Key(fmt.Sprintf("fb%d", slot))
			ui.Row(cc).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(cc, fmt.Sprintf("Fallback %d", slot+1)).FontSize(t.TextSM).Width(110)
				kit.Select(cc.Key("s"), &r.fb[slot], labels).Width(300)
				if kit.IconButton(cc.Key("x"), kit.IconClose, "Remove fallback").Clicked() {
					// Shift the later fallback down.
					for k := slot; k < amMaxFallbacks-1; k++ {
						r.fb[k] = r.fb[k+1]
					}
					r.fb[amMaxFallbacks-1] = amNoModel
					r.nfb--
				}
			})
		}
		if r.nfb < amMaxFallbacks {
			if kit.Button(c.Key("addfb"), kit.ButtonOpts{Label: "Add fallback", Icon: kit.IconPlus, Variant: kit.Secondary, Size: kit.SizeSM}).Clicked() {
				r.nfb++
			}
		}
		m.showErrs(c, fmt.Sprintf("routes[%d].fallbacks", i))
	})
}

func (m *modelAutoMode) deleteDialog(c *ui.Context) {
	name := ""
	if m.delIdx >= 0 && m.delIdx < len(m.routes) {
		name = m.routes[m.delIdx].id
		if name == "" {
			name = strconv.Itoa(m.delIdx + 1)
		}
	}
	kit.Dialog(c, &m.delOpen, kit.DialogOpts{
		Title: "Delete route", Description: fmt.Sprintf("Delete route %q?", name),
		Footer: func() {
			if kit.SecondaryButton(c.Key("del-cancel"), "Cancel").Clicked() {
				m.delOpen = false
			}
			if kit.Button(c.Key("del-ok"), kit.ButtonOpts{Label: "Delete", Variant: kit.Danger}).Clicked() {
				if m.delIdx >= 0 && m.delIdx < len(m.routes) {
					m.routes = append(m.routes[:m.delIdx:m.delIdx], m.routes[m.delIdx+1:]...)
				}
				m.delOpen = false
			}
		},
	})
}

func (m *modelAutoMode) runPlayground() {
	d, err := m.draft()
	if err != nil {
		m.pgErr = err.Error()
		return
	}
	var hist []string
	for _, l := range strings.Split(m.pgHistory, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			hist = append(hist, l)
		}
	}
	m.pgRunning, m.pgErr = true, ""
	a, prompt := m.s.app, m.prompt
	a.Go(func(ctx context.Context) {
		r, err := a.Client.ModelAutoPlayground(ctx, prompt, hist, d)
		a.Update(func() {
			m.pgRunning = false
			if err != nil {
				m.pg = nil
				m.pgErr, _ = client.ParseAPIError(err)
				return
			}
			m.pg = &r
		})
	})
}

func (m *modelAutoMode) playground(c *ui.Context) {
	t := kit.Current()
	kit.SettingsSection(c.Key("pg"), "Playground", "Try a prompt against the current (unsaved) routes and the saved decision model. Nothing is sent to a chat model.", func() {
		ui.Column(c).Padding(12, 16).Gap(8).Children(func() {
			ui.TextArea(c.Key("pgp"), &m.prompt).Placeholder("Type a prompt to route…").Lines(3, 6)
			ui.TextArea(c.Key("pgh"), &m.pgHistory).Placeholder("Optional previous prompts, one per line").Lines(2, 5)
			ui.Row(c).Children(func() {
				if kit.Button(c.Key("route"), kit.ButtonOpts{Label: "Route", Icon: kit.IconSend, Variant: kit.Secondary, Disabled: strings.TrimSpace(m.prompt) == "" || m.pgRunning}).Clicked() {
					m.runPlayground()
				}
			})
			if m.pgErr != "" {
				ui.Text(c, m.pgErr).FontSize(t.TextXS).TextColor(t.Danger)
			}
			pg := m.pg
			if pg == nil {
				return
			}
			d := pg.Decision
			route, tone := "no match", kit.ToneWarning
			if d.Matched {
				route, tone = d.RouteID, kit.ToneSuccess
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Route: "+route).FontWeight(600).FontSize(t.TextSM)
				kit.Badge(c, d.Reason, tone)
			})
			if d.Error != "" {
				ui.Text(c, d.Error).FontSize(t.TextSM).TextColor(t.Danger)
			}
			keys := make([]string, 0, len(d.Probabilities))
			for k := range d.Probabilities {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool { return d.Probabilities[keys[i]] > d.Probabilities[keys[j]] })
			for _, k := range keys {
				p := d.Probabilities[k]
				cc := c.Key("p:" + k)
				ui.Row(cc).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(cc, k).FontSize(t.TextSM).Width(140).SingleLine().Ellipsis("…")
					ui.Box(cc).Width(200).Height(8).Radius(4).Background(t.BgInput).Children(func() {
						ui.Box(cc).Width(float32(200 * p)).Height(8).Radius(4).Background(t.Accent)
					})
					ui.Text(cc, fmt.Sprintf("%.0f%%", p*100)).FontSize(t.TextXS)
				})
			}
			ui.Text(c, fmt.Sprintf("Candidates: %s · Usable: %s", orDash(d.Candidates), orDash(pg.UsableCandidates))).FontSize(t.TextXS).TextColor(t.FgMuted)
			if len(pg.Skipped) > 0 {
				var parts []string
				for k, v := range pg.Skipped {
					parts = append(parts, k+" ("+v+")")
				}
				sort.Strings(parts)
				ui.Text(c, "Skipped: "+strings.Join(parts, ", ")).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
			foot := fmt.Sprintf("%d ms", d.LatencyMs)
			if d.CostUSD != nil {
				foot += fmt.Sprintf(" · $%.6f", *d.CostUSD)
			}
			if d.RouterModel != "" {
				foot += " · " + d.RouterProvider + " " + d.RouterModel
			}
			ui.Text(c, foot).FontSize(t.TextXS).TextColor(t.FgMuted)
		})
	})
}

func orDash(l []string) string {
	if len(l) == 0 {
		return "—"
	}
	return strings.Join(l, ", ")
}
