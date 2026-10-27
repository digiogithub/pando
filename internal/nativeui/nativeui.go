// Package nativeui is the entry point of the native desktop UI (MyGo): Run
// connects the API client, builds the app context and the root view with
// every registered area, and runs the window on the main goroutine.
//
// Layout of the tree (see README.md):
//
//	client/  Pando HTTP API client (no MyGo)
//	i18n/    Web UI catalogs + T()
//	kit/     theme, icons, base components
//	shell/   MyGo app, window, menus
//	app/     Context, area registry, router, layout
//	app/<area>/  one package per area (chat, settings, ...)
package nativeui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/i18n"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/digiogithub/pando/internal/nativeui/natives"
	"github.com/digiogithub/pando/internal/nativeui/shell"
	"github.com/egoist/mygo"

	// Areas register themselves; add each new area package here.
	_ "github.com/digiogithub/pando/internal/nativeui/app/agentvcs"
	_ "github.com/digiogithub/pando/internal/nativeui/app/chat"
	_ "github.com/digiogithub/pando/internal/nativeui/app/design"
	_ "github.com/digiogithub/pando/internal/nativeui/app/editor"
	_ "github.com/digiogithub/pando/internal/nativeui/app/evaluator"
	_ "github.com/digiogithub/pando/internal/nativeui/app/extensions"
	_ "github.com/digiogithub/pando/internal/nativeui/app/instances"
	_ "github.com/digiogithub/pando/internal/nativeui/app/logs"
	_ "github.com/digiogithub/pando/internal/nativeui/app/orchestrator"
	_ "github.com/digiogithub/pando/internal/nativeui/app/overlays"
	_ "github.com/digiogithub/pando/internal/nativeui/app/projects"
	_ "github.com/digiogithub/pando/internal/nativeui/app/settings"
	_ "github.com/digiogithub/pando/internal/nativeui/app/setup"
	_ "github.com/digiogithub/pando/internal/nativeui/app/terminal"
)

// ErrNoDisplay is returned by Run when no display server is reachable.
var ErrNoDisplay = shell.ErrNoDisplay

// AcquireSingleInstance makes `pando desktop` focus the window of a running
// native desktop instead of starting a second one. key is the working
// directory: different directories (a project's own window, spawned with
// `pando desktop --cwd`) are different instances. It returns false after
// handing the command line to the running instance; call it before any
// server or database starts. PANDO_DESKTOP_MULTI=1 disables the lock.
func AcquireSingleInstance(key string) bool { return shell.AcquireSingleInstance(key) }

// Options configures Run.
type Options struct {
	// Ctx ends the app when canceled (signals, server shutdown).
	Ctx context.Context
	// BaseURL of the Pando API server.
	BaseURL string
	// Token is the API token when known (in-process server); otherwise it
	// is fetched from /api/v1/token.
	Token string
	// CWD is the working directory the server runs in (window title).
	CWD string
	// Language selects the UI language ("" uses $LANG, falling back to en).
	Language string
	// Theme selects the theme (zero value: Pando following the system).
	Theme kit.Settings
	// Path is the route opened first (default /chat).
	Path string
	// Simple opens the simple chat (`pando desktop --simple`).
	Simple bool
	// Logger receives UI logs (default slog.Default()).
	Logger *slog.Logger
}

// Run shows the native desktop and blocks until it quits. It must be called
// from the main goroutine.
func Run(o Options) error {
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	c, err := client.New(client.Options{BaseURL: o.BaseURL, Token: o.Token})
	if err != nil {
		return err
	}
	lang := o.Language
	if lang == "" {
		lang = os.Getenv("LANG")
	}
	i18n.SetLanguage(lang)

	ctx, cancel := context.WithCancel(o.Ctx)
	defer cancel()
	a := app.NewContext(ctx, c)
	a.CWD = o.CWD
	if o.Logger != nil {
		a.Log = o.Logger.With("component", "nativeui")
	}
	// The terminal's libghostty-vt travels inside the binary: extract it
	// before any area is built (MyGo reads $MYGO_GHOSTTY_VT when the
	// terminal first loads). Without an embedded copy MyGo finds or
	// downloads its own.
	if p, err := natives.Install(); err != nil {
		a.Log.Warn("embedded libghostty-vt not installed; terminal falls back to MyGo's lookup", "error", err)
	} else if p != "" {
		a.Log.Debug("libghostty-vt extracted", "path", p)
	}
	if o.Theme.Family != "" || o.Theme.Mode != "" {
		a.Theme = o.Theme
		if a.Theme.Mode == "" {
			a.Theme.Mode = kit.ModeSystem
		}
	}
	if o.Path == "" {
		// Development aid: open a route at start (PANDO_NATIVE_PATH=/chat/s/<id>).
		o.Path = os.Getenv("PANDO_NATIVE_PATH")
	}
	if o.Path == "" && o.Simple {
		o.Path = "/chat/simple"
	}
	if o.Path != "" {
		a.Router.Replace(o.Path)
		a.Shell().StartExplicit = true
	}
	root := app.NewRoot(a)
	defer root.Close()

	a.Go(func(ctx context.Context) {
		info, err := c.Health(ctx)
		if err != nil {
			a.Toasts.Error("Pando server unreachable", err.Error())
			return
		}
		a.Update(func() { a.Server = info })
	})

	title := "Pando"
	if o.CWD != "" {
		title = "Pando — " + o.CWD
	}
	var servicesOnce sync.Once
	width, height := devWindowSize()
	err = shell.Run(shell.Options{
		Title:  title,
		Width:  width,
		Height: height,
		Ctx:    ctx,
		View:   root.View,
		OnReady: func(h *shell.Controller) {
			// The window does not exist yet; services start with it.
			a.Shell().Host = h
		},
		OnWindow: func(w *mygo.Window) {
			a.SetDispatcher(w)
			startCapture(w, func(p string) { a.Router.Push(p) })
			servicesOnce.Do(func() { a.StartServices(a.Shell().Host) })
		},
		Tray: shell.TrayOptions{
			Labels: a.TrayLabels(),
			OnSettings: func() {
				a.Update(func() { a.Navigate("/settings") })
			},
			OnProject: func(id string) {
				a.Update(func() { a.FocusProjectWorkspace(id) })
			},
		},
		OnSecondInstance: func(args []string) {
			// `pando desktop --simple` while running: show the simple chat.
			if slices.Contains(args, "--simple") {
				a.Update(func() { a.Router.Push("/chat/simple") })
			}
		},
		Menu: []*mygo.MenuItem{
			{Label: "Open in Browser", Click: func(*mygo.MenuItem, *mygo.Window) {
				_ = mygo.Shell.OpenExternal(c.BaseURL())
			}},
		},
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// devWindowSize reads PANDO_NATIVE_SIZE=<w>x<h> (development aid: wide-window
// screenshots); zero values keep the shell's default size.
func devWindowSize() (w, h int) {
	_, err := fmt.Sscanf(os.Getenv("PANDO_NATIVE_SIZE"), "%dx%d", &w, &h)
	if err != nil || w < 0 || h < 0 {
		return 0, 0
	}
	return w, h
}
