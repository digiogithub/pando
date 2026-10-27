// Package overlays holds the window-level layers of the native desktop that
// the Web UI draws over its layout: the quick menu (command palette), the
// model switcher, the config-init banner and the update notice. Each
// registers itself with the app registries from init().
package overlays

import (
	"context"
	"slices"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func init() {
	app.RegisterOverlay(func(a *app.Context) app.Overlay { return newQuickMenu(a) })
	app.RegisterOverlay(func(a *app.Context) app.Overlay { return newModelSwitcher(a) })
	app.RegisterBanner(10, func(a *app.Context) app.Banner { return newConfigInit(a) })
	app.RegisterBanner(20, func(a *app.Context) app.Banner { return newUpdateNotice(a) })

	// Commands of the palette (the web's "Commands" group).
	app.RegisterCommand(app.Command{ID: "toggle-theme", Title: "Toggle Dark Mode", TitleKey: "header.toggleTheme", Icon: kit.IconMoon,
		Keywords: []string{"theme", "dark", "light", "appearance"}, Run: func(a *app.Context) { a.CycleThemeMode() }})
	app.RegisterCommand(app.Command{ID: "switch-model", Title: "Switch Model", Icon: kit.IconBot,
		Keywords: []string{"model", "llm"}, Description: "Ctrl+O", Run: func(a *app.Context) { a.OpenModelSwitcher() }})
	app.RegisterCommand(app.Command{ID: "toggle-sidebar", Title: "Toggle Sidebar", TitleKey: "header.toggleSidebar", Icon: kit.IconPanelLeft, Run: func(a *app.Context) { a.SetSidebarCollapsed(!a.Shell().SidebarCollapsed) }})
	app.RegisterCommand(app.Command{ID: "simple-chat", Title: "Toggle Simple Chat", TitleKey: "header.simpleChat", Icon: kit.IconSimpleChat,
		Keywords: []string{"chat", "simple"}, Run: func(a *app.Context) { a.ToggleSimpleChat() }})
	app.RegisterCommand(app.Command{ID: "docs", Title: "Open Documentation", TitleKey: "shell.documentation", Icon: kit.IconBookOpen,
		Keywords: []string{"help", "docs"}, Run: func(*app.Context) { app.OpenExternal(app.DocsURL) }})
	app.RegisterCommand(app.Command{ID: "open-browser", Title: "Open in Browser", Icon: kit.IconGlobe,
		Keywords: []string{"web", "browser", "webui"}, Run: func(a *app.Context) { app.OpenExternal(a.Client.BaseURL()) }})
	app.RegisterCommand(app.Command{ID: "quit", Title: "Quit Pando", Icon: kit.IconClose,
		Keywords: []string{"exit", "close"}, Run: func(a *app.Context) {
			if h := a.Shell().Host; h != nil {
				h.Quit()
			}
		}})
}

// quickMenu is the command palette (Ctrl/Cmd+K, Ctrl/Cmd+P): views, commands,
// recently used entries and the account commands of the providers.
type quickMenu struct {
	a        *app.Context
	pick     picker
	wasOpen  bool
	recent   []string
	accounts []app.Command
}

func newQuickMenu(a *app.Context) *quickMenu { return &quickMenu{a: a} }

// Build implements app.Overlay.
func (q *quickMenu) Build(c *ui.Context) {
	sh := q.a.Shell()
	if !sh.QuickMenuOpen {
		q.wasOpen = false
		return
	}
	if !q.wasOpen {
		q.wasOpen = true
		q.pick.reset()
		q.loadAccounts()
	}
	rows := q.rows()
	kit.Dialog(c, &sh.QuickMenuOpen, kit.DialogOpts{
		Width: 600,
		Body: func() {
			q.pick.build(c, "Search views and commands…", "No results", rows, 340, func(r pickRow) {
				sh.QuickMenuOpen = false
				q.remember(r.ID)
				r.Run()
			})
			t := kit.Current()
			ui.Text(c, "Ctrl+Alt+1…9 "+q.a.T("projects.tabs.shortcutFocusTabs")+"  ·  Ctrl+Alt+W "+
				q.a.T("projects.tabs.shortcutCloseTab")+"  ·  Ctrl+Alt+←/→ "+q.a.T("projects.tabs.shortcutCycleTabs")).
				FontSize(t.TextXS).TextColor(t.FgFaint)
		},
	})
}

func (q *quickMenu) remember(id string) {
	q.recent = slices.DeleteFunc(q.recent, func(s string) bool { return s == id })
	q.recent = append([]string{id}, q.recent...)
	if len(q.recent) > 5 {
		q.recent = q.recent[:5]
	}
}

// rows builds the palette: recently used (when not searching), views,
// commands, accounts.
func (q *quickMenu) rows() []pickRow {
	a := q.a
	var views, cmds, accts []pickRow
	for _, ar := range app.Areas() {
		if ar.Section == app.SectionHidden || ar.Path == "" {
			continue
		}
		ar := ar
		views = append(views, pickRow{ID: "view:" + ar.ID, Group: "Views", Icon: ar.Icon, Title: a.T(ar.TitleKey), Desc: ar.Path,
			Run: func() { a.Navigate(ar.Path) }})
	}
	for _, cm := range app.Commands() {
		cm := cm
		title := cm.Title
		if cm.TitleKey != "" {
			if s := a.T(cm.TitleKey); s != cm.TitleKey {
				title = s
			}
		}
		row := pickRow{ID: "cmd:" + cm.ID, Icon: cm.Icon, Title: title, Desc: cm.Description,
			Search: strings.Join(slices.Concat(cm.Keywords, []string{cm.Title}), " "), Run: func() { cm.Run(a) }}
		row.Group = "Commands"
		cmds = append(cmds, row)
	}
	for _, cm := range q.accounts {
		cm := cm
		accts = append(accts, pickRow{ID: "acct:" + cm.ID, Group: "Accounts", Icon: cm.Icon, Title: cm.Title, Desc: cm.Description,
			Search: strings.Join(cm.Keywords, " "), Run: func() { cm.Run(a) }})
	}
	all := slices.Concat(views, cmds, accts)
	if strings.TrimSpace(q.pick.query) != "" || len(q.recent) == 0 {
		return all
	}
	var recent []pickRow
	for _, id := range q.recent {
		for _, r := range all {
			if r.ID == id {
				r.Group = "Recently used"
				recent = append(recent, r)
			}
		}
	}
	return slices.Concat(recent, all)
}

// loadAccounts fetches the provider account commands (Copilot status, login,
// logout) like the web's commandLauncher.
func (q *quickMenu) loadAccounts() {
	a := q.a
	a.Go(func(ctx context.Context) {
		types, err := a.Client.AuthProviderTypes(ctx)
		if err != nil {
			if client.IsAuthError(err) {
				a.ReportError("Could not load launcher commands", err)
			}
			return
		}
		var cmds []app.Command
		for _, p := range types {
			if p.Type != "copilot" {
				continue
			}
			name := p.DisplayName
			kw := []string{p.Type, strings.ToLower(name), "github", "oauth"}
			cmds = append(cmds,
				app.Command{ID: p.Type + ":status", Title: name + " Status", Description: "Show " + name + " authentication status",
					Icon: kit.IconInfo, Keywords: append(slices.Clone(kw), "status"), Run: func(a *app.Context) { a.Go(func(ctx context.Context) { providerStatus(ctx, a, p.Type) }) }},
				app.Command{ID: p.Type + ":logout", Title: name + " Logout", Description: "Remove saved " + name + " credentials",
					Icon: kit.IconLogOut, Keywords: append(slices.Clone(kw), "logout", "sign out"), Run: func(a *app.Context) { a.Go(func(ctx context.Context) { copilotLogout(ctx, a) }) }},
				app.Command{ID: "copilot:login", Title: "Copilot Login", Description: "Start GitHub Copilot device login flow",
					Icon: kit.IconKey, Keywords: append(slices.Clone(kw), "login", "device code", "sign in"), Run: func(a *app.Context) { a.Go(func(ctx context.Context) { copilotLogin(ctx, a) }) }},
			)
		}
		a.Update(func() { q.accounts = cmds })
	})
}

func providerStatus(ctx context.Context, a *app.Context, provider string) {
	s, err := a.Client.ProviderAuthStatus(ctx, provider)
	if err != nil {
		a.ReportError("Provider status failed", err)
		return
	}
	msg := s.Message
	if bits := strings.TrimSpace(strings.Join([]string{s.EnterpriseURL, s.Source}, " · ")); bits != "" && bits != "·" {
		msg += " (" + strings.Trim(bits, " ·") + ")"
	}
	if s.Authenticated {
		a.Toasts.Success(provider, msg)
	} else {
		a.Toasts.Info(provider, msg)
	}
}

func copilotLogout(ctx context.Context, a *app.Context) {
	msg, err := a.Client.CopilotLogout(ctx)
	if err != nil {
		a.ReportError("Copilot logout failed", err)
		return
	}
	a.Toasts.Success("Copilot", msg)
}

func copilotLogin(ctx context.Context, a *app.Context) {
	l, err := a.Client.StartCopilotLogin(ctx)
	if err != nil {
		a.ReportError("Copilot login failed", err)
		return
	}
	app.OpenExternal(l.VerificationURI)
	a.Toasts.Info("GitHub Copilot", "Enter code "+l.UserCode+" at "+l.VerificationURI)
}
