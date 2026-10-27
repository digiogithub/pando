package projects

import (
	"context"
	"path"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// dirBrowser is the "select directory" dialog (web DirBrowserDialog) over
// GET /fs/browse.
type dirBrowser struct {
	a       *app.Context
	open    bool
	loading bool
	err     string
	list    client.DirListing
	// onSelect receives the chosen directory.
	onSelect func(string)
}

func newDirBrowser(a *app.Context) *dirBrowser { return &dirBrowser{a: a} }

// Show opens the dialog at start ("~" when empty).
func (d *dirBrowser) Show(start string, onSelect func(string)) {
	if start == "" {
		start = "~"
	}
	d.open, d.onSelect = true, onSelect
	d.go_(start)
}

func (d *dirBrowser) go_(p string) {
	d.loading, d.err = true, ""
	d.a.Go(func(ctx context.Context) {
		l, err := d.a.Client.BrowseDirectories(ctx, p)
		d.a.Update(func() {
			d.loading = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.list = l
		})
	})
}

func (d *dirBrowser) Build(c *ui.Context) {
	if !d.open {
		return
	}
	tk := kit.Current()
	kit.Dialog(c.Key("dlg-dirs"), &d.open, kit.DialogOpts{
		Title: "Select Directory", Width: 480,
		Body: func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if d.list.Parent != "" && kit.IconButton(c.Key("dir-up"), kit.IconArrowUp, "Go up").Clicked() {
					d.go_(d.list.Parent)
				}
				ui.Text(c, d.list.Path).FontSize(tk.TextXS).SingleLine().Ellipsis("…").Grow(1).Shrink(1).
					Padding(6, 10).Radius(tk.RadiusSM).Background(tk.BgInput).Border(1, tk.Border)
			})
			ui.Column(c).Height(260).Radius(tk.RadiusMD).Border(1, tk.Border).Children(func() {
				switch {
				case d.loading:
					ui.Row(c).Padding(24).Gap(8).Justify(ui.Center).Children(func() { kit.Spinner(c); ui.Text(c, "Loading…") })
				case d.err != "":
					ui.Text(c, d.err).TextColor(tk.Danger).FontSize(tk.TextSM).Padding(12)
				case len(d.list.Dirs) == 0:
					ui.Text(c, "No subdirectories").TextColor(tk.FgMuted).FontSize(tk.TextSM).Padding(24).TextAlign(ui.Center)
				default:
					ui.Scroll(c).Grow(1).Children(func() {
						for _, dir := range d.list.Dirs {
							b := ui.ButtonBase(c.Key("dir:"+dir)).Label(dir).Height(28).Padding(0, 10).Gap(8).
								AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer)
							if b.Hovered() {
								b.Background(tk.BgHover)
							}
							b.Children(func() {
								kit.Icon(c, kit.IconFolder).FontSize(14).TextColor(tk.FgMuted)
								ui.Text(c, dir).FontSize(tk.TextSM).SingleLine().Ellipsis("…")
							})
							if b.Clicked() {
								d.go_(path.Join(d.list.Path, dir))
							}
						}
					})
				}
			})
		},
		Footer: func() {
			if kit.Button(c.Key("dir-cancel"), kit.ButtonOpts{Label: d.a.T("projects.view.common.cancel")}).Clicked() {
				d.open = false
			}
			if kit.Button(c.Key("dir-ok"), kit.ButtonOpts{Variant: kit.Primary, Icon: kit.IconFolder, Label: "Select", Disabled: d.list.Path == ""}).Clicked() {
				d.open = false
				if d.onSelect != nil {
					d.onSelect(d.list.Path)
				}
			}
		},
	})
}
