package projects

import (
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// buildTab draws the content of the workspace tab id.
func (m *manager) buildTab(c *ui.Context, id string) {
	t := m.find(id)
	if t == nil {
		kit.EmptyState(c, kit.IconFolder, m.a.T("projects.workspace.unknownTitle"), m.a.T("projects.workspace.unknownDescription"))
		return
	}
	switch t.state {
	case stateRunning:
		root := m.ensureRoot(t)
		ui.Column(c.Key("ws:" + id)).Fill().AlignItems(ui.Stretch).Children(func() { root.View(c.Key("wsroot:" + id)) })
	case stateStarting:
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
			kit.Spinner(c).FontSize(22)
			ui.Text(c, m.a.T("projects.workspace.startingTitle")).FontSize(kit.Current().TextLG).FontWeight(600)
			ui.Text(c, m.a.T("projects.workspace.startingDescription", "path", orID(t.path, t.name))).
				FontSize(kit.Current().TextSM).TextColor(kit.Current().FgMuted)
		})
	case stateStopped:
		m.workspaceMessage(c, t, kit.IconFolder, m.a.T("projects.workspace.stoppedTitle", "name", t.name),
			m.a.T("projects.workspace.stoppedDescription"), "", m.a.T("projects.workspace.reopen"), false)
	default:
		m.workspaceMessage(c, t, kit.IconError, m.a.T("projects.workspace.errorTitle", "name", t.name),
			m.a.T("projects.workspace.errorDescription"), t.errMsg, m.a.T("projects.workspace.restart"), true)
	}
}

func (m *manager) workspaceMessage(c *ui.Context, t *wsTab, icon, title, desc, detail, action string, showClose bool) {
	tk := kit.Current()
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Padding(32).Children(func() {
		kit.Icon(c, icon).FontSize(32).TextColor(tk.FgFaint)
		ui.Text(c, title).FontSize(tk.TextLG).FontWeight(600).TextAlign(ui.Center)
		ui.Text(c, desc).FontSize(tk.TextSM).TextColor(tk.FgMuted).TextAlign(ui.Center).MaxWidth(420)
		if detail != "" {
			ui.Text(c, detail).FontSize(tk.TextXS).TextColor(tk.Danger).MaxWidth(560).Padding(8, 12).
				Radius(tk.RadiusSM).Background(tk.BgCard).Border(1, tk.Border)
		}
		ui.Row(c).Gap(8).Children(func() {
			if kit.Button(c.Key("ws-act"), kit.ButtonOpts{Variant: kit.Primary, Icon: kit.IconRefresh, Label: action}).Clicked() {
				m.Open(t.id)
			}
			if showClose && kit.Button(c.Key("ws-close"), kit.ButtonOpts{Label: m.a.T("projects.workspace.close"), Icon: kit.IconClose}).Clicked() {
				m.removeTab(t.id)
			}
		})
	})
}

// Dialogs implements app.ShellExt.
func (m *manager) Dialogs(c *ui.Context) {
	tk := kit.Current()
	if m.initFor != nil {
		p := m.initFor
		kit.Dialog(c.Key("dlg-init"), &m.initOpen, kit.DialogOpts{
			Title: m.a.T("projects.initWizard.title"), Width: 460,
			Body: func() {
				ui.Text(c, shortenPath(p.Path)).FontSize(tk.TextSM).Padding(8, 12).Radius(tk.RadiusSM).Background(tk.BgShell)
				ui.Text(c, m.a.T("projects.initWizard.description")).FontSize(tk.TextSM).TextColor(tk.FgMuted)
				for _, it := range [][2]string{
					{".pando.toml", "config"}, {".pando/data/", "database"}, {".pando/mesnada/", "agents"}, {"agents/skills/", "skills"},
				} {
					ui.Row(c).Gap(6).Children(func() {
						ui.Text(c, "•").TextColor(tk.FgMuted)
						ui.Text(c, it[0]+" — "+m.a.T("projects.initWizard.items."+it[1])).FontSize(tk.TextSM).TextColor(tk.FgMuted)
					})
				}
			},
			Footer: func() {
				if kit.Button(c.Key("init-cancel"), kit.ButtonOpts{Label: m.a.T("projects.initWizard.cancel")}).Clicked() {
					m.initOpen = false
				}
				if kit.Button(c.Key("init-ok"), kit.ButtonOpts{Variant: kit.Primary, Label: m.a.T("projects.initWizard.confirm"), Disabled: m.initBusy}).Clicked() {
					m.confirmInit()
				}
			},
		})
		if !m.initOpen && !m.initBusy {
			// Closed with Escape or a click outside: same as cancel.
			m.cancelInit()
		}
	}
	if m.closeFor != "" {
		id := m.closeFor
		kit.Dialog(c.Key("dlg-close"), &m.closeOpen, kit.DialogOpts{
			Title: m.a.T("projects.tabs.closeConfirmTitle"), Description: m.a.T("projects.tabs.closeConfirmMessage"), Width: 460,
			Footer: func() {
				if kit.Button(c.Key("close-cancel"), kit.ButtonOpts{Label: m.a.T("projects.view.common.cancel")}).Clicked() {
					m.closeOpen = false
				}
				if kit.Button(c.Key("close-keep"), kit.ButtonOpts{Label: m.a.T("projects.tabs.keepRunningTitle")}).Clicked() {
					m.chooseClose(id, "keep")
				}
				if kit.Button(c.Key("close-stop"), kit.ButtonOpts{Variant: kit.Danger, Label: m.a.T("projects.tabs.closeAndStop")}).Clicked() {
					m.chooseClose(id, "stop")
				}
			},
		})
		if !m.closeOpen {
			m.closeFor = ""
		}
	}
}

func (m *manager) chooseClose(id, choice string) {
	m.closeChoice = choice // remembered for the session, as the Web UI does
	m.closeOpen, m.closeFor = false, ""
	m.closeWith(id, choice == "stop")
}
