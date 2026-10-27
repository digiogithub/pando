package setup

import (
	"cmp"
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Build implements app.View.
func (w *wizard) Build(c *ui.Context, r *ui.Route) {
	t := kit.Current()
	if w.status == nil {
		if !w.loading {
			w.loading = true
			w.app.Go(func(ctx context.Context) {
				st, err := fetchStatusRetry(ctx, w.app.Client)
				w.app.Update(func() {
					w.loading = false
					if err != nil {
						w.err = errText(err, "")
						return
					}
					w.status = &st
				})
			})
		}
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			if w.err != "" {
				notice(c, kit.ToneDanger, w.err)
			} else {
				kit.Spinner(c)
			}
		})
		return
	}
	if !w.inited {
		w.reset()
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).AlignItems(ui.Center).Padding(24).Children(func() {
			ui.Column(c).Width(640).MaxWidth(10000).Gap(14).Padding(24).Radius(t.RadiusLG).
				Background(t.BgCard).Border(1, t.Border).Children(func() {
				ui.Text(c, w.t("title")).FontSize(t.TextXL).FontWeight(700)
				ui.Text(c, w.t("subtitle")).FontSize(t.TextSM).TextColor(t.FgMuted)
				w.stepIndicator(c, t)
				switch w.step {
				case stepScope:
					w.scopeStep(c, t)
				case stepProvider:
					w.providerStep(c, t)
				case stepModels:
					w.modelsStep(c, t)
				case stepRemembrances:
					w.remembrancesStep(c, t)
				case stepDone:
					w.doneStep(c, t)
				}
				w.footer(c)
			})
		})
	})
}

func (w *wizard) stepIndicator(c *ui.Context, t *kit.Tokens) {
	ui.Row(c).Gap(6).Children(func() {
		for i, k := range stepKeys {
			ui.Column(c.Key("step:" + k)).Grow(1).Basis(0).Gap(4).Children(func() {
				col := t.BgRaised
				if i <= int(w.step) {
					col = t.Accent
				}
				ui.Box(c).Height(4).Radius(999).Background(col)
				txt := ui.Text(c, w.t("steps."+k)).FontSize(t.TextXS).SingleLine().Ellipsis("…").TextColor(t.FgFaint)
				if i == int(w.step) {
					txt.TextColor(t.Fg).FontWeight(500)
				}
			})
		}
	})
}

func notice(c *ui.Context, tone kit.Tone, text string) {
	t := kit.Current()
	fg, bg := kit.ToneColors(tone)
	ui.Row(c).Padding(8, 12).Radius(t.RadiusSM).Background(bg).Children(func() {
		ui.Text(c, text).FontSize(t.TextSM).TextColor(fg).Grow(1).Shrink(1)
	})
}

// choiceCard is a selectable card (setupShared.tsx ChoiceCard).
func choiceCard(c *ui.Context, key string, selected, disabled bool, icon, title string, badge string, body ...string) bool {
	t := kit.Current()
	b := ui.ButtonBase(c.Key(key)).Radius(t.RadiusMD).Padding(10, 12).Gap(10).AlignItems(ui.Start).
		Justify(ui.Start).Cursor(ui.CursorPointer).Disabled(disabled).Label(title)
	if selected {
		b.Background(t.AccentSoft).Border(1, t.Accent)
	} else {
		b.Background(t.BgCard).Border(1, t.Border)
		if b.Hovered() {
			b.Background(t.BgHover)
		}
	}
	if disabled {
		b.Opacity(0.5)
	}
	b.Children(func() {
		kit.Icon(c, icon).FontSize(18)
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontWeight(600).FontSize(t.TextSM)
				if badge != "" {
					kit.Badge(c, badge, kit.ToneAccent)
				}
			})
			for _, s := range body {
				ui.Text(c, s).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
		})
	})
	return b.Clicked() && !disabled
}

func (w *wizard) scopeStep(c *ui.Context, t *kit.Tokens) {
	st := w.status
	ui.Text(c, w.t("scope.intro")).FontSize(t.TextSM).TextColor(t.FgMuted)
	ui.Text(c, shorten(st.WorkingDir)).FontSize(t.TextXS).Font("monospace").Padding(6, 10).
		Radius(t.RadiusSM).Background(t.BgShell)
	gp := cmp.Or(st.GlobalConfigPath, st.DefaultGlobalConfigPath, "~/.config/pando/.pando.toml")
	if choiceCard(c, "scope:global", w.scope == "global", false, kit.IconHome, w.t("scope.global"), w.t("scope.default"),
		w.t("scope.globalHint"), shorten(gp)) {
		w.scope = "global"
	}
	hint := w.t("scope.projectHint")
	if st.IsHomeDir {
		hint = w.t("scope.projectHome")
	}
	if choiceCard(c, "scope:project", w.scope == "project", st.IsHomeDir, kit.IconFolder, w.t("scope.project"), "",
		hint, shorten(st.WorkingDir+"/.pando.toml")) {
		w.scope = "project"
	}
	w.errNotice(c)
}

func (w *wizard) errNotice(c *ui.Context) {
	if w.err != "" {
		notice(c, kit.ToneDanger, w.err)
	}
}

func (w *wizard) providerStep(c *ui.Context, t *kit.Tokens) {
	if w.copilot != nil {
		w.copilotView(c, t)
		return
	}
	if !w.accountsLoad || w.accounts == nil {
		ui.Row(c).Justify(ui.Center).Padding(24).Children(func() { kit.Spinner(c) })
		return
	}
	if len(w.accounts) > 0 && !w.addingNew {
		notice(c, kit.ToneSuccess, w.app.T("setup.provider.existing", "count", len(w.accounts)))
		ui.Column(c).Gap(6).Children(func() {
			for _, a := range w.accounts {
				ui.Row(c.Key("acc:"+a.ID)).Gap(8).Padding(8, 12).Radius(t.RadiusSM).Background(t.BgCard).
					Border(1, t.Border).AlignItems(ui.Center).Children(func() {
					name := cmp.Or(a.DisplayName, a.ID)
					ui.Text(c, name).FontSize(t.TextSM).FontWeight(500)
					ui.Text(c, a.Type).FontSize(t.TextXS).TextColor(t.FgMuted).Font("monospace")
					if a.Disabled {
						ui.Text(c, w.t("provider.disabled")).FontSize(t.TextXS).TextColor(t.FgFaint)
					}
				})
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			if kit.Button(c.Key("keep"), kit.ButtonOpts{Variant: kit.Primary, Label: w.t("provider.keepExisting")}).Clicked() {
				first := ""
				for _, a := range w.accounts {
					if !a.Disabled {
						first = a.Type
						break
					}
				}
				w.sum.provider = first
				w.go_(stepModels)
			}
			if kit.Button(c.Key("another"), kit.ButtonOpts{Label: w.t("provider.addAnother")}).Clicked() {
				w.addingNew = true
			}
		})
		return
	}
	g := w.guide()
	ui.Text(c, w.t("provider.intro")).FontSize(t.TextSM).TextColor(t.FgMuted)
	ui.Row(c).Wrap().Gap(8).Children(func() {
		for _, p := range providers {
			ui.Column(c.Key("p:" + p.typ)).Width(190).Children(func() {
				if choiceCard(c, "pc:"+p.typ, w.provType == p.typ, false, p.icon, p.label, "") {
					w.selectProvider(p.typ)
					g = w.guide()
				}
			})
		}
	})
	kit.Card(c, func() {
		ui.Text(c, w.t("provider.guide."+g.typ)).FontSize(t.TextSM)
		if g.keyURL != "" {
			if kit.Button(c.Key("getkey"), kit.ButtonOpts{Size: kit.SizeSM, Icon: kit.IconExternal, Label: w.t("provider.getKey")}).Clicked() {
				openExternal(g.keyURL)
			}
		}
		if g.needsBaseURL {
			ui.Text(c, w.t("provider.baseUrl")).FontSize(t.TextSM).TextColor(t.FgMuted)
			kit.Input(c.Key("baseurl:"+g.typ), &w.baseURL, g.defaultBase)
		}
		if g.needsKey {
			ui.Text(c, w.t("provider.apiKey")).FontSize(t.TextSM).TextColor(t.FgMuted)
			kit.Input(c.Key("apikey:"+g.typ), &w.apiKey, "sk-…").Password()
		}
		w.errNotice(c)
		label := w.t("provider.save")
		if g.typ == "copilot" {
			label = w.t("provider.connectCopilot")
		}
		ui.Row(c).Children(func() {
			if kit.Button(c.Key("saveprov"), kit.ButtonOpts{Variant: kit.Primary, Label: label, Disabled: w.busy}).Clicked() {
				w.saveProvider()
			}
		})
	})
	ui.Text(c, w.t("provider.moreInSettings")).FontSize(t.TextXS).TextColor(t.FgFaint)
}

func (w *wizard) copilotView(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).AlignItems(ui.Center).Gap(14).Padding(8, 0).Children(func() {
		ui.Text(c, w.t("provider.copilotIntro")).FontSize(t.TextSM).TextColor(t.FgMuted)
		for _, k := range []string{"copilotStep1", "copilotStep2", "copilotStep3"} {
			ui.Text(c, "• "+w.t("provider."+k)).FontSize(t.TextSM).TextColor(t.FgMuted)
		}
		ui.Text(c, w.copilot.UserCode).FontSize(32).FontWeight(600).Font("monospace").LetterSpacing(6).
			Padding(16, 28).Radius(t.RadiusLG).Background(t.BgShell).Border(1, t.BorderStrong)
		ui.Row(c).Gap(8).Children(func() {
			lbl := w.t("provider.copyCode")
			if w.copied {
				lbl = w.t("common.copied")
			}
			if kit.Button(c.Key("cpcode"), kit.ButtonOpts{Icon: kit.IconCopy, Label: lbl}).Clicked() {
				w.copied = kit.CopyText(w.copilot.UserCode)
			}
			if kit.Button(c.Key("opengh"), kit.ButtonOpts{Variant: kit.Primary, Icon: kit.IconExternal, Label: w.t("provider.openGithub")}).Clicked() {
				openExternal(w.copilot.VerificationURI)
			}
		})
		ui.Text(c, w.copilot.VerificationURI).FontSize(t.TextXS).Font("monospace").TextColor(t.FgMuted)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			kit.Spinner(c)
			ui.Text(c, w.t("provider.copilotWaiting")).FontSize(t.TextSM).TextColor(t.FgMuted)
		})
		if kit.Button(c.Key("cpcancel"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: w.t("provider.copilotCancel")}).Clicked() {
			if w.copilotStop != nil {
				w.copilotStop()
				w.copilotStop = nil
			}
			w.copilot = nil
		}
	})
}

func (w *wizard) modelsStep(c *ui.Context, t *kit.Tokens) {
	ui.Text(c, w.t("models.intro")).FontSize(t.TextSM).TextColor(t.FgMuted)
	if w.modelsLoading {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			kit.Spinner(c)
			ui.Text(c, w.t("models.loading")).FontSize(t.TextSM).TextColor(t.FgMuted)
		})
	}
	field := func(key, label, hint string, v *string, optional bool) {
		ui.Column(c.Key("mf:" + key)).Gap(4).Children(func() {
			ui.Text(c, label).FontSize(t.TextSM).FontWeight(500)
			ui.Text(c, hint).FontSize(t.TextXS).TextColor(t.FgMuted)
			opts := w.modelOptions
			if optional {
				opts = append([]string{""}, opts...)
			}
			if len(opts) > 0 {
				kit.Select(c.Key("msel:"+key), v, opts)
			}
			ph := w.t("models.pick")
			if optional {
				ph = w.t("models.sameAsMain")
			}
			kit.Input(c.Key("minp:"+key), v, ph)
		})
	}
	field("main", w.t("models.main"), w.t("models.mainHint"), &w.mainModel, false)
	field("fast", w.t("models.fast"), w.t("models.fastHint"), &w.fastModel, true)
	if w.modelsNotice != "" {
		notice(c, kit.ToneWarning, w.modelsNotice)
	}
	w.errNotice(c)
	ui.Row(c).Gap(8).Children(func() {
		if kit.Button(c.Key("savemodels"), kit.ButtonOpts{Variant: kit.Primary, Label: w.t("models.save"), Disabled: w.busy}).Clicked() {
			w.saveModels()
		}
		if w.sum.provider != "" {
			if kit.Button(c.Key("refreshmodels"), kit.ButtonOpts{Variant: kit.Ghost, Icon: kit.IconRefresh, Label: w.t("models.refresh")}).Clicked() {
				w.suggest(true)
			}
		}
	})
}

func (w *wizard) remembrancesStep(c *ui.Context, t *kit.Tokens) {
	ui.Text(c, w.t("remembrances.intro")).FontSize(t.TextSM).TextColor(t.FgMuted)
	s := w.oll
	if s == nil {
		ui.Row(c).Gap(8).Justify(ui.Center).Padding(16).Children(func() {
			kit.Spinner(c)
			ui.Text(c, w.t("remembrances.checking")).FontSize(t.TextSM).TextColor(t.FgMuted)
		})
		w.errNotice(c)
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Ollama").FontWeight(600).FontSize(t.TextSM)
		switch {
		case s.Running:
			kit.Badge(c, w.app.T("setup.remembrances.running", "version", s.Version), kit.ToneSuccess)
		case s.Installed:
			kit.Badge(c, w.t("remembrances.stopped"), kit.ToneWarning)
		default:
			kit.Badge(c, w.t("remembrances.notInstalled"), kit.ToneDanger)
		}
		ui.Text(c, s.BaseURL).FontSize(t.TextXS).Font("monospace").TextColor(t.FgFaint).Grow(1)
		if kit.Button(c.Key("recheck"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: kit.IconRefresh,
			Label: w.t("remembrances.checkAgain"), Disabled: w.ollChecking}).Clicked() {
			w.refreshOllama()
		}
	})
	installing := w.jobs["install"] != nil && w.jobs["install"].State == "running"
	if !s.Installed {
		ui.Text(c, w.t("remembrances.installTitle")).FontWeight(500).FontSize(t.TextSM)
		for _, o := range s.InstallOptions {
			kit.Card(c, func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, w.app.T("setup.remembrances.option."+o.ID)).FontWeight(500).FontSize(t.TextSM)
					if o.Recommended {
						kit.Badge(c, w.t("remembrances.recommended"), kit.ToneAccent)
					}
				})
				ui.Text(c, w.app.T("setup.remembrances.optionHint."+o.ID)).FontSize(t.TextXS).TextColor(t.FgMuted)
				if o.Command != "" {
					ui.Text(c, o.Command).FontSize(t.TextXS).Font("monospace").Padding(6, 10).Radius(t.RadiusSM).Background(t.BgShell)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					v := kit.Secondary
					if o.Recommended {
						v = kit.Primary
					}
					if o.URL != "" && kit.Button(c.Key("dl:"+o.ID), kit.ButtonOpts{Variant: v, Size: kit.SizeSM, Icon: kit.IconExternal,
						Label: w.t("remembrances.openDownload")}).Clicked() {
						openExternal(o.URL)
					}
					if o.Command != "" && o.Runnable && w.confirmOpt != o.ID {
						if kit.Button(c.Key("run:"+o.ID), kit.ButtonOpts{Variant: v, Size: kit.SizeSM, Label: w.t("remembrances.runForMe"),
							Disabled: installing}).Clicked() {
							w.confirmOpt = o.ID
						}
					}
					if w.confirmOpt == o.ID {
						ui.Text(c, w.t("remembrances.confirmRun")).FontSize(t.TextXS).TextColor(t.FgMuted)
						if kit.Button(c.Key("go:"+o.ID), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Label: w.t("remembrances.confirm")}).Clicked() {
							w.install(o.ID)
						}
						if kit.Button(c.Key("no:"+o.ID), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: w.t("common.cancel")}).Clicked() {
							w.confirmOpt = ""
						}
					}
					if o.Command != "" && !o.Runnable && o.Reason != "" {
						ui.Text(c, o.Reason).FontSize(t.TextXS).TextColor(t.FgFaint)
					}
				})
			})
		}
	}
	if installing || len(w.installLog) > 0 {
		lbl := w.t("remembrances.installOutput")
		if installing {
			lbl = w.t("remembrances.installing")
		}
		ui.Text(c, lbl).FontSize(t.TextXS).TextColor(t.FgMuted)
		log := w.installLog
		if len(log) > 12 {
			log = log[len(log)-12:]
		}
		ui.Text(c, strings.Join(log, "\n")).FontSize(t.TextXS).Font("monospace").TextColor(t.FgMuted).
			Padding(8).Radius(t.RadiusSM).Background(t.BgShell)
	}
	if s.Installed && !s.Running {
		notice(c, kit.ToneWarning, w.t("remembrances.notRunning"))
		if s.CanStart {
			if kit.Button(c.Key("startoll"), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Label: w.t("remembrances.start"), Disabled: w.busy}).Clicked() {
				w.startOllama()
			}
		}
	}
	docPresent, codePresent := hasModel(s.Models, w.docModel), hasModel(s.Models, w.codeModel)
	if s.Installed || s.Running {
		ui.Text(c, w.t("remembrances.modelsTitle")).FontWeight(500).FontSize(t.TextSM)
		w.pullRow(c, t, "doc", w.t("remembrances.docModel"), w.t("remembrances.docModelHint"), &w.docModel, docPresent, s.Running)
		w.pullRow(c, t, "code", w.t("remembrances.codeModel"), w.t("remembrances.codeModelHint"), &w.codeModel, codePresent, s.Running)
	}
	w.errNotice(c)
	if s.Running && (!docPresent || !codePresent) {
		notice(c, kit.ToneInfo, w.t("remembrances.pullFirst"))
	}
	ui.Row(c).Children(func() {
		if kit.Button(c.Key("enable"), kit.ButtonOpts{Variant: kit.Primary, Label: w.t("remembrances.enable"),
			Disabled: !s.Running || strings.TrimSpace(w.docModel) == "" || w.busy}).Clicked() {
			w.enableRemembrances()
		}
	})
}

func (w *wizard) pullRow(c *ui.Context, t *kit.Tokens, key, label, hint string, model *string, present, canPull bool) {
	kit.Card(c, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).FontWeight(500).FontSize(t.TextSM)
			if present {
				kit.Badge(c, w.t("remembrances.installed"), kit.ToneSuccess)
			} else {
				kit.Badge(c, w.t("remembrances.missing"), kit.ToneWarning)
			}
		})
		ui.Text(c, hint).FontSize(t.TextXS).TextColor(t.FgMuted)
		kit.Input(c.Key("model:"+key), model, "")
		ui.Text(c, "ollama pull "+*model).FontSize(t.TextXS).Font("monospace").Padding(6, 10).Radius(t.RadiusSM).Background(t.BgShell)
		j := w.jobs[*model]
		running := j != nil && j.State == "running"
		if running {
			frac := -1.0
			if j.Total > 0 {
				frac = float64(j.Completed) / float64(j.Total)
			}
			ui.Progress(c, frac).Label(j.Status)
			ui.Text(c, j.Status).FontSize(t.TextXS).TextColor(t.FgMuted)
		}
		if !present {
			lbl := w.t("remembrances.pull")
			if running {
				lbl = w.t("remembrances.pulling")
			}
			ui.Row(c).Children(func() {
				if kit.Button(c.Key("pull:"+key), kit.ButtonOpts{Size: kit.SizeSM, Icon: "download", Label: lbl,
					Disabled: !canPull || running || strings.TrimSpace(*model) == ""}).Clicked() {
					w.pull(*model)
				}
			})
		}
	})
}

func (w *wizard) doneStep(c *ui.Context, t *kit.Tokens) {
	notice(c, kit.ToneSuccess, w.t("done.intro"))
	if w.err != "" {
		notice(c, kit.ToneDanger, w.err)
	}
	unchanged := w.t("done.unchanged")
	scope := "—"
	if w.sum.scope != "" {
		scope = w.t("scope." + w.sum.scope)
		if w.sum.configPath != "" {
			scope += "   " + shorten(w.sum.configPath)
		}
	}
	rem := w.t("done.skipped")
	if w.sum.remembrances {
		rem = w.sum.docModel + " · " + w.sum.codeModel
	}
	rows := [][2]string{
		{w.t("done.scope"), scope},
		{w.t("done.provider"), cmp.Or(w.sum.provider, unchanged)},
		{w.t("done.mainModel"), cmp.Or(w.sum.mainModel, unchanged)},
		{w.t("done.fastModel"), cmp.Or(w.sum.fastModel, unchanged)},
		{w.t("done.remembrances"), rem},
	}
	ui.Column(c).Gap(6).Children(func() {
		for _, r := range rows {
			ui.Row(c.Key("sum:" + r[0])).Gap(16).Children(func() {
				ui.Text(c, r[0]).FontSize(t.TextSM).TextColor(t.FgMuted).Width(140)
				ui.Text(c, r[1]).FontSize(t.TextSM).Grow(1).Shrink(1)
			})
		}
	})
	ui.Text(c, w.t("done.settingsHint")).FontSize(t.TextXS).TextColor(t.FgFaint)
}

func (w *wizard) footer(c *ui.Context) {
	st := w.status
	ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Margin(8, 0, 0, 0).Children(func() {
		if w.step != stepDone {
			if kit.Button(c.Key("f:cancel"), kit.ButtonOpts{Variant: kit.Ghost, Label: w.t("cancel")}).Clicked() {
				w.cancel()
			}
		}
		ui.Box(c).Grow(1)
		first := stepScope
		if st.HasLocalConfig {
			first = stepProvider
		}
		if w.step != first && w.step != stepDone {
			if kit.Button(c.Key("f:back"), kit.ButtonOpts{Icon: kit.IconChevronL, Label: w.t("back"), Disabled: w.busy || w.copilot != nil}).Clicked() {
				w.go_(w.step - 1)
			}
		}
		if w.step == stepScope {
			if kit.Button(c.Key("f:next"), kit.ButtonOpts{Variant: kit.Primary, Label: w.t("next"), Disabled: w.busy}).Clicked() {
				w.applyScope()
			}
		}
		skip := step(-1)
		switch {
		case w.step == stepProvider && st.ProviderAccounts > 0:
			skip = stepModels
		case w.step == stepModels:
			skip = stepRemembrances
		case w.step == stepRemembrances:
			skip = stepDone
		}
		if skip >= 0 {
			if kit.Button(c.Key("f:skip"), kit.ButtonOpts{Label: w.t("skip"), Disabled: w.busy || w.copilot != nil}).Clicked() {
				w.go_(skip)
			}
		}
		if w.step == stepDone {
			if kit.Button(c.Key("f:finish"), kit.ButtonOpts{Variant: kit.Primary, Icon: kit.IconCheck, Label: w.t("finish"), Disabled: w.busy}).Clicked() {
				w.finish()
			}
		}
	})
}

var _ = client.SetupStatus{}
