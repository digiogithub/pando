package agentvcs

import (
	"context"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// diffView is the full-area viewer of one changed file: the old and new
// versions side by side (MyGo DiffView), read-only.
type diffView struct {
	entry    client.VCSDiffEntry
	commitID string
	loading  bool
	err      string
	state    *editor.DiffState
	split    bool
}

func (v *View) openDiff(commitID string, e client.VCSDiffEntry) {
	d := &diffView{entry: e, commitID: commitID, loading: true, split: true}
	v.diff = d
	v.app.Go(func(ctx context.Context) {
		var oldText, newText string
		var err error
		if e.OldHash != "" {
			oldText, err = v.app.Client.VCSBlob(ctx, e.OldHash)
		}
		if err == nil && e.NewHash != "" {
			newText, err = v.app.Client.VCSBlob(ctx, e.NewHash)
		}
		v.app.Update(func() {
			if v.diff != d {
				return
			}
			d.loading = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.state = newDiffState(e.Path, oldText, newText)
		})
	})
}

func newDiffState(file, oldText, newText string) *editor.DiffState {
	lang := editor.LanguageFor(file)
	ob, nb := editor.NewBuffer(oldText), editor.NewBuffer(newText)
	ob.SetLanguage(lang)
	nb.SetLanguage(lang)
	return &editor.DiffState{Old: ob, New: nb, Layout: editor.DiffSplit}
}

func (d *diffView) build(c *ui.Context, t *kit.Tokens, v *View) {
	if c.Shortcut(0, ui.KeyEscape) {
		v.diff = nil
		return
	}
	ui.Row(c).Padding(8, 16).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		kit.Icon(c, iconCode).FontSize(15).TextColor(t.FgMuted)
		ui.Text(c, d.entry.Path).Font("monospace").FontSize(t.TextSM).SingleLine().Ellipsis("…").Shrink(1)
		kit.Badge(c, diffTypeLabel(d.entry.Type), diffTone(d.entry.Type))
		ui.Text(c, "commit "+short(d.commitID, 12)).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine()
		ui.Box(c).Grow(1)
		if d.state != nil {
			a, del := d.state.Stats()
			ui.Text(c, "+"+itoa(a)).FontSize(t.TextXS).TextColor(t.Success)
			ui.Text(c, "−"+itoa(del)).FontSize(t.TextXS).TextColor(t.Danger)
			seg := 0
			if !d.split {
				seg = 1
			}
			if ui.Segmented(c.Key("vcs:layout"), &seg, "Split", "Unified").Label("Layout").Changed() {
				d.split = seg == 0
				if d.split {
					d.state.Layout = editor.DiffSplit
				} else {
					d.state.Layout = editor.DiffUnified
				}
			}
		}
		if kit.Button(c.Key("vcs:closediff"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: kit.IconClose, Tooltip: "Close diff viewer (Esc)"}).Clicked() {
			v.diff = nil
		}
	})
	switch {
	case d.loading:
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(8).Children(func() {
			kit.Spinner(c).FontSize(22).TextColor(t.FgFaint)
			ui.Text(c, "Loading file content…").TextColor(t.FgMuted)
		})
	case d.err != "":
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, d.err).TextColor(t.Danger)
		})
	default:
		editor.DiffView(c.Key("vcs:diff"), d.state).Grow(1).FillWidth().MinHeight(0)
	}
	ui.Row(c).Padding(4, 16).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Background(t.BgShell).Children(func() {
		ui.Text(c, "Press Esc or click X to close").FontSize(t.TextXS).TextColor(t.FgFaint)
	})
}

func diffTypeLabel(t string) string {
	switch t {
	case "added":
		return "ADDED"
	case "deleted":
		return "DELETED"
	}
	return "MODIFIED"
}

func itoa(n int) string { return fmtInt(n) }
