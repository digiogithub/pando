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
	"log/slog"
	"os"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/i18n"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/digiogithub/pando/internal/nativeui/shell"
	"github.com/egoist/mygo"

	// Areas register themselves; add each new area package here.
	_ "github.com/digiogithub/pando/internal/nativeui/app/chat"
)

// ErrNoDisplay is returned by Run when no display server is reachable.
var ErrNoDisplay = shell.ErrNoDisplay

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
	if o.Path != "" {
		a.Router.Replace(o.Path)
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
	err = shell.Run(shell.Options{
		Title: title,
		Ctx:   ctx,
		View:  root.View,
		OnWindow: func(w *mygo.Window) {
			a.SetDispatcher(w)
			startCapture(w)
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
