package overlays

import (
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// badge is a small tag at the end of a picker row.
type badge struct {
	text string
	tone kit.Tone
}

// pickRow is a row of a picker. Rows with equal Group must be adjacent.
type pickRow struct {
	ID     string
	Group  string
	Icon   string
	Title  string
	Desc   string
	Meta   string
	Active bool
	Badges []badge
	// Search is the extra text the query matches (keywords).
	Search string
	Run    func()
}

func (r pickRow) matches(q string) bool {
	if q == "" {
		return true
	}
	for _, s := range []string{r.Title, r.Desc, r.Search, r.Group} {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

// picker is the search field + grouped list both overlays share (the web's
// ovl-search / ovl-list): type to filter, Up/Down to move, Enter to run,
// click to run, Escape closes (the dialog does).
type picker struct {
	query  string
	sel    int
	reveal bool // scroll the selected row into view next frame
}

func (p *picker) reset() { *p = picker{} }

// filter returns the rows matching the query.
func (p *picker) filter(rows []pickRow) []pickRow {
	q := strings.ToLower(strings.TrimSpace(p.query))
	out := make([]pickRow, 0, len(rows))
	for _, r := range rows {
		if r.matches(q) {
			out = append(out, r)
		}
	}
	return out
}

func (p *picker) move(d, n int) {
	if n == 0 {
		p.sel = 0
		return
	}
	p.sel = max(0, min(p.sel+d, n-1))
	p.reveal = true
}

// build draws the search input and the list; run is called with the chosen
// row (Enter or click).
func (p *picker) build(c *ui.Context, placeholder, empty string, rows []pickRow, height float32, run func(pickRow)) {
	t := kit.Current()
	shown := p.filter(rows)
	in := kit.Input(c.Key("pick-q"), &p.query, placeholder).AutoFocus()
	if in.Changed() {
		p.sel = 0
	}
	in.OnShortcut(0, ui.KeyDown, func() { p.move(1, len(shown)) })
	in.OnShortcut(0, ui.KeyUp, func() { p.move(-1, len(shown)) })
	if in.Submitted() && len(shown) > 0 {
		run(shown[max(0, min(p.sel, len(shown)-1))])
		return
	}
	ui.Scroll(c.Key("pick-list")).Height(height).Gap(1).Children(func() {
		if len(shown) == 0 {
			ui.Text(c, empty).FontSize(t.TextSM).TextColor(t.FgMuted).Padding(16, 8)
			return
		}
		group := "\x00"
		for i, r := range shown {
			if r.Group != group {
				group = r.Group
				if group != "" {
					kit.SectionLabel(c, group)
				}
			}
			if p.row(c, t, i, r) {
				run(r)
				return
			}
		}
	})
}

// row draws one row and reports a click.
func (p *picker) row(c *ui.Context, t *kit.Tokens, i int, r pickRow) bool {
	selected := i == p.sel
	b := ui.ButtonBase(c.Key("pick:"+r.ID)).Label(r.Title).MinHeight(34).Gap(10).Padding(6, 10).Radius(t.RadiusSM).
		AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer)
	switch {
	case selected:
		b.Background(t.AccentSoft)
	case b.Hovered():
		b.Background(t.BgHover)
	}
	b.Children(func() {
		if r.Icon != "" {
			ic := kit.Icon(c, r.Icon).FontSize(15)
			if r.Active {
				ic.TextColor(t.Accent)
			} else {
				ic.TextColor(t.FgMuted)
			}
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			title := ui.Text(c, r.Title).FontSize(t.TextSM).SingleLine().Ellipsis("…")
			if r.Active {
				title.FontWeight(600).TextColor(t.Accent)
			} else {
				title.TextColor(t.Fg)
			}
			if r.Desc != "" {
				ui.Text(c, r.Desc).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
			}
			if r.Meta != "" {
				ui.Text(c, r.Meta).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine().Ellipsis("…")
			}
		})
		for _, bd := range r.Badges {
			kit.Badge(c, bd.text, bd.tone)
		}
	})
	if selected && p.reveal {
		b.ScrollIntoView()
		p.reveal = false
	}
	return b.Clicked()
}
