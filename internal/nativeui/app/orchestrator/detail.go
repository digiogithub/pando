package orchestrator

import (
	"encoding/json"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const maxShown = 20000

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func fieldLabel(c *ui.Context, t *kit.Tokens, s string) {
	ui.Text(c, s).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgMuted).Margin(0, 0, 4, 0)
}

func metaRow(c *ui.Context, t *kit.Tokens, icon, label, value string, mono bool) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		kit.Icon(c, icon).FontSize(13).TextColor(t.FgMuted)
		ui.Text(c, label+":").FontSize(t.TextSM).TextColor(t.FgMuted)
		tx := ui.Text(c, value).FontSize(t.TextSM).FontWeight(500).Shrink(1)
		if mono {
			tx.Font(t.MonoFont)
		}
	})
}

// Detail panel width bounds in DIPs: the pointer drags its left edge.
const (
	detailMinW     = 320
	detailDefaultW = 400
)

// detailWidth clamps the panel width to the room the window leaves the
// task table.
func (v *View) detailWidth(avail float32) float32 {
	if v.detailW == 0 {
		v.detailW = detailDefaultW
	}
	maxW := max(avail-360, detailMinW)
	return min(max(v.detailW, detailMinW), maxW)
}

func (v *View) detail(c *ui.Context, t *kit.Tokens, task client.OrchestratorTask) {
	winW, _ := c.Size()
	w := v.detailWidth(winW)
	ui.Column(c).Width(w).Shrink(0).AlignItems(ui.Stretch).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).
		Background(t.BgShell).Children(func() {
		ui.Row(c).Height(44).Padding(0, 16).AlignItems(ui.Center).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.Text(c, "Task Detail").FontWeight(600).Grow(1)
			if kit.IconButton(c, kit.IconClose, "Close detail").Clicked() {
				v.selID, v.selIdx = "", -1
			}
		})
		ui.Scroll(c).Grow(1).Padding(16).Gap(14).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Text(c, task.Name).FontSize(t.TextMD).FontWeight(600)
				kit.Badge(c, task.Status, statusTone(task.Status))
			})
			if task.Prompt != "" {
				ui.Column(c).Children(func() {
					fieldLabel(c, t, "Prompt")
					ui.Text(c, tail(task.Prompt, maxShown)).FontSize(t.TextXS).Padding(10, 12).
						Background(t.AccentSoft).BorderWidth(0, 0, 0, 2).BorderColor(t.Accent).Radius(t.RadiusSM)
				})
			}
			ui.Column(c).Gap(6).Children(func() {
				metaRow(c, t, kit.IconBot, "Agent", task.Agent, false)
				metaRow(c, t, kit.IconTool, "Model", task.Model, true)
				if task.Persona != "" {
					metaRow(c, t, kit.IconUser, "Persona", task.Persona, false)
				}
				metaRow(c, t, kit.IconClock, "Created", fmtTime(task.CreatedAt), false)
				if task.CurrentTool != "" {
					metaRow(c, t, kit.IconTool, "Current tool", task.CurrentTool, false)
				}
			})
			ui.Column(c).Gap(4).Children(func() {
				ui.Row(c).Children(func() {
					ui.Text(c, "Progress").FontSize(t.TextXS).TextColor(t.FgMuted).Grow(1)
					ui.Text(c, itoa(task.Progress)+"%").FontSize(t.TextXS).TextColor(t.FgMuted)
				})
				ui.Progress(c, float64(task.Progress)/100)
			})
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, "Tokens used").FontSize(t.TextXS).TextColor(t.FgMuted)
				ui.Text(c, groupInt(task.Tokens)).FontSize(t.TextLG).FontWeight(600).Font(t.MonoFont)
			})
			if task.Error != "" {
				ui.Text(c, task.Error).FontSize(t.TextSM).TextColor(t.Danger).Padding(8, 12).
					Background(t.DangerSoft).Radius(t.RadiusSM)
			}
			if len(task.ToolCalls) > 0 {
				ui.Column(c).Gap(8).Children(func() {
					fieldLabel(c, t, "Tool Calls")
					for i, tc := range task.ToolCalls {
						v.toolCall(c, t, task.ID, i, tc)
					}
				})
			}
			if task.Output != "" {
				ui.Column(c).Gap(4).Children(func() {
					fieldLabel(c, t, "Output")
					kit.CodeBlock(c.Key("out:"+task.ID), "", tail(task.Output, maxShown), nil)
				})
			}
		})
		// The left edge, built last to sit on top, resizes the panel.
		edge := ui.Box(c.Key("resize")).Absolute().Left(-3).Top(0).Bottom(0).Width(6).Cursor(ui.CursorResizeEW).Label("Resize detail panel")
		if dx, _, ok := edge.Dragged(); ok && dx != 0 {
			v.detailW = v.detailWidth(winW) - dx
		}
		if edge.Hovered() || edge.Dragging() {
			edge.Background(t.Accent.Alpha(0.35))
		}
	})
}

func itoa(n int) string { return groupInt(n) }

// toolCall is a collapsible card: header always visible, input/result on
// expand (keeps long runs cheap to build).
func (v *View) toolCall(c *ui.Context, t *kit.Tokens, taskID string, i int, tc client.OrchestratorToolCall) {
	key := taskID + ":" + tc.ID
	if tc.ID == "" {
		key = taskID + ":" + itoa(i)
	}
	if v.expanded == nil {
		v.expanded = map[string]bool{}
	}
	open := v.expanded[key]
	title := tc.Title
	if title == "" {
		title = tc.Name
	}
	ui.Column(c.Key("tc:"+key)).Gap(6).Padding(10).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).Children(func() {
		hdr := ui.ButtonBase(c.Key("tch:" + key)).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(title)
		hdr.Children(func() {
			ico := kit.IconChevronR
			if open {
				ico = kit.IconChevronDown
			}
			kit.Icon(c, ico).FontSize(14).TextColor(t.FgMuted)
			ui.Column(c).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, title).FontSize(t.TextSM).FontWeight(600).SingleLine().Ellipsis("…")
				ui.Text(c, tc.Name).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
			})
			kit.Badge(c, tc.Status, statusTone(tc.Status))
		})
		if hdr.Clicked() {
			v.expanded[key] = !open
		}
		if !open {
			return
		}
		if len(tc.Arguments) > 0 {
			b, _ := json.MarshalIndent(tc.Arguments, "", "  ")
			fieldLabel(c, t, "Input")
			kit.CodeBlock(c.Key("in:"+key), "json", tail(string(b), 4000), nil)
		}
		if tc.Result != "" {
			fieldLabel(c, t, "Result")
			kit.CodeBlock(c.Key("res:"+key), "", tail(tc.Result, 4000), nil)
		}
		for _, l := range tc.Locations {
			ui.Text(c, l).FontSize(t.TextXS).Font(t.MonoFont)
		}
	})
}
