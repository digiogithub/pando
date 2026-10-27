package design

import (
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// inspector is the structure index of the render (filtered) with the
// selected node's details, plus the Issues tab (the quality gate).
func (v *View) inspector(c *ui.Context, t *kit.Tokens, art client.DesignArtifact) {
	s := v.s
	issues := ""
	if s.critique != nil && len(s.critique.Issues) > 0 {
		issues = fmt.Sprintf(" (%d)", len(s.critique.Issues))
	}
	ui.Row(c).Shrink(0).Padding(6, 8).Children(func() {
		ui.Segmented(c, &v.inspTab, v.a.T("design.critique.tabs.structure"),
			v.a.T("design.critique.tabs.issues")+issues).Label(v.a.T("design.inspector.title"))
	})
	if v.inspTab == 1 {
		v.issues(c, t, art)
		return
	}
	ui.Column(c).Shrink(0).Padding(4, 10, 8, 10).Gap(6).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, "crosshair").FontSize(12).TextColor(t.FgMuted)
			ui.Text(c, v.a.T("design.inspector.title")).FontSize(t.TextXS).FontWeight(600).Grow(1)
			ui.Text(c, v.a.T("design.inspector.nodeCount", "count", s.nodesTot)).FontSize(t.TextXS).TextColor(t.FgFaint)
		})
		kit.Input(c.Key("node-filter"), &v.filter, v.a.T("design.inspector.filterPlaceholder")).Height(t.ControlHSm)
	})

	if s.sel != nil {
		v.selectionDetail(c, t)
	}

	nodes := v.filteredNodes()
	if len(nodes) == 0 {
		msg := v.a.T("design.inspector.noMatches")
		if s.nodesTot == 0 {
			msg = v.a.T("design.inspector.emptyIndex")
		}
		ui.Text(c, msg).FontSize(t.TextXS).TextColor(t.FgMuted).Padding(12)
		return
	}
	ui.List(c, &v.nodeList, len(nodes), func(i int) {
		v.nodeRow(c, t, art.ID, nodes[i])
	}).Grow(1).Padding(0, 6, 8, 6).Gap(1)
}

func (v *View) filteredNodes() []client.DesignNode {
	needle := strings.ToLower(strings.TrimSpace(v.filter))
	if needle == "" {
		return v.s.nodes
	}
	var out []client.DesignNode
	for _, n := range v.s.nodes {
		if strings.Contains(strings.ToLower(n.Text), needle) ||
			strings.Contains(strings.ToLower(n.Selector), needle) ||
			strings.Contains(strings.ToLower(n.Role), needle) {
			out = append(out, n)
		}
	}
	return out
}

func (v *View) selectedNode() (client.DesignNode, bool) {
	if v.s.sel == nil {
		return client.DesignNode{}, false
	}
	for _, n := range v.s.nodes {
		if n.NodeID == v.s.sel.NodeID {
			return n, true
		}
	}
	return client.DesignNode{}, false
}

func (v *View) selectionDetail(c *ui.Context, t *kit.Tokens) {
	s := v.s
	node, hasNode := v.selectedNode()
	ui.Column(c).Shrink(0).Margin(0, 10, 8, 10).Padding(8, 10).Gap(5).Radius(t.RadiusSM).Background(t.BgCard).Border(1, t.Border).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, s.sel.Selection).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.Accent).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
			if kit.IconButton(c.Key("clear-sel"), kit.IconClose, v.a.T("design.inspector.clearSelection")).Clicked() {
				s.sel = nil
			}
		})
		if hasNode && node.Selector != "" {
			ui.Text(c, node.Selector).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
		}
		text := s.sel.Text
		if hasNode && node.Text != "" {
			text = node.Text
		}
		if text != "" {
			ui.Text(c, "“"+clip(text, 120)+"”").FontSize(t.TextXS).Italic().TextColor(t.FgMuted)
		}
		if kit.Button(c.Key("ask"), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Icon: kit.IconSend,
			Label: v.a.T("design.inspector.askAbout")}).Clicked() {
			label := s.sel.Tag
			if hasNode && node.Selector != "" {
				label = node.Selector
			} else if label == "" {
				label = s.sel.NodeID
			}
			v.a.InsertIntoChatDraft(s.sel.Selection + " (" + label + ")")
			if art, ok := s.artifact(s.activeID); ok && art.SessionID != "" {
				v.a.Navigate("/chat/s/" + art.SessionID)
			} else {
				v.a.Navigate("/chat")
			}
		}
		if hasNode && len(node.Styles) > 0 {
			keys := sortedKeys(node.Styles)
			if len(keys) > 12 {
				keys = keys[:12]
			}
			ui.Column(c).Gap(1).Children(func() {
				for _, k := range keys {
					ui.Row(c).Gap(6).Children(func() {
						ui.Text(c, k+":").FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgFaint)
						ui.Text(c, node.Styles[k]).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgMuted).SingleLine().Ellipsis("…").Shrink(1)
					})
				}
			})
		}
	})
}

func (v *View) nodeRow(c *ui.Context, t *kit.Tokens, artID string, n client.DesignNode) {
	s := v.s
	active := s.sel != nil && s.sel.NodeID == n.NodeID
	role := n.Role
	if role == "" {
		role = "node"
	}
	row := ui.Column(c).Gap(2).Padding(6, 8).Radius(t.RadiusSM).Cursor(ui.CursorPointer).Focusable().Label(role + " " + n.NodeID)
	if row.Clicked() {
		s.sel = &selection{NodeID: n.NodeID, Selection: "design://" + n.NodeID, Tag: n.Role, Text: n.Text, Slide: n.Slide}
		if n.Styles == nil {
			s.fetchNodes(artID, n.NodeID)
		}
	}
	if active {
		row.Background(t.AccentSoft)
	} else if row.Hovered() {
		row.Background(t.BgHover)
	}
	row.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, role).FontSize(t.TextXS).FontWeight(600).SingleLine()
			ui.Text(c, n.NodeID).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgFaint).SingleLine().Ellipsis("…").Shrink(1)
			if n.Slide > 0 {
				ui.Text(c, "· s"+itoa(n.Slide)).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine()
			}
		})
		if n.Text != "" {
			ui.Text(c, n.Text).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
		}
	})
}

// issues is the readable end of the quality gate: the findings of the last
// critic pass, each one selecting the element it is about.
func (v *View) issues(c *ui.Context, t *kit.Tokens, art client.DesignArtifact) {
	s := v.s
	ui.Column(c).Shrink(0).Padding(4, 10, 8, 10).Gap(4).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if s.critique != nil {
				col := t.Warning
				if s.decision != nil && s.decision.Pass {
					col = t.Success
				}
				ui.Text(c, v.a.T("design.critique.score", "score", fmt.Sprintf("%.1f", s.critique.Score))).
					FontSize(t.TextMD).FontWeight(700).TextColor(col)
			} else {
				ui.Text(c, v.a.T("design.critique.title")).FontSize(t.TextSM).FontWeight(600)
			}
			ui.Spacer(c)
			label := v.a.T("design.critique.run")
			if s.critiqueRunning {
				label = v.a.T("design.critique.running")
			}
			if kit.Button(c.Key("critique"), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Icon: kit.IconRefresh,
				Label: label, Disabled: s.critiqueRunning}).Clicked() {
				s.runCritique(art.ID)
			}
		})
		if d := s.decision; d != nil {
			verdict := v.a.T("design.critique.stop")
			if d.Pass {
				verdict = v.a.T("design.critique.pass")
			} else if d.Iterate {
				verdict = v.a.T("design.critique.iterate")
			}
			ui.Text(c, verdict+" · "+v.a.T("design.critique.round", "round", d.Round, "max", d.MaxRounds)).
				FontSize(t.TextXS).TextColor(t.FgMuted)
			if d.Reason != "" {
				ui.Text(c, d.Reason).FontSize(t.TextXS).TextColor(t.FgFaint)
			}
		}
		if cs := s.critiqueSet; cs != nil {
			ui.Text(c, v.a.T("design.critique.policy", "policy", cs.Policy, "threshold", fmt.Sprintf("%.1f", cs.Threshold))).
				FontSize(t.TextXS).TextColor(t.FgFaint)
		}
	})
	switch {
	case s.critique == nil:
		if s.critiqueLoaded {
			ui.Text(c, v.a.T("design.critique.never")).FontSize(t.TextXS).TextColor(t.FgMuted).Padding(12)
		}
	case len(s.critique.Issues) == 0:
		ui.Row(c).Gap(6).Padding(12).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, kit.IconSuccess).FontSize(14).TextColor(t.Success)
			ui.Text(c, v.a.T("design.critique.none")).FontSize(t.TextXS).TextColor(t.Success)
		})
	default:
		iss := s.critique.Issues
		ui.List(c, &v.issueList, len(iss), func(i int) { v.issueRow(c, t, iss[i]) }).Grow(1).Padding(6).Gap(4)
	}
}

func (v *View) issueRow(c *ui.Context, t *kit.Tokens, is client.DesignIssue) {
	s := v.s
	tone, icon := kit.ToneInfo, "circle-question-mark"
	switch is.Severity {
	case "blocking":
		tone, icon = kit.ToneDanger, "gavel"
	case "error":
		tone, icon = kit.ToneDanger, kit.IconError
	case "warning":
		tone, icon = kit.ToneWarning, kit.IconWarning
	}
	fg, bg := kit.ToneColors(tone)
	row := ui.Column(c).Gap(2).Padding(7, 9).Radius(t.RadiusSM).Background(bg).BorderWidth(0, 0, 0, 3).BorderColor(fg)
	if is.NodeID != "" {
		row.Cursor(ui.CursorPointer).Focusable().Label(is.Message).Tooltip(v.a.T("design.critique.select"))
		if row.Clicked() {
			s.sel = &selection{NodeID: is.NodeID, Selection: "design://" + is.NodeID, Slide: is.Slide}
		}
	}
	row.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, icon).FontSize(11).TextColor(fg)
			if is.Code != "" {
				ui.Text(c, is.Code).FontSize(t.TextXS).FontWeight(600).TextColor(fg).SingleLine()
			}
			if is.NodeID != "" {
				ui.Text(c, is.NodeID).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgFaint).SingleLine().Ellipsis("…")
			}
		})
		ui.Text(c, is.Message).FontSize(t.TextXS)
		if is.Fix != "" {
			ui.Text(c, v.a.T("design.critique.fix")+": "+is.Fix).FontSize(t.TextXS).TextColor(t.FgMuted)
		}
	})
}

// versions is the timeline of accepted iterations, newest first. A checkout
// rewrites the artifact directory, which the button says plainly.
func (v *View) versions(c *ui.Context, t *kit.Tokens, art client.DesignArtifact) {
	s := v.s
	if len(s.versions) == 0 {
		ui.Text(c, v.a.T("design.versions.empty")).FontSize(t.TextXS).TextColor(t.FgMuted).Padding(12)
		return
	}
	ui.List(c, &v.verList, len(s.versions), func(i int) {
		ver := s.versions[i]
		current := ver.Number == art.CurrentVersion
		row := ui.Column(c.Key(ver.Number)).Gap(3).Padding(8, 10).Radius(t.RadiusSM).Border(1, t.Border).Background(t.BgCard)
		if current {
			row.BorderColor(t.Accent)
		}
		row.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "v"+itoa(ver.Number)).FontSize(t.TextSM).FontWeight(700)
				if current {
					kit.Badge(c, v.a.T("design.versions.current"), kit.ToneAccent)
				}
				if ver.Critique != nil {
					ui.Text(c, v.a.T("design.versions.score", "score", fmt.Sprintf("%.1f", ver.Critique.Score))).
						FontSize(t.TextXS).TextColor(t.FgMuted)
				}
				ui.Spacer(c)
				if !current {
					if kit.Button(c.Key("co"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: "rotate-ccw",
						Label: v.a.T("design.versions.checkout"), Tooltip: v.a.T("design.versions.checkoutHint")}).Clicked() {
						s.checkout(art.ID, ver.Number)
					}
				}
			})
			if ver.Summary != "" {
				ui.Text(c, ver.Summary).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
			ui.Text(c, safeDate(ver.CreatedAt, "2006-01-02 15:04")).FontSize(t.TextXS).TextColor(t.FgFaint)
		})
	}).Grow(1).Padding(8).Gap(6)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
