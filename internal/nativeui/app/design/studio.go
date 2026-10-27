package design

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// studio is the workspace of one artifact: toolbar, the canvas (embedded web
// view) with the deck slide strip, and the side column with the inspector and
// the version timeline. The chat lives in its own area: the toolbar jumps to
// the artifact's session, and "Ask about this element" hands the selection to
// the chat composer.
func (v *View) studio(c *ui.Context, t *kit.Tokens, art client.DesignArtifact) {
	s := v.s
	if v.cv == nil {
		v.cv = newCanvas(s)
	}
	ui.Column(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		v.studioToolbar(c, t, art)
		ui.Row(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Padding(8).Gap(8).Children(func() {
				if s.sel != nil {
					v.selectionBanner(c, t)
				}
				v.cv.Build(c, art, v.previewEmptyMessage(art))
				if art.Kind == "deck" {
					v.slideStrip(c, t, art.Slides)
				}
			})
			if !v.sideHidden {
				ui.Column(c).Width(320).Shrink(0).AlignItems(ui.Stretch).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Children(func() {
					v.side(c, t, art)
				})
			}
		})
	})
}

func (v *View) studioToolbar(c *ui.Context, t *kit.Tokens, art client.DesignArtifact) {
	s := v.s
	ui.Row(c).Height(46).Shrink(0).Padding(0, 12).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		if kit.IconButton(c.Key("back"), "arrow-left", v.a.T("design.backToGallery")).Clicked() {
			v.a.Navigate("/design")
		}
		ui.Text(c, art.Title).FontSize(t.TextBase).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
		ui.Text(c, v.a.T("design.kind."+art.Kind)+" · v"+itoa(art.CurrentVersion)).
			FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine()
		ui.Spacer(c)
		render := kit.Button(c.Key("render"), kit.ButtonOpts{Size: kit.SizeSM, Icon: kit.IconRefresh,
			Label: v.a.T("design.render"), Tooltip: v.a.T("design.renderHint"), Disabled: s.rendering})
		if render.Clicked() {
			s.render(art.ID)
		}
		if kit.Button(c.Key("chat"), kit.ButtonOpts{Size: kit.SizeSM, Icon: kit.IconChat, Label: v.a.T("design.pane.chat")}).Clicked() {
			if art.SessionID != "" {
				v.a.Navigate("/chat/s/" + art.SessionID)
			} else {
				v.a.Navigate("/chat")
			}
		}
		if kit.Button(c.Key("canvas"), kit.ButtonOpts{Size: kit.SizeSM, Icon: "layout-grid",
			Label: v.a.T("design.canvasWindow"), Tooltip: v.a.T("design.openCanvas")}).Clicked() {
			v.openCanvasWindow()
		}
		if art.URL != "" {
			if kit.IconButton(c.Key("ext"), kit.IconExternal, v.a.T("design.openExternal")).Clicked() {
				_ = mygo.Shell.OpenExternal(rebase(art.URL, v.a.Client.BaseURL()))
			}
		}
		exp := kit.Button(c.Key("export"), kit.ButtonOpts{Size: kit.SizeSM, Icon: "upload",
			Label: v.a.T("design.export.label"), Disabled: s.exporting})
		exp.Menu(func(m *ui.Menu) {
			for _, f := range []string{"html", "png", "pdf"} {
				if m.Item(v.a.T("design.export." + f)).Chosen() {
					slide := 0
					if f == "png" {
						slide = s.slide
					}
					v.export(art.ID, f, slide)
				}
			}
		})
		if kit.IconButton(c.Key("side"), kit.IconPanelRight, v.a.T("design.inspector.title")).Clicked() {
			v.sideHidden = !v.sideHidden
		}
	})
}

func (v *View) selectionBanner(c *ui.Context, t *kit.Tokens) {
	sel := v.s.sel
	ui.Row(c).Gap(8).Padding(5, 10).Radius(t.RadiusSM).Background(t.AccentSoft).AlignItems(ui.Center).Shrink(0).Children(func() {
		kit.Icon(c, "crosshair").FontSize(12).TextColor(t.Accent)
		ui.Text(c, sel.Selection).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.Accent).SingleLine()
		if sel.Text != "" {
			ui.Text(c, sel.Text).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
		}
	})
}

// slideStrip is deck mode's navigation: one button per slide (numbers, not
// thumbnails: a screenshot per slide on every render would be the most
// expensive thing a deck under iteration could do).
func (v *View) slideStrip(c *ui.Context, t *kit.Tokens, slides int) {
	if slides <= 0 {
		return
	}
	s := v.s
	ui.Row(c).Gap(4).Padding(4, 8).AlignItems(ui.Center).Shrink(0).Wrap().Children(func() {
		ui.Text(c, v.a.T("design.deck.slides")).FontSize(t.TextXS).TextColor(t.FgMuted).Margin(0, 6, 0, 0)
		for n := 1; n <= slides; n++ {
			if kit.Button(c.Key(fmt.Sprintf("slide-%d", n)), kit.ButtonOpts{Size: kit.SizeSM, Label: itoa(n),
				Variant: kit.Ghost, Active: s.slide == n}).Clicked() {
				s.slide = n
			}
		}
	})
}

func (v *View) previewEmptyMessage(art client.DesignArtifact) string {
	st := v.s.status
	if st != nil && !st.Preview && st.PreviewReason != "" {
		return st.PreviewReason
	}
	if art.FileURL != "" && art.BridgeURL == "" {
		return v.a.T("design.canvas.noPreviewServer")
	}
	return v.a.T("design.canvas.notRenderedYet")
}

// side is the right column: Inspector / Versions.
func (v *View) side(c *ui.Context, t *kit.Tokens, art client.DesignArtifact) {
	ui.Row(c).Shrink(0).Padding(6, 8, 0, 8).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Tabs(c, &v.sideTab, v.a.T("design.inspector.title"), v.a.T("design.versions.title"))
	})
	if v.sideTab == 1 {
		v.versions(c, t, art)
		return
	}
	v.inspector(c, t, art)
}

// openCanvasWindow opens the read-only canvas (every artifact as an artboard
// on one pan-and-zoom surface) in a native window of its own.
func (v *View) openCanvasWindow() {
	v.a.Go(func(ctx context.Context) {
		url, _, err := v.a.Client.DesignCanvasURL(ctx)
		v.a.Update(func() {
			if err != nil || url == "" {
				msg := "The design canvas is not available in this process."
				if err != nil {
					msg = err.Error()
				}
				v.a.Toasts.Error(v.a.T("design.title"), msg)
				return
			}
			kit.OpenWebWindow(v.a.T("design.canvasWindow"), rebase(url, v.a.Client.BaseURL()),
				func(c *ui.Context) { kit.Apply(c, v.a.Theme) })
		})
	})
}

// export writes the export on the server and then saves the file to the
// Downloads folder (a second request on purpose: an export can be a
// multi-megabyte PDF).
func (v *View) export(id, format string, slide int) {
	s := v.s
	if s.exporting {
		return
	}
	s.exporting = true
	v.a.Go(func(ctx context.Context) {
		res, err := v.a.Client.DesignExportArtifact(ctx, id, format, slide)
		var saved string
		if err == nil && res.DownloadURL != "" {
			var data []byte
			if data, err = v.a.Client.DesignDownload(ctx, res.DownloadURL); err == nil {
				saved, err = saveDownload(id+"."+format, data)
			}
		}
		v.a.Update(func() {
			s.exporting = false
			switch {
			case err != nil:
				v.a.Toasts.Error("Export failed", err.Error())
			case res.Note != "":
				v.a.Toasts.Info("Exported "+format, res.Note+" "+saved)
			default:
				v.a.Toasts.Success("Exported "+format, saved)
			}
		})
	})
}

// saveDownload writes data to the user's Downloads folder (or the home
// directory) without overwriting an existing file.
func saveDownload(name string, data []byte) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Downloads")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = home
	}
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	path := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
	return path, os.WriteFile(path, data, 0o644)
}
