package projects

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func init() {
	app.Register(app.Area{
		ID: "projects", Path: "/projects", TitleKey: "nav.projects", Icon: kit.IconProjects,
		Section: app.SectionMain, Order: 2,
		New: func(a *app.Context) app.View { return newView(a) },
	})
}

// shortenPath replaces the home directory with ~ (web shortenPath).
func shortenPath(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	for _, pre := range []string{"/home/", "/Users/"} {
		if strings.HasPrefix(p, pre) {
			rest := strings.TrimPrefix(p, pre)
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				return "~" + rest[i:]
			}
			return "~"
		}
	}
	return p
}

// view is the Projects list (web ProjectsView).
type view struct {
	a *app.Context
	m *manager

	list ui.ListState

	showAdd    bool
	newPath    string
	newName    string
	adding     bool
	browser    *dirBrowser
	pendingDel string

	renameOpen bool
	renameID   string
	renameName string
	renameOrig string

	stopOpen  bool
	stopProj  client.Project
	requested bool
}

func newView(a *app.Context) *view {
	v := &view{a: a, m: managerFor(a), browser: newDirBrowser(a)}
	return v
}

// Title implements app.Titled.
func (v *view) Title() string {
	return fmt.Sprintf("%s (%d)", v.a.T("nav.projects"), len(v.m.projects))
}

// HeaderActions implements app.HeaderActions.
func (v *view) HeaderActions(c *ui.Context) {
	if v.m.target != "" && kit.Button(c.Key("clear-target"), kit.ButtonOpts{Label: v.a.T("projects.view.clearDelegationTarget"), Size: kit.SizeSM}).Clicked() {
		id := v.m.target
		v.a.Go(func(ctx context.Context) {
			if err := v.a.Client.DeactivateProject(ctx, id); err != nil {
				v.a.Toasts.Error(v.a.T("projects.toasts.error"), err.Error())
			}
			v.a.Update(v.m.refresh)
		})
	}
	if kit.Button(c.Key("add-project"), kit.ButtonOpts{Variant: kit.Primary, Icon: kit.IconPlus, Label: v.a.T("projects.view.addProject"), Size: kit.SizeSM}).Clicked() {
		v.showAdd = !v.showAdd
	}
}

func (v *view) add() {
	path := strings.TrimSpace(v.newPath)
	if path == "" || v.adding {
		return
	}
	name := strings.TrimSpace(v.newName)
	v.adding = true
	v.a.Go(func(ctx context.Context) {
		err := v.a.Client.AddProject(ctx, path, name)
		v.a.Update(func() {
			v.adding = false
			if err != nil {
				v.a.Toasts.Error(v.a.T("projects.toasts.error"), err.Error())
				return
			}
			v.a.Toasts.Success(v.a.T("projects.toasts.added", "path", path), "")
			v.newPath, v.newName, v.showAdd = "", "", false
			v.m.refresh()
		})
	})
}

func (v *view) stop(p client.Project) {
	v.m.StopProject(p.ID, func(err error) {
		if err != nil {
			v.a.Toasts.Error(v.a.T("projects.toasts.error"), err.Error())
		}
	})
}

func (v *view) remove(id string) {
	v.a.Go(func(ctx context.Context) {
		err := v.a.Client.DeleteProject(ctx, id)
		v.a.Update(func() {
			if err != nil {
				v.a.Toasts.Error(v.a.T("projects.toasts.error"), err.Error())
				return
			}
			v.m.removeTab(id)
			v.m.refresh()
		})
	})
}

func (v *view) rename() {
	id, name := v.renameID, strings.TrimSpace(v.renameName)
	v.renameOpen = false
	if name == "" || name == v.renameOrig {
		return
	}
	v.a.Go(func(ctx context.Context) {
		err := v.a.Client.RenameProject(ctx, id, name)
		v.a.Update(func() {
			if err != nil {
				v.a.Toasts.Error(v.a.T("projects.toasts.error"), err.Error())
				return
			}
			v.m.refresh()
		})
	})
}

func workspaceState(p client.Project) string {
	if p.WebState == "" {
		return stateStopped
	}
	return p.WebState
}

// Build implements app.View.
func (v *view) Build(c *ui.Context, r *ui.Route) {
	tk := kit.Current()
	m := v.m
	if !v.requested {
		v.requested = true
		m.start() // children have no OnStart hook; loads the list
		m.refresh()
	}

	if v.showAdd {
		v.addForm(c, tk)
	}
	ui.Row(c).Gap(8).Padding(10, 16).AlignItems(ui.Center).Children(func() {
		kit.Badge(c, v.a.T("projects.view.delegationTargetLabel"), kit.ToneNeutral).Tooltip(v.a.T("projects.view.delegationTargetTooltip"))
		txt := v.a.T("projects.view.delegationTargetUnset")
		if p, ok := m.project(m.target); ok {
			txt = v.a.T("projects.view.delegationTargetValue", "name", p.Name)
		}
		ui.Text(c, txt).FontSize(tk.TextSM).TextColor(tk.FgMuted)
	})

	switch {
	case m.loadErr != "" && len(m.projects) == 0:
		kit.EmptyState(c, kit.IconError, v.a.T("projects.toasts.error"), m.loadErr)
	case !m.loaded && len(m.projects) == 0:
		ui.Row(c).Grow(1).Gap(8).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			kit.Spinner(c)
			ui.Text(c, v.a.T("projects.view.loading")).TextColor(tk.FgMuted).FontSize(tk.TextSM)
		})
	case len(m.projects) == 0:
		kit.EmptyState(c, kit.IconProjects, v.a.T("projects.view.emptyTitle"), v.a.T("projects.view.emptyDescription"))
	default:
		v.table(c, tk)
	}

	v.browser.Build(c)
	v.dialogs(c, tk)
}

func (v *view) addForm(c *ui.Context, tk *kit.Tokens) {
	ui.Row(c).Gap(8).Padding(10, 16).AlignItems(ui.Center).Background(tk.BgShell).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
		in := kit.Input(c.Key("add-path"), &v.newPath, v.a.T("projects.view.addForm.pathPlaceholder")).Grow(2).Shrink(1).MinWidth(120)
		if in.Submitted() {
			v.add()
		}
		if kit.IconButton(c.Key("add-browse"), kit.IconFolder, v.a.T("projects.view.addForm.browse")).Clicked() {
			start := v.newPath
			v.browser.Show(start, func(p string) { v.newPath = p })
		}
		in2 := kit.Input(c.Key("add-name"), &v.newName, v.a.T("projects.view.addForm.namePlaceholder")).Grow(1).Shrink(1).MinWidth(100)
		if in2.Submitted() {
			v.add()
		}
		if kit.Button(c.Key("add-ok"), kit.ButtonOpts{Variant: kit.Primary, Label: v.a.T("projects.view.addForm.add"),
			Disabled: v.adding || strings.TrimSpace(v.newPath) == ""}).Clicked() {
			v.add()
		}
		if kit.IconButton(c.Key("add-cancel"), kit.IconClose, v.a.T("projects.view.common.cancel")).Clicked() {
			v.showAdd, v.newPath, v.newName = false, "", ""
		}
	})
}

func (v *view) table(c *ui.Context, tk *kit.Tokens) {
	m := v.m
	cols := []ui.TableColumn{
		{Title: v.a.T("projects.view.columns.name"), Width: 0},
		{Title: v.a.T("projects.view.columns.path"), Width: 0},
		{Title: v.a.T("projects.view.columns.workspace"), Width: 300, Fixed: true},
		{Title: v.a.T("projects.view.columns.actions"), Width: 230, Fixed: true, Align: ui.End},
	}
	v.list.Key = func(i int) any { return m.projects[i].ID }
	v.list.Label = func(i int) string { return m.projects[i].Name }
	ui.Table(c, &v.list, cols, len(m.projects), func(row, col int) {
		p := m.projects[row]
		switch col {
		case 0:
			ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(6, 0).Children(func() {
				nb := ui.ButtonBase(c.Key("n:" + p.ID)).Label(p.Name).Cursor(ui.CursorPointer).Padding(0).
					TextColor(tk.Fg).Tooltip(v.a.T("projects.view.rowTitle.openTab"))
				nb.Children(func() {
					name := ui.Text(c, p.Name).FontSize(tk.TextSM).SingleLine().Ellipsis("…")
					if p.ID == m.target {
						name.FontWeight(600)
					}
				})
				if nb.Clicked() {
					m.Open(p.ID)
				}
				if p.ID == m.target {
					kit.Badge(c, v.a.T("projects.view.badges.delegationTarget"), kit.ToneNeutral)
				}
			})
		case 1:
			ui.Text(c, shortenPath(p.Path)).FontSize(tk.TextXS).TextColor(tk.FgMuted).SingleLine().Ellipsis("…")
		case 2:
			v.statusBadges(c, p)
		case 3:
			v.actions(c, p)
		}
	}).Grow(1)
}

func (v *view) statusBadges(c *ui.Context, p client.Project) {
	state := workspaceState(p)
	label := v.a.T("projects.view.workspaceState." + state)
	if state == stateRunning && p.WebPort > 0 {
		label = v.a.T("projects.view.workspaceState.runningWithPort", "port", p.WebPort)
	}
	tone := kit.ToneNeutral
	switch state {
	case stateRunning:
		tone = kit.ToneSuccess
	case stateError:
		tone = kit.ToneDanger
	case stateStarting:
		tone = kit.ToneWarning
	}
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		kit.Badge(c, label, tone)
		if p.External {
			kit.Badge(c, v.a.T("projects.view.badges.external"), kit.ToneNeutral).Tooltip(v.a.T("projects.view.badgeTitles.external"))
		}
		if p.DelegationSpawned {
			kit.Badge(c, v.a.T("projects.view.badges.auto"), kit.ToneNeutral).Tooltip(v.a.T("projects.view.badgeTitles.auto"))
		}
		if p.Delegations > 0 {
			kit.Badge(c, v.a.T("projects.view.badges.delegations", "count", p.Delegations), kit.ToneWarning).
				Tooltip(v.a.T("projects.view.badgeTitles.delegations", "count", p.Delegations))
		}
	})
}

func (v *view) actions(c *ui.Context, p client.Project) {
	m := v.m
	open := false
	if t := m.find(p.ID); t != nil && (t.state == stateRunning || t.state == stateStarting) {
		open = true
	}
	canStop := p.Status == "running" || state3(workspaceState(p)) || m.find(p.ID) != nil
	ui.Row(c).Gap(4).AlignItems(ui.Center).Justify(ui.End).Children(func() {
		openLabel := v.a.T("projects.view.actions.openTab")
		if open {
			openLabel = v.a.T("projects.view.actions.focusTab")
		}
		if kit.IconButton(c.Key("o:"+p.ID), kit.IconProjects, openLabel).Clicked() {
			m.Open(p.ID)
		}
		stopLabel := v.a.T("projects.view.actions.stopDisabled")
		if canStop {
			stopLabel = v.a.T("projects.view.actions.stop")
		}
		sb := kit.Button(c.Key("s:"+p.ID), kit.ButtonOpts{Variant: ghostOrDanger(canStop), Icon: kit.IconStop, Tooltip: stopLabel, Size: kit.SizeSM, Disabled: !canStop})
		if sb.Clicked() {
			if p.Delegations > 0 {
				v.stopProj, v.stopOpen = p, true
			} else {
				v.stop(p)
			}
		}
		if kit.IconButton(c.Key("w:"+p.ID), kit.IconExternal, v.a.T("projects.view.actions.openInNewWindow")).Clicked() {
			v.a.Go(func(ctx context.Context) {
				if err := v.a.Client.OpenProjectDesktop(ctx, p.ID); err != nil {
					v.a.Toasts.Error(v.a.T("projects.toasts.error"), err.Error())
				}
			})
		}
		if kit.IconButton(c.Key("r:"+p.ID), kit.IconEdit, v.a.T("projects.view.actions.rename")).Clicked() {
			v.renameID, v.renameName, v.renameOrig, v.renameOpen = p.ID, p.Name, p.Name, true
		}
		if v.pendingDel == p.ID {
			if kit.Button(c.Key("dc:"+p.ID), kit.ButtonOpts{Variant: kit.Danger, Size: kit.SizeSM, Label: v.a.T("projects.view.actions.confirmDelete")}).Clicked() {
				v.pendingDel = ""
				v.remove(p.ID)
			}
		} else if kit.IconButton(c.Key("d:"+p.ID), kit.IconClose, v.a.T("projects.view.actions.delete")).Clicked() {
			v.pendingDel = p.ID
		}
	})
}

func state3(s string) bool { return s == stateStarting || s == stateRunning || s == stateError }

func ghostOrDanger(on bool) kit.Variant {
	if on {
		return kit.Danger
	}
	return kit.Ghost
}

func (v *view) dialogs(c *ui.Context, tk *kit.Tokens) {
	if v.renameOpen {
		kit.Dialog(c.Key("dlg-rename"), &v.renameOpen, kit.DialogOpts{
			Title: v.a.T("projects.view.renameDialog.title", "name", v.renameOrig), Width: 420,
			Body: func() {
				ui.Text(c, v.a.T("projects.view.renameDialog.label")).FontSize(tk.TextSM).TextColor(tk.FgMuted)
				if kit.Input(c.Key("rename-in"), &v.renameName, "").Submitted() {
					v.rename()
				}
			},
			Footer: func() {
				if kit.Button(c.Key("rename-cancel"), kit.ButtonOpts{Label: v.a.T("projects.view.common.cancel")}).Clicked() {
					v.renameOpen = false
				}
				if kit.Button(c.Key("rename-ok"), kit.ButtonOpts{Variant: kit.Primary, Label: v.a.T("projects.view.renameDialog.confirm")}).Clicked() {
					v.rename()
				}
			},
		})
	}
	if v.stopOpen {
		p := v.stopProj
		kit.Dialog(c.Key("dlg-stop"), &v.stopOpen, kit.DialogOpts{
			Title: v.a.T("projects.view.stopConfirmTitle"), Width: 440,
			Description: v.a.T("projects.view.stopConfirmMessage", "count", p.Delegations),
			Footer: func() {
				if kit.Button(c.Key("stop-cancel"), kit.ButtonOpts{Label: v.a.T("projects.view.common.cancel")}).Clicked() {
					v.stopOpen = false
				}
				if kit.Button(c.Key("stop-ok"), kit.ButtonOpts{Variant: kit.Danger, Label: v.a.T("projects.view.actions.stop")}).Clicked() {
					v.stopOpen = false
					v.stop(p)
				}
			},
		})
	}
}
