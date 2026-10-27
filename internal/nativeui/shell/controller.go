package shell

import (
	_ "embed"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// PNG for the tray on every platform (MyGo converts it where needed): the
// Pando tray artwork.
//
//go:embed assets/tray.png
var trayIconPNG []byte

// TrayProject is an open project workspace listed in the tray menu.
type TrayProject struct {
	ID, Name string
	// Current marks the workspace the window shows.
	Current bool
}

// TrayOptions configures the tray icon and menu.
type TrayOptions struct {
	// Labels override the English menu labels.
	Labels TrayLabels
	// OnSettings runs when "Settings" is chosen (main thread, window shown).
	OnSettings func()
	// OnProject runs when a project workspace is chosen.
	OnProject func(id string)
}

// TrayLabels are the tray menu labels.
type TrayLabels struct {
	Show, Settings, Projects, Quit string
}

func (l TrayLabels) orDefault() TrayLabels {
	if l.Show == "" {
		l.Show = "Show Pando"
	}
	if l.Settings == "" {
		l.Settings = "Settings"
	}
	if l.Projects == "" {
		l.Projects = "Project workspaces"
	}
	if l.Quit == "" {
		l.Quit = "Quit"
	}
	return l
}

// Controller is the handle on the window, tray and notifications that
// services use. Methods are safe from any goroutine.
type Controller struct {
	opts Options

	mu       sync.Mutex
	win      *mygo.Window
	tray     *mygo.Tray
	projects []TrayProject
	labels   TrayLabels

	trayOK      atomic.Bool // a tray icon is live
	trayOnClose atomic.Bool // setting: minimise/close go to the tray
	quitting    atomic.Bool // an explicit quit started
	focused     atomic.Bool // the window has the keyboard focus
}

func newController(o Options) *Controller {
	return &Controller{opts: o, labels: o.Tray.Labels.orDefault()}
}

// TrayAvailable reports whether a tray icon is live (it is not without
// libayatana-appindicator on Linux, or a tray host).
func (c *Controller) TrayAvailable() bool { return c.trayOK.Load() }

// SetTrayOnClose mirrors Desktop.TrayOnMinimizeClose: only then, and only
// with a live tray icon, do minimise and close hide the window.
func (c *Controller) SetTrayOnClose(on bool) { c.trayOnClose.Store(on) }

// UseTray reports whether minimise/close currently hide into the tray.
func (c *Controller) UseTray() bool { return c.trayOnClose.Load() && c.trayOK.Load() }

// Focused reports whether the main window has the keyboard focus.
func (c *Controller) Focused() bool { return c.focused.Load() }

// Quitting reports whether an explicit quit started.
func (c *Controller) Quitting() bool { return c.quitting.Load() }

// Quit quits the app, whatever the tray setting.
func (c *Controller) Quit() {
	c.quitting.Store(true)
	mygo.App.Quit()
}

// Window returns the main window, nil before it exists.
func (c *Controller) Window() *mygo.Window {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.win
}

// Show brings the main window back (from the tray, minimised or behind
// others), opening it again if it was closed.
func (c *Controller) Show() {
	c.openMain()
}

// SetTitle sets the main window title.
func (c *Controller) SetTitle(title string) {
	if w := c.Window(); w != nil && !w.IsDestroyed() {
		w.SetTitle(title)
	}
}

// SetTrayLabels replaces the tray menu labels (the UI language changed);
// empty ones keep the English default.
func (c *Controller) SetTrayLabels(l TrayLabels) {
	c.mu.Lock()
	c.labels = l.orDefault()
	t := c.tray
	c.mu.Unlock()
	if t != nil {
		t.SetMenu(c.trayMenu())
	}
}

// SetTrayProjects replaces the project workspaces of the tray menu.
func (c *Controller) SetTrayProjects(p []TrayProject) {
	c.mu.Lock()
	c.projects = append([]TrayProject(nil), p...)
	t := c.tray
	c.mu.Unlock()
	if t != nil {
		t.SetMenu(c.trayMenu())
	}
}

// Notify shows an OS notification; clicking it shows the window. A
// notification with the id of one still shown replaces it. Errors (macOS
// outside an app bundle, permission denied, no service) are logged.
func (c *Controller) Notify(id, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	n := mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body})
	n.OnClick(func() {
		c.Show()
		n.Close()
	})
	go func() {
		if err := n.Show(); err != nil {
			slog.Debug("nativeui: notification not shown", "error", err)
		}
	}()
}

// openMain shows the main window, creating it when it does not exist.
func (c *Controller) openMain() {
	c.mu.Lock()
	w := c.win
	c.mu.Unlock()
	if w != nil && !w.IsDestroyed() {
		w.Show()
		w.Restore()
		w.Focus()
		return
	}
	o := c.opts
	w = mygo.NewWindow(mygo.WindowOptions{
		Title:     o.Title,
		Width:     o.Width,
		Height:    o.Height,
		MinWidth:  720,
		MinHeight: 480,
		StateKey:  "main",
		Content:   ui.View(o.View),
	})
	c.mu.Lock()
	c.win = w
	c.mu.Unlock()
	c.focused.Store(true)
	w.OnFocus(func() { c.focused.Store(true) })
	w.OnBlur(func() { c.focused.Store(false) })
	w.OnClose(func(e *mygo.CloseEvent) {
		if c.quitting.Load() || !c.UseTray() {
			return
		}
		e.PreventDefault()
		w.Hide()
	})
	w.OnMinimize(func() {
		if c.UseTray() && !c.quitting.Load() {
			w.Hide()
		}
	})
	if o.OnWindow != nil {
		o.OnWindow(w)
	}
}

func (c *Controller) setupTray() {
	t, err := mygo.NewTray(mygo.TrayOptions{
		ID:      "pando",
		Icon:    trayIconPNG,
		ToolTip: "Pando",
		Menu:    c.trayMenu(),
	})
	if err != nil {
		slog.Info("nativeui: no tray icon, minimise/close keep the normal behaviour", "error", err)
		return
	}
	// With a menu the platform opens it on click; where it reports
	// activations instead (Linux AppIndicator double click) show the window.
	t.OnClick(c.Show)
	c.mu.Lock()
	c.tray = t
	c.mu.Unlock()
	c.trayOK.Store(true)
}

func (c *Controller) trayMenu() *mygo.Menu {
	c.mu.Lock()
	projects := append([]TrayProject(nil), c.projects...)
	l := c.labels
	c.mu.Unlock()
	items := []*mygo.MenuItem{
		{Label: l.Show, Click: func(*mygo.MenuItem, *mygo.Window) { c.Show() }},
		{Label: l.Settings, Click: func(*mygo.MenuItem, *mygo.Window) {
			c.Show()
			if c.opts.Tray.OnSettings != nil {
				c.opts.Tray.OnSettings()
			}
		}},
	}
	if len(projects) > 0 {
		items = append(items, mygo.Separator(), &mygo.MenuItem{Label: l.Projects, Disabled: true})
		for _, p := range projects {
			label := p.Name
			if p.Current {
				label += " (current)"
			}
			id := p.ID
			items = append(items, &mygo.MenuItem{Label: label, Click: func(*mygo.MenuItem, *mygo.Window) {
				c.Show()
				if c.opts.Tray.OnProject != nil {
					c.opts.Tray.OnProject(id)
				}
			}})
		}
	}
	items = append(items, mygo.Separator(), &mygo.MenuItem{Label: l.Quit, Click: func(*mygo.MenuItem, *mygo.Window) { c.Quit() }})
	return mygo.NewMenu(items)
}
