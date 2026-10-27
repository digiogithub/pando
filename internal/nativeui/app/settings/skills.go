package settings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// skillsSection is the Skills category: the enable/paths/catalog config is a
// Doc over /api/v1/config/extensions (Save / Reset bar), plus the installed
// skills list and the online catalogue browser (immediate actions).
type skillsSection struct {
	*Doc
	s *Settings

	installed    []client.InstalledSkill
	instLoading  bool
	instErr      string
	uninstall    string
	confirm      bool
	catOpen      bool
	search       string
	lastQuery    string
	queryAt      time.Time
	hits         []client.SkillCatalogItem
	searching    bool
	searchErr    string
	installing   string
	catScroll    ui.ScrollState
	searchSerial int
}

func newSkills(s *Settings) section {
	d := newDoc(s, client.PathConfigExtensions,
		Group{Fields: []Field{sw("skills.enabled", "Enable skills", "Allow Pando to discover and inject skills into sessions")}},
		Group{Title: "Skill paths", Desc: "Local skill search directories", Fields: []Field{list("skills.paths", "Skill paths", "", "/path/to/my-skills")}},
		Group{Title: "Catalog", Fields: []Field{
			sw("skillsCatalog.enabled", "Enable catalog", "Browse and install skills from the online catalog"),
			txt("skillsCatalog.baseUrl", "Base URL", ""),
			sw("skillsCatalog.autoUpdate", "Auto update", "Automatically update installed skills when new versions are available"),
			{Path: "skillsCatalog.defaultScope", Label: "Default scope", Kind: KSelect, Opts: []Opt{{"session", "Session"}, {"global", "Global"}, {"project", "Project"}}},
		}})
	d.PutWhole = true
	d.SavedMsg = "Skills settings saved"
	k := &skillsSection{Doc: d, s: s}
	d.Head = func(c *ui.Context, _ *Doc) { k.installedView(c) }
	d.Tail = func(c *ui.Context, d *Doc) {
		if kit.Button(c.Key("browse"), kit.ButtonOpts{Label: "Browse Catalog", Variant: kit.Primary, Disabled: !d.Bool("skillsCatalog.enabled")}).Clicked() {
			k.catOpen = true
		}
	}
	return k
}

func (k *skillsSection) Load() {
	k.Doc.Load()
	k.loadInstalled()
}

func (k *skillsSection) loadInstalled() {
	k.instLoading = true
	a := k.s.app
	a.Go(func(ctx context.Context) {
		list, err := a.Client.InstalledSkills(ctx)
		a.Update(func() {
			k.instLoading = false
			if err != nil {
				k.instErr = err.Error()
				return
			}
			k.instErr, k.installed = "", list
		})
	})
}

func (k *skillsSection) Build(c *ui.Context) {
	k.s.pageHeader(c, k.s.app.T("settings.categories.skills"), "")
	k.Doc.Build(c)
	k.dialogs(c)
}

func (k *skillsSection) installedView(c *ui.Context) {
	t := kit.Current()
	kit.SettingsSection(c, "Installed skills", "", func() {
		switch {
		case k.instLoading && len(k.installed) == 0:
			ui.Text(c, "Loading installed skills…").Padding(12, 16).TextColor(t.FgMuted)
		case k.instErr != "":
			ui.Text(c, k.instErr).Padding(12, 16).TextColor(t.Danger)
		case len(k.installed) == 0:
			ui.Text(c, "No skills installed. Skills are loaded from ~/.pando/skills/ and .pando/skills/.").Padding(12, 16).TextColor(t.FgMuted).FontSize(t.TextSM)
		}
		for i := range k.installed {
			sk := k.installed[i]
			cc := c.Key("sk:" + sk.Name)
			name := sk.Name
			if sk.Version != "" {
				name += " v" + sk.Version
			}
			desc := sk.Description
			if sk.Source != "" && sk.Source != "(local)" {
				desc += "\n" + sk.Source
			}
			kit.SettingsRow(cc, name, strings.TrimSpace(desc), func() {
				kit.Badge(cc, sk.Scope, kit.ToneNeutral)
				if sk.Active {
					kit.Badge(cc, "active", kit.ToneAccent)
				}
				if kit.Button(cc.Key("un"), kit.ButtonOpts{Label: "Uninstall", Variant: kit.Danger, Size: kit.SizeSM}).Clicked() {
					k.uninstall, k.confirm = sk.Name, true
				}
			})
		}
		ui.Row(c).Padding(8, 16).Children(func() {
			if kit.Button(c.Key("refresh"), kit.ButtonOpts{Label: k.s.app.T("common.refresh"), Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
				k.loadInstalled()
			}
		})
	})
}

func (k *skillsSection) scope() string {
	if k.Get("skillsCatalog.defaultScope") == "project" {
		return "project"
	}
	return "global"
}

func (k *skillsSection) runSearch() {
	q := strings.TrimSpace(k.search)
	k.lastQuery = k.search
	if len(q) < 2 {
		k.hits, k.searching = nil, false
		return
	}
	k.searchSerial++
	serial := k.searchSerial
	k.searching, k.searchErr = true, ""
	a := k.s.app
	a.Go(func(ctx context.Context) {
		hits, err := a.Client.SearchSkills(ctx, q)
		a.Update(func() {
			if serial != k.searchSerial {
				return
			}
			k.searching = false
			if err != nil {
				k.searchErr = err.Error()
				return
			}
			k.hits = hits
		})
	})
}

func (k *skillsSection) dialogs(c *ui.Context) {
	t := kit.Current()
	a := k.s.app
	crConfirm(c, &k.confirm, "Uninstall Skill", fmt.Sprintf("Are you sure you want to uninstall %q? This action cannot be undone.", k.uninstall), "Uninstall", func() {
		name := k.uninstall
		a.Go(func(ctx context.Context) {
			err := a.Client.UninstallSkill(ctx, name)
			a.Update(func() {
				if err != nil {
					k.instErr = err.Error()
					a.Toasts.Error(err.Error(), "")
					return
				}
				a.Toasts.Success(fmt.Sprintf("Skill %q uninstalled", name), "")
				k.loadInstalled()
			})
		})
	})
	kit.Dialog(c, &k.catOpen, kit.DialogOpts{Title: "Skills Catalog", Width: 600,
		Body: func() {
			// Debounced search: 300 ms after the last keystroke, min 2 chars.
			if k.search != k.lastQuery {
				k.lastQuery = k.search
				k.queryAt = time.Now()
				a.Go(func(ctx context.Context) {
					time.Sleep(300 * time.Millisecond)
					a.Update(func() {
						if time.Since(k.queryAt) >= 290*time.Millisecond && strings.TrimSpace(k.search) == strings.TrimSpace(k.lastQuery) {
							k.runSearch()
						}
					})
				})
			}
			kit.Input(c, &k.search, "Type to search skills… (min 2 chars)")
			ui.Scroll(c).TrackScroll(&k.catScroll).Height(320).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					switch {
					case len(strings.TrimSpace(k.search)) < 2:
						ui.Text(c, "Start typing to search the skills catalog.").TextColor(t.FgMuted).FontSize(t.TextSM)
					case k.searching:
						ui.Text(c, "Searching…").TextColor(t.FgMuted).FontSize(t.TextSM)
					case k.searchErr != "":
						ui.Text(c, k.searchErr).TextColor(t.Danger).FontSize(t.TextSM)
					case len(k.hits) == 0:
						ui.Text(c, "No skills found.").TextColor(t.FgMuted).FontSize(t.TextSM)
					}
					for _, it := range k.hits {
						cc := c.Key("hit:" + it.ID + it.Name)
						done := false
						for _, in := range k.installed {
							done = done || in.Name == it.Name
						}
						sub := it.Source
						if it.Installs > 0 {
							sub += "  ·  " + installs(it.Installs)
						}
						ui.Row(cc).Padding(8, 0).Gap(12).AlignItems(ui.Center).Children(func() {
							ui.Column(cc).Grow(1).Gap(2).Children(func() {
								ui.Text(cc, it.Name).FontWeight(600).FontSize(t.TextSM)
								ui.Text(cc, sub).FontSize(t.TextXS).TextColor(t.FgMuted)
							})
							switch {
							case done:
								kit.Badge(cc, "Installed", kit.ToneAccent)
							default:
								lbl := "Install"
								if k.installing == it.Name {
									lbl = "Installing…"
								}
								if kit.Button(cc.Key("inst"), kit.ButtonOpts{Label: lbl, Size: kit.SizeSM, Disabled: k.installing != ""}).Clicked() {
									k.install(it)
								}
							}
						})
					}
				})
			})
		},
		Footer: func() {
			if kit.SecondaryButton(c.Key("cat-close"), "Close").Clicked() {
				k.catOpen = false
			}
		}})
}

func installs(n int) string {
	switch {
	case n >= 1_000_000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e6), ".0") + "M installs"
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1e3), ".0") + "K installs"
	}
	return fmt.Sprintf("%d installs", n)
}

func (k *skillsSection) install(it client.SkillCatalogItem) {
	k.installing = it.Name
	scope := k.scope()
	a := k.s.app
	a.Go(func(ctx context.Context) {
		err := a.Client.InstallSkill(ctx, it, scope)
		a.Update(func() {
			k.installing = ""
			if err != nil {
				k.searchErr = err.Error()
				a.Toasts.Error(err.Error(), "")
				return
			}
			a.Toasts.Success(fmt.Sprintf("Skill %q installed", it.Name), "")
			k.loadInstalled()
		})
	})
}
