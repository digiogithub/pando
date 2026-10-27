package design

import (
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// toolbar is the row above the galleries: title, renderer warning, canvas
// window button and the Artifacts / Templates switch.
func (v *View) toolbar(c *ui.Context, t *kit.Tokens) {
	s := v.s
	ui.Row(c).Height(44).Shrink(0).Padding(0, 14).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		kit.Icon(c, kit.IconDesign).FontSize(15).TextColor(t.FgMuted)
		ui.Text(c, v.a.T("design.title")).FontSize(t.TextBase).FontWeight(600)
		if s.status != nil && !s.status.Renderer {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				kit.Icon(c, kit.IconWarning).FontSize(12).TextColor(t.Warning)
				ui.Text(c, v.a.T("design.noRenderer")).FontSize(t.TextXS).TextColor(t.Warning).SingleLine().Ellipsis("…")
			})
		}
		ui.Spacer(c)
		if kit.Button(c.Key("canvas"), kit.ButtonOpts{Size: kit.SizeSM, Icon: "layout-grid",
			Label: v.a.T("design.canvasWindow"), Tooltip: v.a.T("design.openCanvas")}).Clicked() {
			v.openCanvasWindow()
		}
		ui.Segmented(c, &v.tab, v.a.T("design.tab.artifacts"), v.a.T("design.tab.templates")).Label(v.a.T("design.title"))
	})
}

// gallery is the landing view: artifacts or templates.
func (v *View) gallery(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		v.toolbar(c, t)
		if v.tab == 1 {
			v.templates(c, t)
			return
		}
		v.artifacts(c, t)
	})
}

func (v *View) artifacts(c *ui.Context, t *kit.Tokens) {
	s := v.s
	if s.loading && len(s.artifacts) == 0 {
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(8).Children(func() {
			kit.Spinner(c).TextColor(t.FgFaint)
			ui.Text(c, v.a.T("design.gallery.loading")).TextColor(t.FgMuted).FontSize(t.TextSM)
		})
		return
	}
	if len(s.artifacts) == 0 {
		kit.EmptyState(c, kit.IconDesign, v.a.T("design.gallery.emptyTitle"), v.a.T("design.gallery.emptyBody"))
		return
	}
	ui.GridView(c, &v.artGrid, len(s.artifacts), 260, 252, func(i int) {
		v.artifactCard(c.Key(s.artifacts[i].ID), t, s.artifacts[i])
	}).Grow(1).Padding(14).Gap(14)
}

func (v *View) artifactCard(c *ui.Context, t *kit.Tokens, art client.DesignArtifact) {
	s := v.s
	card := ui.Column(c).Grow(1).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).Clip().
		Cursor(ui.CursorPointer).Focusable().Label(art.Title)
	if card.Clicked() {
		v.a.Navigate("/design/" + art.ID)
	}
	if card.Hovered() {
		card.BorderColor(t.BorderStrong)
	}
	card.Children(func() {
		th := s.thumbFor(art)
		ui.Row(c).Height(130).Shrink(0).AlignSelf(ui.Stretch).Background(t.BgRaised).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			if th.bmp != nil {
				ui.Image(c, th.bmp).Fit(ui.Cover).Grow(1).Height(130)
			} else if !th.failed {
				kit.Spinner(c).TextColor(t.FgFaint)
			} else {
				kit.Icon(c, kit.IconDesign).FontSize(24).TextColor(t.FgFaint)
			}
		})
		ui.Column(c).Padding(10, 12).Gap(3).Children(func() {
			ui.Text(c, art.Title).FontSize(t.TextSM).FontWeight(600).SingleLine().Ellipsis("…")
			meta := v.a.T("design.kind." + art.Kind)
			meta += " · v" + itoa(art.CurrentVersion)
			if art.Slides > 0 {
				meta += " · " + v.a.T("design.deck.count", "count", art.Slides)
			}
			ui.Text(c, meta).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
			ui.Text(c, art.Dir).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine().Ellipsis("…").Font(t.MonoFont)
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, safeDate(art.CreatedAt, "2006-01-02")).FontSize(t.TextXS).TextColor(t.FgFaint).Grow(1)
				if art.URL != "" {
					if kit.IconButton(c.Key("ext-"+art.ID), kit.IconExternal, v.a.T("design.openExternal")).Clicked() {
						_ = mygo.Shell.OpenExternal(rebase(art.URL, v.a.Client.BaseURL()))
					}
				}
			})
		})
	})
}

// templates is the template gallery. "Try it" hands the starter brief to
// the chat composer: a template is only half the input.
func (v *View) templates(c *ui.Context, t *kit.Tokens) {
	s := v.s
	if !s.templatesLoaded && !s.templatesBusy {
		s.fetchTemplates()
	}
	if s.templatesBusy && len(s.templates) == 0 {
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(8).Children(func() {
			kit.Spinner(c).TextColor(t.FgFaint)
			ui.Text(c, v.a.T("design.templates.loading")).TextColor(t.FgMuted).FontSize(t.TextSM)
		})
		return
	}
	if len(s.templates) == 0 {
		kit.EmptyState(c, "sparkles", v.a.T("design.templates.empty"), "")
		return
	}
	var startable, others []client.DesignTemplate
	for _, tp := range s.templates {
		if tp.Startable {
			startable = append(startable, tp)
		} else {
			others = append(others, tp)
		}
	}
	ui.Scroll(c).TrackScroll(&v.tplScroll).Grow(1).Padding(16).Gap(14).Children(func() {
		ui.Text(c, v.a.T("design.templates.hint")).FontSize(t.TextSM).TextColor(t.FgMuted).MaxWidth(760)
		ui.Row(c).Wrap().Gap(12).Children(func() {
			for _, tp := range startable {
				v.templateCard(c.Key("tpl-"+tp.Name), t, tp)
			}
		})
		if len(others) > 0 {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, v.a.T("design.templates.workflows")).FontSize(t.TextSM).FontWeight(600)
				for _, tp := range others {
					ui.Text(c, tp.Name+" — "+tp.Description).FontSize(t.TextXS).TextColor(t.FgMuted)
				}
			})
		}
		if len(s.craft) > 0 {
			ui.Text(c, v.a.T("design.templates.craft")+": "+join(s.craft)).FontSize(t.TextXS).TextColor(t.FgFaint)
		}
	})
}

func (v *View) templateCard(c *ui.Context, t *kit.Tokens, tp client.DesignTemplate) {
	ui.Column(c).Width(300).Gap(8).Padding(14).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, tp.Name).FontSize(t.TextSM).FontWeight(600).Grow(1).SingleLine().Ellipsis("…")
			if tp.Kind != "" {
				kit.Badge(c, tp.Kind, kit.ToneNeutral)
			}
		})
		ui.Text(c, tp.Description).FontSize(t.TextXS).TextColor(t.FgMuted)
		if tp.RequiresSystem {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				kit.Icon(c, kit.IconDesign).FontSize(12).TextColor(t.Warning)
				ui.Text(c, v.a.T("design.templates.needsSystem")).FontSize(t.TextXS).TextColor(t.Warning)
			})
		}
		if tp.ExamplePrompt != "" {
			ui.Text(c, tp.ExamplePrompt).FontSize(t.TextXS).TextColor(t.FgFaint).Italic()
		}
		ui.Row(c).Gap(8).Children(func() {
			if kit.Button(c.Key("try"), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Label: v.a.T("design.templates.tryIt")}).Clicked() {
				prompt := "Use the " + tp.Name + " design template."
				if tp.ExamplePrompt != "" {
					prompt += " " + tp.ExamplePrompt
				}
				v.a.InsertIntoChatDraft(prompt)
				v.a.Navigate("/chat")
			}
			label, icon := v.a.T("design.templates.install"), "download"
			if tp.Installed {
				label, icon = v.a.T("design.templates.installed"), kit.IconCheck
			}
			if kit.Button(c.Key("install"), kit.ButtonOpts{Size: kit.SizeSM, Icon: icon, Label: label,
				Disabled: tp.Installed, Tooltip: v.a.T("design.templates.installHint")}).Clicked() {
				v.s.installTemplate(tp.Name)
			}
		})
	})
}

func join(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
