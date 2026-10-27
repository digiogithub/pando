// Package settings is the settings area of the native desktop: the 23
// categories of the Web UI settings (web-ui/src/components/settings), their
// unsaved-changes guard and the "?section=" deep link. Plain config
// documents are data driven (form.go, specs.go); sections with their own UI
// (providers, MCP servers, LSP, skills, ...) live in their own files.
package settings

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/app/extensions"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func init() {
	app.Register(app.Area{
		ID: "settings", Path: "/settings", Patterns: []string{"/settings", "/settings/{section...}"},
		TitleKey: "nav.settings", Icon: kit.IconSettings, Section: app.SectionBottom,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

// categories in the order of SettingsView.tsx. path is what the extension
// UI policy hides.
func categories() []category {
	return []category{
		{id: "general", labelKey: "settings.categories.general", make: newGeneral},
		{id: "appearance", labelKey: "settings.categories.appearance", make: newAppearance},
		{id: "providers", labelKey: "settings.categories.providers", path: "providerAccounts", make: newProviders},
		{id: "agents", labelKey: "settings.categories.agents", path: "agents", make: newAgents},
		{id: "model-auto-mode", labelKey: "settings.categories.modelAutoMode", path: "modelAutoMode", make: newModelAutoMode},
		{id: "decision-model", labelKey: "settings.categories.decisionModel", path: "decisionModel", make: newDecisionModel},
		{id: "mcp-servers", labelKey: "settings.categories.mcpServers", path: "mcpServers", make: newMCPServers},
		{id: "mcp-gateway", labelKey: "settings.categories.mcpGateway", path: "mcpGateway", make: docSection("settings.categories.mcpGateway", "The gateway tracks tool usage to promote frequently-used MCP servers as favorites.", mcpGatewayDoc)},
		{id: "lsp", labelKey: "settings.categories.lsp", path: "lsp", make: newLSP},
		{id: "tools", labelKey: "settings.categories.tools", path: "internalTools", make: docSection("settings.categories.tools", "", toolsDoc)},
		{id: "container-runtime", labelKey: "settings.categories.containerRuntime", path: "container", make: newContainer},
		{id: "sandbox", labelKey: "settings.categories.sandbox", path: "sandbox", make: newSandbox},
		{id: "bash", labelKey: "settings.categories.bash", path: "bash", make: docSection("Bash Settings", "Banned commands are always blocked, even with user confirmation. Allowed commands run without requiring confirmation.", bashDoc)},
		{id: "token-optimization", labelKey: "settings.categories.tokenOptimization", path: "tokenOptimization", make: newTokenOpt},
		{id: "skills", labelKey: "settings.categories.skills", path: "skills", make: newSkills},
		{id: "design-system", labelKey: "settings.categories.designSystem", make: newDesignSystem},
		{id: "lua", labelKey: "settings.categories.lua", path: "lua", make: docSection("settings.categories.lua", "", luaDoc)},
		{id: "self-improvement", labelKey: "settings.categories.selfImprovement", path: "evaluator", make: docSection("settings.categories.selfImprovement", "", evaluatorDoc)},
		{id: "mesnada", labelKey: "settings.categories.mesnada", group: "services", path: "mesnada", make: newMesnada},
		{id: "remembrances", labelKey: "settings.categories.remembrances", group: "services", path: "remembrances", make: newRemembrances},
		{id: "snapshots", labelKey: "settings.categories.snapshots", group: "services", path: "snapshots", make: newSnapshots},
		{id: "api-server", labelKey: "settings.categories.apiServer", group: "services", path: "server", make: newAPIServer},
		{id: "webui-access", labelKey: "settings.categories.webuiAccess", group: "services", make: newWebUIAccess},
	}
}

// Settings is the area's state (UI thread only).
type Settings struct {
	app  *app.Context
	cats []category
	secs map[string]section

	active    string // category id, or "ext:<panel id>"
	lastParam string
	navScroll ui.ScrollState
	scroll    ui.ScrollState

	// Unsaved-changes dialog.
	dialog  bool
	pending func()
	saveErr bool

	// catalog is GET /api/v1/models for the model fields (modelpicker.go).
	catalog modelCatalog

	hidden []string // extension UI policy
	banner client.UIPolicyBanner
	exts   []client.ExtensionPanel
	policy bool
}

// New returns the settings view.
func New(a *app.Context) *Settings {
	return &Settings{app: a, cats: categories(), secs: map[string]section{}, active: "general"}
}

// Title is the header title: the open category.
func (s *Settings) Title() string {
	for _, c := range s.cats {
		if c.id == s.active {
			return s.app.T("nav.settings") + " / " + s.app.T(c.labelKey)
		}
	}
	return ""
}

func (s *Settings) section(id string) section {
	if sec, ok := s.secs[id]; ok {
		return sec
	}
	for _, c := range s.cats {
		if c.id == id {
			sec := c.make(s)
			s.secs[id] = sec
			return sec
		}
	}
	return nil
}

func (s *Settings) current() section {
	if strings.HasPrefix(s.active, "ext:") {
		return nil
	}
	return s.section(s.active)
}

// ConfirmLeave implements app.LeaveGuard: leaving the area with unsaved
// edits asks first.
func (s *Settings) ConfirmLeave(proceed func()) bool {
	if cur := s.current(); cur != nil && cur.Dirty() {
		s.askUnsaved(proceed)
		return false
	}
	return true
}

func (s *Settings) askUnsaved(then func()) {
	s.pending, s.dialog, s.saveErr = then, true, false
}

// visible reports whether a category is offered (extension UI policy).
func (s *Settings) visible(c category) bool {
	if c.path == "" {
		return true
	}
	for _, h := range s.hidden {
		if pathsOverlap(h, c.path) {
			return false
		}
	}
	return true
}

func pathsOverlap(a, b string) bool {
	l, r := strings.Split(strings.ToLower(strings.TrimSpace(a)), "."), strings.Split(strings.ToLower(strings.TrimSpace(b)), ".")
	if a == "" || b == "" {
		return false
	}
	n := min(len(l), len(r))
	for i := 0; i < n; i++ {
		if l[i] != r[i] {
			return false
		}
	}
	return true
}

// open switches category now (no guard).
func (s *Settings) open(id string) {
	s.active = id
	s.scroll.Y = 0
	if sec := s.current(); sec != nil {
		sec.Load()
	}
	if s.app.Router.Path() != "/settings/"+id && strings.HasPrefix(s.app.Router.Path(), "/settings") {
		s.lastParam = id
		s.app.Router.Replace("/settings/" + id)
	}
}

// Select switches to a category, asking first when edits are unsaved.
func (s *Settings) Select(id string) {
	if id == s.active {
		return
	}
	do := func() {
		if cur := s.current(); cur != nil {
			cur.Discard()
		}
		s.open(id)
	}
	if cur := s.current(); cur != nil && cur.Dirty() {
		s.askUnsaved(do)
		return
	}
	do()
}

func (s *Settings) loadPolicy() {
	s.policy = true
	a := s.app
	a.Go(func(ctx context.Context) {
		pol, _ := a.Client.UIPolicy(ctx)
		a.Update(func() {
			s.hidden, s.banner = pol.HiddenSections, pol.Banner
		})
	})
}

// Build draws the nav and the open category.
func (s *Settings) Build(c *ui.Context, r *ui.Route) {
	t := kit.Current()
	// Extension sections come from the manifest the extensions area loads
	// at start (empty on standard builds).
	s.exts = extensions.Panels(s.app, client.SlotSettings)
	if !s.policy {
		s.loadPolicy()
		s.open(s.active)
	}
	// Deep link: /settings/<id> or /settings?section=<id>.
	want := s.lastParam
	if r != nil {
		if want = r.Param("section"); want == "" {
			want = r.Query("section")
		}
	}
	if want != s.lastParam {
		s.lastParam = want
		if want != "" && want != s.active && (s.section(want) != nil || strings.HasPrefix(want, "ext:")) {
			s.Select(want)
		}
	}
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).MinHeight(0).Children(func() {
		s.nav(c, t)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			ui.Scroll(c).TrackScroll(&s.scroll).Grow(1).Padding(24, 32).Children(func() {
				ui.Column(c).MaxWidth(820).Gap(12).Children(func() { s.content(c) })
			})
		})
	})
	s.unsavedDialog(c)
	// Escape goes back to the chat, asking first about unsaved edits. Open
	// popups and dialogs take the key before this.
	if !s.dialog && c.Shortcut(0, ui.KeyEscape) {
		s.backToChat()
	}
}

// backToChat leaves settings for the chat (which reopens its session).
func (s *Settings) backToChat() {
	leave := func() {
		if cur := s.current(); cur != nil {
			cur.Discard()
		}
		s.app.Router.Push("/chat")
	}
	if s.ConfirmLeave(leave) {
		leave()
	}
}

func (s *Settings) nav(c *ui.Context, t *kit.Tokens) {
	ui.Scroll(c).TrackScroll(&s.navScroll).Width(220).Shrink(0).Background(t.BgShell).BorderWidth(0, 1, 0, 0).
		BorderColor(t.Border).Padding(12, 8).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			prev := ""
			for _, cat := range s.cats {
				if !s.visible(cat) {
					continue
				}
				if cat.group != prev && cat.group != "" {
					kit.SectionLabel(c, s.app.T("nav.sections.services"))
				}
				prev = cat.group
				if s.navButton(c, cat.id, s.app.T(cat.labelKey)) {
					s.Select(cat.id)
				}
			}
			if len(s.exts) > 0 {
				kit.SectionLabel(c, "Extensions") // no Web UI key
				for _, p := range s.exts {
					title := p.Title
					if title == "" {
						title = p.ID
					}
					if s.navButton(c, "ext:"+p.ID, title) {
						s.Select("ext:" + p.ID)
					}
				}
			}
		})
	})
}

func (s *Settings) navButton(c *ui.Context, id, label string) bool {
	t := kit.Current()
	active := id == s.active
	b := ui.ButtonBase(c.Key("cat:"+id)).Label(label).Height(32).Padding(0, 10).Radius(t.RadiusSM).
		AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer).FocusRing(true)
	switch {
	case active:
		b.Background(t.AccentSoft).TextColor(t.Accent)
	case b.Hovered():
		b.Background(t.BgHover).TextColor(t.Fg)
	default:
		b.TextColor(t.FgMuted)
	}
	b.Children(func() {
		txt := ui.Text(c, label).FontSize(t.TextSM).SingleLine().Ellipsis("…")
		if active {
			txt.FontWeight(600)
		}
	})
	return b.Clicked()
}

func (s *Settings) content(c *ui.Context) {
	t := kit.Current()
	if s.banner.Text != "" {
		ui.Row(c).Padding(10, 12).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).Children(func() {
			ui.Text(c, s.banner.Text+" "+s.banner.Link).FontSize(t.TextSM)
		})
	}
	if strings.HasPrefix(s.active, "ext:") {
		s.extensionPanel(c)
		return
	}
	sec := s.current()
	if sec == nil {
		return
	}
	sec.Build(c)
	if _, own := sec.(selfManaged); !own && sec.St().loaded {
		s.actionBar(c, sec)
	}
}

// extensionPanel draws an extension section: its ES-module panel in an
// embedded web view (extensions.Slot), which falls back to "Open in browser"
// where no web view is available.
func (s *Settings) extensionPanel(c *ui.Context) {
	for _, p := range s.exts {
		if "ext:"+p.ID != s.active {
			continue
		}
		s.pageHeader(c, p.Title, p.Extension)
		extensions.Slot(s.app, c, client.SlotSettings, p.ID, 640)
	}
}

func (s *Settings) unsavedDialog(c *ui.Context) {
	cur := s.current()
	label := ""
	for _, cat := range s.cats {
		if cat.id == s.active {
			label = s.app.T(cat.labelKey)
		}
	}
	kit.Dialog(c, &s.dialog, kit.DialogOpts{
		Title:       s.app.T("settings.unsaved.title"),
		Description: s.app.T("settings.unsaved.descriptionSections", "sections", label),
		Body: func() {
			if s.saveErr {
				ui.Text(c, s.app.T("settings.unsaved.saveFailed")).TextColor(kit.Current().Danger).FontSize(kit.Current().TextSM)
			}
		},
		Footer: func() {
			if kit.SecondaryButton(c.Key("u-cancel"), s.app.T("settings.unsaved.cancel")).Clicked() {
				s.dialog, s.pending = false, nil
			}
			if kit.Button(c.Key("u-discard"), kit.ButtonOpts{Label: s.app.T("settings.unsaved.discard"), Variant: kit.Danger}).Clicked() {
				s.dialog = false
				if cur != nil {
					cur.Discard()
				}
				s.runPending()
			}
			if kit.PrimaryButton(c.Key("u-save"), s.app.T("settings.unsaved.saveAndContinue")).Clicked() && cur != nil {
				cur.Save(func(ok bool) {
					if ok {
						s.dialog = false
						s.runPending()
					} else {
						s.saveErr = true
					}
				})
			}
		},
	})
	// Closing the dialog with Escape cancels.
	if !s.dialog && s.pending != nil && !s.saveErr {
		s.pending = nil
	}
}

func (s *Settings) runPending() {
	p := s.pending
	s.pending = nil
	if p != nil {
		p()
	}
}
