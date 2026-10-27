package settings

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

var (
	lspActivateOn = [][2]string{
		{"edits", "edits — files Pando edits (default)"}, {"reads", "reads — also files it opens or views"},
		{"workspace", "workspace — also files changed outside Pando"}, {"off", "off — never start a server on demand"}}
	lspRunners = [][2]string{
		{"auto", "auto — bun when available, npm otherwise (default)"}, {"bun", "bun — only bun, never npm"},
		{"npm", "npm — only npm/npx, even if bun is installed"}, {"off", "off — never use bun or npm"}}
)

type lspForm struct {
	language, command          string
	args, languages, filenames []string
	addArg, addLang, addFile   string
	disabled, autostart        bool
}

type lspSection struct {
	status
	s          *Settings
	configs    []client.LSPConfig
	catalog    []client.LSPServerStatus
	act        client.LSPActivation
	startup    string // timeout inputs
	install    string
	editing    bool
	editLang   string
	form       lspForm
	confirm    bool
	delLang    string
	showCat    bool
	formScroll ui.ScrollState
}

func newLSP(s *Settings) section {
	return &lspSection{s: s, act: client.LSPActivation{AutoActivate: true, ActivateOn: "edits", AutoInstall: true, Runner: "auto", StartupTimeout: "20s", InstallTimeout: "2m0s"}}
}

func (l *lspSection) SelfManaged()         {}
func (l *lspSection) Dirty() bool          { return false }
func (l *lspSection) Save(done func(bool)) { done(true) }
func (l *lspSection) Discard()             {}

func (l *lspSection) Load() {
	if l.loading {
		return
	}
	l.loading = true
	a := l.s.app
	a.Go(func(ctx context.Context) {
		cfgs, act, err := a.Client.LSPConfigs(ctx)
		var cat []client.LSPServerStatus
		if err == nil {
			cat, _ = a.Client.LSPCatalog(ctx)
		}
		a.Update(func() {
			l.loading, l.loaded = false, true
			if err != nil {
				l.err = err.Error()
				return
			}
			l.err, l.configs, l.catalog = "", cfgs, cat
			if act != nil {
				l.act = *act
			}
			l.startup, l.install = l.act.StartupTimeout, l.act.InstallTimeout
		})
	})
}

// reload refreshes configs and catalogue after a change.
func (l *lspSection) reload() {
	l.loading = false
	l.Load()
}

func (l *lspSection) saveActivation(next client.LSPActivation) {
	prev := l.act
	l.act, l.saving = next, true
	a := l.s.app
	a.Go(func(ctx context.Context) {
		got, err := a.Client.SaveLSPActivation(ctx, next)
		a.Update(func() {
			l.saving = false
			if err != nil {
				l.act = prev
				a.Toasts.Error(err.Error(), "")
				return
			}
			l.act = got
			l.startup, l.install = got.StartupTimeout, got.InstallTimeout
			a.Toasts.Success("LSP activation settings saved", "")
			l.reload()
		})
	})
}

func (l *lspSection) openForm(cfg *client.LSPConfig, preset *client.LSPServerStatus) {
	f := lspForm{}
	l.editLang = ""
	switch {
	case cfg != nil:
		l.editLang = cfg.Language
		f = lspForm{language: cfg.Language, command: cfg.Command, args: cfg.Args, languages: cfg.Languages, filenames: cfg.Filenames, disabled: cfg.Disabled, autostart: cfg.Autostart}
	case preset != nil:
		f = lspForm{language: preset.Name, command: preset.Command, args: preset.Args, languages: preset.Languages, filenames: preset.Filenames, autostart: preset.Autostart}
	}
	l.form, l.editing = f, true
}

func (l *lspSection) saveForm() {
	f := l.form
	a := l.s.app
	if strings.TrimSpace(f.language) == "" {
		a.Toasts.Error("Language key is required", "")
		return
	}
	if strings.TrimSpace(f.command) == "" {
		a.Toasts.Error("Command is required", "")
		return
	}
	cfg := client.LSPConfig{Language: strings.TrimSpace(f.language), Command: strings.TrimSpace(f.command),
		Args: nz(f.args), Languages: nz(f.languages), Filenames: nz(f.filenames), Disabled: f.disabled, Autostart: f.autostart}
	l.saving = true
	a.Go(func(ctx context.Context) {
		err := a.Client.SaveLSP(ctx, cfg)
		a.Update(func() {
			l.saving = false
			if err != nil {
				a.Toasts.Error(err.Error(), "")
				return
			}
			l.editing = false
			a.Toasts.Success(fmt.Sprintf("LSP %q saved", cfg.Language), "")
			l.reload()
		})
	})
}

func nz(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func (l *lspSection) delete(lang string) {
	a := l.s.app
	a.Go(func(ctx context.Context) {
		err := a.Client.DeleteLSP(ctx, lang)
		a.Update(func() {
			if err != nil {
				a.Toasts.Error(err.Error(), "")
				return
			}
			a.Toasts.Success(fmt.Sprintf("LSP %q deleted", lang), "")
			l.reload()
		})
	})
}

func (l *lspSection) statusOf(name string) *client.LSPServerStatus {
	for i := range l.catalog {
		if l.catalog[i].Name == name {
			return &l.catalog[i]
		}
	}
	return nil
}

func lspTone(avail string) kit.Tone {
	switch avail {
	case "installed":
		return kit.ToneSuccess
	case "installable":
		return kit.ToneAccent
	}
	return kit.ToneNeutral
}

func (l *lspSection) Build(c *ui.Context) {
	s, t := l.s, kit.Current()
	s.crHeader(c, "Language Servers (LSP)", "Configure LSP servers Pando starts to read diagnostics and navigate code.", "Add LSP", func() { l.openForm(nil, nil) })
	if !l.loaded {
		s.loadingView(c, &l.status)
		return
	}
	l.activation(c)
	if len(l.configs) == 0 {
		ui.Text(c, "No language server configured explicitly. Pando still activates the built-in catalogue below on demand.").FontSize(t.TextSM).TextColor(t.FgMuted)
	} else {
		kit.SettingsSection(c, "Configured servers", "", func() {
			for i := range l.configs {
				cfg := &l.configs[i]
				cc := c.Key("lsp:" + cfg.Language)
				st := l.statusOf(cfg.Language)
				kit.SettingsRow(cc, cfg.Language, cfg.Command+" "+strings.Join(cfg.Args, " "), func() {
					if cfg.Disabled {
						kit.Badge(cc, "Disabled", kit.ToneNeutral)
					} else {
						kit.Badge(cc, "Enabled", kit.ToneSuccess)
					}
					if st != nil {
						kit.Badge(cc, st.AvailabilityLabel, lspTone(st.Availability))
						if st.RunState == "ready" {
							kit.Badge(cc, "running", kit.ToneInfo)
						}
					}
					if kit.Button(cc.Key("edit"), kit.ButtonOpts{Icon: kit.IconEdit, Tooltip: "Edit", Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
						l.openForm(cfg, nil)
					}
					if kit.Button(cc.Key("test"), kit.ButtonOpts{Icon: kit.IconRefresh, Tooltip: "Test", Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
						l.test(cfg.Language)
					}
					if kit.Button(cc.Key("del"), kit.ButtonOpts{Icon: kit.IconTrash, Tooltip: "Delete", Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
						l.confirm, l.delLang = true, cfg.Language
					}
				})
			}
		})
	}
	l.catalogView(c)
	l.dialogs(c)
}

// test reports what the server resolved to (the same messages as the web).
func (l *lspSection) test(lang string) {
	a := l.s.app
	st := l.statusOf(lang)
	switch {
	case st == nil:
		a.Toasts.Info(lang+": no status available yet", "")
	case st.Availability == "installed":
		a.Toasts.Info(fmt.Sprintf("%s: %s (%s)", lang, orStr(st.ResolvedCommand, st.Command), st.RunState), "")
	case st.Availability == "installable":
		a.Toasts.Info(fmt.Sprintf("%s: %s — Pando will install it on first use", lang, st.AvailabilityLabel), "")
	default:
		a.Toasts.Error(fmt.Sprintf("%s: %s", lang, orStr(st.Reason, "not installed")), "")
	}
}

func (l *lspSection) activation(c *ui.Context) {
	act := l.act
	kit.SettingsSection(c, "On-demand activation", "", func() {
		kit.SettingsRow(c, "On-demand activation", "Start a language server when Pando touches a file it handles, instead of at boot", func() {
			v := act.AutoActivate
			kit.Switch(c.Key("act"), &v, "")
			if v != act.AutoActivate {
				act.AutoActivate = v
				l.saveActivation(act)
			}
		})
		kit.SettingsRow(c, "Activate on", "", func() {
			v := act.ActivateOn
			crSelect(c.Key("on"), &v, lspActivateOn).Width(300).Disabled(!act.AutoActivate || l.saving)
			if v != act.ActivateOn {
				act.ActivateOn = v
				l.saveActivation(act)
			}
		})
		kit.SettingsRow(c, "Install servers automatically", "Install npm-distributed servers with bun or npm when their binary is missing", func() {
			v := act.AutoInstall
			kit.Switch(c.Key("inst"), &v, "")
			if v != act.AutoInstall {
				act.AutoInstall = v
				l.saveActivation(act)
			}
		})
		kit.SettingsRow(c, "Package manager", `With "off", npm-distributed servers are only used when their binary is already on PATH.`, func() {
			v := act.Runner
			crSelect(c.Key("run"), &v, lspRunners).Width(300).Disabled(l.saving)
			if v != act.Runner {
				act.Runner = v
				l.saveActivation(act)
			}
		})
		kit.SettingsRow(c, "Timeouts", "Startup / install timeout (Go durations)", func() {
			kit.Input(c.Key("t1"), &l.startup, "20s").Width(100)
			kit.Input(c.Key("t2"), &l.install, "2m").Width(100)
			if kit.Button(c.Key("tapply"), kit.ButtonOpts{Label: "Apply", Size: kit.SizeSM, Disabled: l.saving || (l.startup == l.act.StartupTimeout && l.install == l.act.InstallTimeout)}).Clicked() {
				act.StartupTimeout, act.InstallTimeout = l.startup, l.install
				l.saveActivation(act)
			}
		})
	})
}

func (l *lspSection) catalogView(c *ui.Context) {
	t := kit.Current()
	var avail []client.LSPServerStatus
	for _, s := range l.catalog {
		if !s.Configured {
			avail = append(avail, s)
		}
	}
	if len(avail) == 0 {
		return
	}
	icon := kit.IconChevronR
	if l.showCat {
		icon = kit.IconChevronDown
	}
	if kit.Button(c.Key("cat-toggle"), kit.ButtonOpts{Icon: icon, Label: fmt.Sprintf("Built-in catalogue (%d more servers)", len(avail)), Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
		l.showCat = !l.showCat
	}
	if !l.showCat {
		return
	}
	ui.Text(c, "These servers are activated on demand without any configuration. Installable servers are installed by Pando with bun or npm the first time they are needed; opt-in servers stay off until you add them.").FontSize(t.TextXS).TextColor(t.FgMuted)
	kit.SettingsSection(c, "", "", func() {
		for i := range avail {
			sv := &avail[i]
			cc := c.Key("cat:" + sv.Name)
			desc := sv.Description
			if len(sv.Languages) > 0 {
				desc += " · " + strings.Join(sv.Languages, " ")
			}
			if len(sv.Filenames) > 0 {
				desc += " · " + strings.Join(sv.Filenames, " ")
			}
			if sv.Availability == "manual" && (sv.Hint != "" || sv.Reason != "") {
				desc += "\n" + orStr("run: "+sv.Hint, sv.Reason)
			}
			name := sv.Name
			if sv.OptIn {
				name += " (opt-in)"
			}
			kit.SettingsRow(cc, name, desc, func() {
				kit.Badge(cc, sv.AvailabilityLabel, lspTone(sv.Availability))
				lbl := "Customize"
				if sv.OptIn {
					lbl = "Enable"
				}
				if kit.Button(cc.Key("en"), kit.ButtonOpts{Label: lbl, Size: kit.SizeSM}).Clicked() {
					l.openForm(nil, sv)
				}
			})
		}
	})
}

func (l *lspSection) dialogs(c *ui.Context) {
	f := &l.form
	title := "Add Language Server"
	if l.editLang != "" {
		title = "Edit: " + l.editLang
	}
	kit.Dialog(c, &l.editing, kit.DialogOpts{Title: title, Width: 560,
		Body: func() {
			ui.Scroll(c).TrackScroll(&l.formScroll).Height(420).Children(func() {
				ui.Column(c).Gap(12).Padding(0, 8, 0, 0).Children(func() {
					crField(c, "Language", func() {
						if l.editLang == "" {
							ui.Column(c).Gap(6).Children(func() {
								var avail [][2]string
								avail = append(avail, [2]string{"", "Select a catalogue server…"})
								for _, sv := range l.catalog {
									if !sv.Configured {
										avail = append(avail, [2]string{sv.Name, fmt.Sprintf("%s — %s [%s]", sv.Name, sv.Description, sv.AvailabilityLabel)})
									}
								}
								pick := ""
								if sv := l.statusOf(f.language); sv != nil && !sv.Configured {
									pick = f.language
								}
								was := pick
								crSelect(c.Key("preset"), &pick, avail)
								if pick != was && pick != "" {
									if sv := l.statusOf(pick); sv != nil {
										l.openForm(nil, sv)
									}
								}
								kit.Input(c, &f.language, "or type a custom key (e.g. kotlin)")
							})
						} else {
							kit.Input(c, &f.language, "").Disabled(true)
						}
					})
					crField(c, "Command", func() { kit.Input(c, &f.command, "gopls") })
					crField(c, "Arguments", func() { tagList(c.Key("args"), &f.args, &f.addArg, "Add argument…") })
					crField(c, "Languages (extensions)", func() { tagList(c.Key("langs"), &f.languages, &f.addLang, ".go") })
					crField(c, "File names", func() { tagList(c.Key("files"), &f.filenames, &f.addFile, "Dockerfile") })
					kit.Switch(c.Key("auto"), &f.autostart, "Autostart — start this server at boot instead of waiting for a matching file")
					kit.Switch(c.Key("dis"), &f.disabled, "Disabled — disable this language server without removing it")
				})
			})
		},
		Footer: func() {
			if kit.SecondaryButton(c.Key("lsp-cancel"), "Cancel").Clicked() {
				l.editing = false
			}
			lbl := l.s.app.T("common.save")
			if l.saving {
				lbl = l.s.app.T("common.saving")
			}
			if kit.Button(c.Key("lsp-save"), kit.ButtonOpts{Label: lbl, Variant: kit.Primary, Disabled: l.saving}).Clicked() {
				l.saveForm()
			}
		}})
	crConfirm(c, &l.confirm, "Delete Language Server", fmt.Sprintf("Remove LSP configuration for %q?", l.delLang), "Delete", func() { l.delete(l.delLang) })
}
