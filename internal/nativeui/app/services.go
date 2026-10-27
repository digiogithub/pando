package app

import (
	"context"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/shell"
)

// StartServices connects the context to the window shell and starts the
// background services of the window: persisted UI preferences, the
// minimise/close-to-tray setting (followed live through /config/events) and
// OS notifications for runs finishing while the window is unfocused. Call it
// on the UI thread once the window exists. h may be nil (headless).
func (a *Context) StartServices(h Host) {
	s := a.Shell()
	s.Host = h
	a.Go(a.followPrefs)
	if h == nil {
		return
	}
	a.Go(a.followDesktopSettings)
	a.Go(a.followNotifications)
}

func (a *Context) followPrefs(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := a.Client.GetUIPrefs(cctx)
	if err != nil {
		if isAuthErr(err) {
			a.ReportError("Could not load preferences", err)
		}
		return
	}
	a.Update(func() {
		a.ApplyPrefs(p)
		// Reopen in the simple chat when that was the last choice, unless
		// the launcher asked for a route.
		if sh := a.Shell(); p.ChatMode == "simple" && !sh.StartExplicit && a.Router.Path() == DefaultPath {
			a.Router.Replace("/chat/simple")
		}
	})
}

// followDesktopSettings applies Desktop.TrayOnMinimizeClose now and on every
// config change.
func (a *Context) followDesktopSettings(ctx context.Context) {
	h := a.Shell().Host
	apply := func() {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		s, err := a.Client.GetDesktopSettings(cctx)
		if err != nil {
			if isAuthErr(err) {
				a.ReportError("Could not read settings", err)
			}
			return
		}
		h.SetTrayOnClose(s.TrayOnMinimizeClose)
	}
	apply()
	err := a.Client.SubscribeConfigEvents(ctx, func(ev client.ConfigEvent) {
		// Connected covers a reconnect that missed changes; an empty section
		// is a full reload.
		if ev.Connected || ev.Section == "" || strings.EqualFold(ev.Section, "desktop") {
			apply()
		}
	})
	if isAuthErr(err) {
		a.ReportError("Lost the settings stream", err)
	}
}

func (a *Context) followNotifications(ctx context.Context) {
	err := a.Client.SubscribeNotifications(ctx, a.handleNotification)
	if isAuthErr(err) {
		a.ReportError("Lost the notification stream", err)
	}
}

// handleNotification shows an OS notification for agent completions and
// tool (MCP) failures while the window is unfocused, as the Wails shell did.
func (a *Context) handleNotification(n client.Notification) {
	h := a.Shell().Host
	if h == nil || h.Focused() {
		return
	}
	title := "Pando"
	switch n.Source {
	case "agent":
	case "tool":
		title = "Pando MCP"
	default:
		return
	}
	id := "pando/" + n.ID
	if n.ID == "" {
		id = "pando/" + time.Now().Format("150405.000")
	}
	h.Notify(id, title, n.Message)
}

// syncChrome pushes window state to the host once per frame: the title and
// the tray project list, each only when it changed. UI thread.
func (r *Root) syncChrome(title string) {
	s := r.App.Shell()
	h := s.Host
	if h == nil {
		return
	}
	if title != "" {
		full := "Pando — " + title
		if full != s.title {
			s.title = full
			h.SetTitle(full)
		}
	}
	var projects []shell.TrayProject
	key := ""
	for _, e := range r.exts {
		for _, tab := range e.Tabs() {
			projects = append(projects, shell.TrayProject{ID: tab.ID, Name: tab.Title, Current: tab.ID == e.Active()})
			key += tab.ID + "|" + tab.Title + "|"
			if tab.ID == e.Active() {
				key += "*"
			}
		}
	}
	if key != s.trayKey {
		s.trayKey = key
		h.SetTrayProjects(projects)
	}
}

// FocusProjectWorkspace focuses a project workspace tab (tray menu).
func (a *Context) FocusProjectWorkspace(id string) {
	s := a.Shell()
	if s.FocusProject != nil {
		s.FocusProject(id)
		return
	}
	a.Router.Push("/projects")
}
