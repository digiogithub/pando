package settings

import (
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Helpers shared by the list/dialog style sections (MCP servers, LSP,
// skills). Prefixed "cr" to stay clear of other sections' identifiers.

// crField is a label above a control, as the web .settings-field.
func crField(c *ui.Context, label string, control func()) {
	t := kit.Current()
	ui.Column(c).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(t.TextSM).FontWeight(500)
		control()
	})
}

// crSelect is a select over (value, label) options bound to *value.
func crSelect(c *ui.Context, value *string, opts [][2]string) ui.Element {
	cur := *value
	label := cur
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o[1]
		if o[0] == cur {
			label = o[1]
		}
	}
	el := kit.Select(c, &label, labels)
	for _, o := range opts {
		if o[1] == label {
			*value = o[0]
		}
	}
	return el
}

type crKV struct{ k, v string }

// crKVEditor edits key/value pairs (env vars, headers).
func crKVEditor(c *ui.Context, label string, pairs *[]crKV, kph, vph string) {
	t := kit.Current()
	crField(c, label, func() {
		ui.Column(c).Gap(6).Children(func() {
			for i := range *pairs {
				cc := c.Key("kv:" + label + string(rune('a'+i%26)) + string(rune('a'+i/26)))
				p := &(*pairs)[i]
				ui.Row(cc).Gap(6).AlignItems(ui.Center).Children(func() {
					kit.Input(cc.Key("k"), &p.k, kph).Width(180)
					kit.Input(cc.Key("v"), &p.v, vph).Grow(1)
					if kit.IconButton(cc.Key("rm"), kit.IconTrash, "Remove").Clicked() {
						*pairs = append((*pairs)[:i:i], (*pairs)[i+1:]...)
					}
				})
			}
			if kit.Button(c.Key("kvadd:"+label), kit.ButtonOpts{Label: "Add", Icon: kit.IconPlus, Size: kit.SizeSM, Variant: kit.Secondary}).Clicked() {
				*pairs = append(*pairs, crKV{})
			}
		})
	})
	_ = t
}

// crConfirm is a confirm dialog; ok runs when the user confirms.
func crConfirm(c *ui.Context, open *bool, title, msg, confirm string, ok func()) {
	kit.Dialog(c, open, kit.DialogOpts{
		Title: title, Description: msg,
		Footer: func() {
			if kit.SecondaryButton(c.Key("cf-cancel"), "Cancel").Clicked() {
				*open = false
			}
			if kit.Button(c.Key("cf-ok"), kit.ButtonOpts{Label: confirm, Variant: kit.Danger}).Clicked() {
				*open = false
				ok()
			}
		},
	})
}

// crHeaderRow draws a section page header with a primary action button.
func (s *Settings) crHeader(c *ui.Context, title, desc, action string, onAction func()) {
	t := kit.Current()
	ui.Row(c).Gap(12).AlignItems(ui.Center).Margin(0, 0, 8, 0).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			ui.Text(c, title).FontSize(t.TextXL).FontWeight(600)
			if desc != "" {
				ui.Text(c, desc).FontSize(t.TextSM).TextColor(t.FgMuted)
			}
		})
		if action != "" {
			if kit.Button(c.Key("hdr-action"), kit.ButtonOpts{Label: action, Icon: kit.IconPlus, Variant: kit.Primary}).Clicked() {
				onAction()
			}
		}
	})
}

// crListCard wraps list rows in the bordered card of the settings lists.
func crListCard(c *ui.Context, rows func()) {
	t := kit.Current()
	ui.Column(c).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).Dividers(1, t.Border).Children(rows)
}

func crJoinSpace(l []string) string { return strings.Join(l, " ") }
