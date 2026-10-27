package settings

import (
	"slices"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// tagList edits a list of strings as removable chips plus an entry that adds
// on Enter (the Web UI's TagListEditor). add is the pending entry.
func tagList(c *ui.Context, items *[]string, add *string, placeholder string) ui.Element {
	t := kit.Current()
	col := ui.Column(c).Gap(8)
	col.Children(func() {
		if len(*items) > 0 {
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for i, it := range *items {
					cc := c.Key("chip:" + it)
					ui.Row(cc).Gap(4).Padding(2, 4, 2, 10).AlignItems(ui.Center).Radius(t.RadiusSM).
						Background(t.BgCard).Border(1, t.BorderStrong).Children(func() {
						ui.Text(cc, it).FontSize(t.TextSM)
						if kit.IconButton(cc.Key("rm"), kit.IconClose, "Remove").Clicked() {
							*items = append(append([]string{}, (*items)[:i]...), (*items)[i+1:]...)
						}
					})
				}
			})
		}
		ui.Row(c).Gap(8).Children(func() {
			in := kit.Input(c, add, placeholder).Width(260)
			submit := in.Submitted()
			if kit.Button(c.Key("add"), kit.ButtonOpts{Label: "Add", Size: kit.SizeSM, Disabled: strings.TrimSpace(*add) == ""}).Clicked() || submit {
				v := strings.TrimSpace(*add)
				if v != "" && !slices.Contains(*items, v) {
					*items = append(append([]string{}, *items...), v)
				}
				*add = ""
			}
		})
	})
	return col
}

// pageHeader draws the title and description of a section.
func (s *Settings) pageHeader(c *ui.Context, title, desc string) {
	t := kit.Current()
	ui.Column(c).Gap(4).Margin(0, 0, 8, 0).Children(func() {
		ui.Text(c, title).FontSize(t.TextXL).FontWeight(600)
		if desc != "" {
			ui.Text(c, desc).FontSize(t.TextSM).TextColor(t.FgMuted)
		}
	})
}

// t translates key, or returns the literal fallback when the catalogs lack
// it (the Web UI hard-codes English in some sections).
func (s *Settings) t(fallback, key string) string {
	if key != "" {
		if v := s.app.T(key); v != key {
			return v
		}
	}
	return fallback
}

func (s *Settings) loadingView(c *ui.Context, st *status) {
	t := kit.Current()
	if st.err != "" {
		s.errorBanner(c, st.err)
		return
	}
	ui.Row(c).Gap(8).Padding(16).AlignItems(ui.Center).Children(func() {
		kit.Spinner(c)
		ui.Text(c, s.app.T("common.loading")).TextColor(t.FgMuted)
	})
}

// toList splits comma separated input.
func toList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
