package settings

import (
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// appearance mirrors AppearanceSettings.tsx: mode and family are staged into
// the backend `theme` field ("family-mode", shared with the TUI) and saved
// with the Save button; the accent is local.
type appearance struct {
	*Doc
	saved kit.Settings // theme applied before the edits (Discard restores it)
}

func newAppearance(s *Settings) section {
	d := newDoc(s, client.PathSettings, Group{Fields: []Field{{Path: "theme", Label: "Theme", Kind: KText, Visible: func(*Doc) bool { return false }}}})
	d.Skip = []string{"language"}
	ap := &appearance{Doc: d, saved: s.app.Theme}
	d.Loaded = func(d *Doc, root client.Obj) {
		// The live theme was applied at start (desktop preferences, else
		// this backend field): opening the page must not switch it.
		ap.saved = s.app.Theme
	}
	return ap
}

// Save stores the backend theme and the desktop preferences (theme, mode
// and accent, applied at the next start).
func (a *appearance) Save(done func(bool)) {
	a.Doc.Save(func(ok bool) {
		if ok {
			a.saved = a.s.app.Theme
			a.s.app.SetTheme(a.s.app.Theme)
		}
		done(ok)
	})
}

func (a *appearance) Discard() {
	a.s.app.Theme.Family, a.s.app.Theme.Mode = a.saved.Family, a.saved.Mode
	a.Doc.Discard()
}

func (a *appearance) stage() {
	th := a.s.app.Theme
	a.vals["theme"].s = string(th.Family) + "-" + string(th.Mode)
}

func (a *appearance) Build(c *ui.Context) {
	s, t := a.s, kit.Current()
	s.pageHeader(c, s.app.T("settings.categories.appearance"), s.t("Choose how Pando looks on this device. Mode and theme are also used by the TUI; the accent is local to this app.", "settings.appearance.description"))
	if !a.loaded {
		a.loadingView(c)
		return
	}
	th := &s.app.Theme
	modes := []kit.Mode{kit.ModeLight, kit.ModeDark, kit.ModeSystem}
	labels := []string{s.app.T("settings.general.themeLight"), s.app.T("settings.general.themeDark"), s.t("System", "settings.general.themeSystem")}
	mi := 0
	for i, m := range modes {
		if m == th.Mode {
			mi = i
		}
	}
	kit.SettingsSection(c, "", "", func() {
		kit.SettingsRow(c, s.t("Appearance mode", "settings.appearance.mode"), s.t("System follows your OS setting and switches live.", "settings.appearance.modeDescription"), func() {
			if ui.Segmented(c, &mi, labels...).Changed() {
				th.Mode = modes[mi]
				a.stage()
			}
		})
	})
	kit.SettingsSection(c, s.t("Theme", "settings.appearance.themeFamily"), s.t("A colour family for surfaces and text. Mode above decides light or dark.", "settings.appearance.themeFamilyDescription"), func() {
		ui.Row(c).Gap(12).Padding(12).Wrap().Children(func() {
			for _, f := range kit.Families() {
				tk := kit.Resolve(f, t.Dark, th.Accent)
				cc := c.Key("fam:" + string(f))
				b := ui.ButtonBase(cc).Label(string(f)).Width(120).Height(86).Radius(t.RadiusMD).Padding(8).Gap(6).Cursor(ui.CursorPointer).
					Background(tk.Bg).Border(1, t.Border)
				if f == th.Family {
					b.Border(2, t.Accent)
				}
				b.Children(func() {
					// ButtonBase lays children in a row: stack preview and name.
					ui.Column(cc).Grow(1).Gap(6).AlignItems(ui.Stretch).Children(func() {
						ui.Column(cc).Grow(1).Radius(t.RadiusSM).Background(tk.BgShell).Padding(6).Gap(4).Children(func() {
							ui.Box(cc).Height(5).Width(60).Radius(2).Background(tk.Fg)
							ui.Box(cc).Height(5).Width(40).Radius(2).Background(tk.FgMuted)
							ui.Box(cc).Height(8).Width(8).Radius(4).Background(tk.Accent)
						})
						ui.Text(cc, strings.ToUpper(string(f)[:1])+string(f)[1:]).FontSize(t.TextXS).TextColor(tk.Fg)
					})
				})
				if b.Clicked() {
					th.Family = f
					a.stage()
				}
			}
		})
	})
	kit.SettingsSection(c, s.t("Accent colour", "settings.appearance.accent"), s.t("Used for the primary action, focus and selection.", "settings.appearance.accentDescription"), func() {
		ui.Row(c).Gap(10).Padding(12).Wrap().Children(func() {
			for _, ac := range append([]kit.Accent{""}, kit.Accents()...) {
				cc := c.Key("acc:" + string(ac))
				tk := kit.Resolve(th.Family, t.Dark, ac)
				name := string(ac)
				if name == "" {
					name = s.t("Theme default", "settings.appearance.accentDefault")
				}
				b := ui.ButtonBase(cc).Label(name).Width(28).Height(28).Radius(14).Background(tk.Accent).Cursor(ui.CursorPointer).Tooltip(name)
				if ac == th.Accent {
					b.Border(3, t.Fg)
				}
				if b.Clicked() {
					th.Accent = ac
					// Not part of the backend document: persisted at once
					// in the desktop preferences.
					s.app.SavePrefs(client.UIPrefs{ThemeAccent: string(ac)})
				}
			}
		})
	})
}
