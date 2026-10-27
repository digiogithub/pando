// Package shell bootstraps the native desktop on MyGo: the application, the
// main window with native decorations, and the application menu. It owns the
// MyGo event loop, which must run on the main goroutine (cobra's RunE does,
// and MyGo's init keeps the main goroutine on the main OS thread).
//
// Platform integration that comes later (P1: tray, notifications, Dock
// reopen, close/minimise to tray, single instance) belongs here too.
package shell

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// ErrNoDisplay is returned by Run when no display server is reachable (an
// SSH session, WSL without WSLg, a headless CI): callers can suggest the
// browser UI instead.
var ErrNoDisplay = errors.New("no display available for the native desktop window")

// Options configures Run.
type Options struct {
	// Name is the application name (menu bar, notifications). Default Pando.
	Name string
	// Title of the main window.
	Title string
	// Width and Height of the main window in DIPs (default 1280x820).
	Width, Height int
	// View builds the window's content every frame.
	View func(c *ui.Context)
	// OnWindow runs on the main thread once the main window exists: connect
	// dispatchers (the window implements Update/Invalidate) here.
	OnWindow func(w *mygo.Window)
	// Menu adds items to the default application menu (see DefaultMenu).
	Menu []*mygo.MenuItem
	// Ctx, when canceled (SIGINT, server shutdown), quits the app.
	Ctx context.Context
}

// Run starts the event loop and shows the main window. It blocks until the
// app quits and must be called from the main goroutine.
func Run(o Options) error {
	if o.View == nil {
		return errors.New("shell: Options.View is required")
	}
	if o.Name == "" {
		o.Name = "Pando"
	}
	if o.Title == "" {
		o.Title = o.Name
	}
	if o.Width <= 0 {
		o.Width = 1280
	}
	if o.Height <= 0 {
		o.Height = 820
	}
	app := mygo.App
	app.SetName(o.Name)
	app.SetMenu(DefaultMenu(o.Menu))

	var win *mygo.Window
	openMain := func() {
		if win != nil && !win.IsDestroyed() {
			win.Show()
			win.Focus()
			return
		}
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     o.Title,
			Width:     o.Width,
			Height:    o.Height,
			MinWidth:  720,
			MinHeight: 480,
			StateKey:  "main",
			Content:   ui.View(o.View),
		})
		if o.OnWindow != nil {
			o.OnWindow(win)
		}
	}
	app.WhenReady(openMain)
	// macOS keeps the app alive with its Dock icon and reopens the window
	// on click; elsewhere the last window closing quits.
	app.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			app.Quit()
		}
	})
	app.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			openMain()
		}
	})
	if o.Ctx != nil {
		stop := context.AfterFunc(o.Ctx, func() { app.Quit() })
		defer stop()
	}
	if err := app.Run(); err != nil {
		if isNoDisplay(err) {
			return fmt.Errorf("%w: %v", ErrNoDisplay, err)
		}
		return err
	}
	return nil
}

func isNoDisplay(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cannot open display") || strings.Contains(msg, "no display") ||
		strings.Contains(msg, "failed to init gtk") || strings.Contains(msg, "gtk_init")
}

// DefaultMenu is the application menu: the app menu (macOS), File with the
// extra items, the Edit role menu (copy and paste keys need it on macOS),
// View and Window.
func DefaultMenu(extra []*mygo.MenuItem) *mygo.Menu {
	file := append([]*mygo.MenuItem{}, extra...)
	if len(file) > 0 {
		file = append(file, mygo.Separator())
	}
	file = append(file, &mygo.MenuItem{Role: mygo.RoleClose})
	if runtime.GOOS != "darwin" {
		file = append(file, &mygo.MenuItem{Role: mygo.RoleQuit})
	}
	items := []*mygo.MenuItem{}
	if runtime.GOOS == "darwin" {
		items = append(items, &mygo.MenuItem{Role: mygo.RoleAppMenu})
	}
	items = append(items,
		&mygo.MenuItem{Label: "File", Submenu: file},
		&mygo.MenuItem{Role: mygo.RoleEditMenu},
		&mygo.MenuItem{Label: "View", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleToggleFullScreen},
		}},
		&mygo.MenuItem{Role: mygo.RoleWindowMenu},
	)
	return mygo.NewMenu(items)
}
