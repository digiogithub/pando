package kit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// WebViewHost creates embedded web views; *mygo.Window implements it.
type WebViewHost interface {
	NewWebView(opts mygo.WebViewOptions) (*mygo.WebView, error)
}

// WebPane is an embedded web view inside the native UI (MyGo ui.WebView):
// the preview canvas of the Design Studio, extension panels. The web view is
// created on first use; where the platform cannot provide one (Linux without
// WebKitGTK) the pane shows a message and an "Open in browser" button
// instead, so callers need no special case.
//
// All methods run on the UI thread (Build and event handlers), except
// Eval/EvalAs which may run from any goroutine once Ready reports true.
type WebPane struct {
	// Host returns the window to create the web view in (nil in tests).
	Host func() WebViewHost
	// Preload is JavaScript run before the page's own scripts.
	Preload string
	// OnChange asks for a redraw (a load finished or failed).
	OnChange func()

	view *mygo.WebView
	// err is why the web view could not be created.
	err error

	mu      sync.Mutex
	url     string // URL requested
	loadErr string // last load failure
	loading bool
}

// ErrNoWebView is the reason shown when no window is available to host a
// web view.
var ErrNoWebView = errors.New("embedded web views are not available in this window")

// Ready reports whether the web view exists.
func (p *WebPane) Ready() bool { return p.view != nil }

// Err is why the web view is unavailable ("" when it is).
func (p *WebPane) Err() string {
	if p.err == nil {
		return ""
	}
	return p.err.Error()
}

// URL returns the URL the pane was asked to show.
func (p *WebPane) URL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url
}

// LoadError returns the last page load failure ("" when none).
func (p *WebPane) LoadError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loadErr
}

// Load shows url, creating the web view on first use. An empty url keeps
// the current page. Loading the same URL again reloads it only when force is
// set.
func (p *WebPane) Load(url string, force bool) {
	p.mu.Lock()
	same := p.url == url
	p.url = url
	p.loadErr = ""
	p.mu.Unlock()
	if url == "" {
		return
	}
	if p.view == nil && p.err == nil {
		p.create(url)
		if p.view != nil {
			return
		}
	}
	if p.view == nil {
		return
	}
	if same && !force {
		return
	}
	p.mu.Lock()
	p.loading = true
	p.mu.Unlock()
	if err := p.view.Page().LoadURL(url); err != nil {
		p.mu.Lock()
		p.loadErr = err.Error()
		p.loading = false
		p.mu.Unlock()
	}
}

func (p *WebPane) create(url string) {
	var host WebViewHost
	if p.Host != nil {
		host = p.Host()
	}
	if host == nil {
		p.err = ErrNoWebView
		return
	}
	v, err := host.NewWebView(mygo.WebViewOptions{
		URL:  url,
		Page: mygo.PageOptions{PreloadScript: p.Preload},
	})
	if err != nil {
		p.err = err
		return
	}
	p.view = v
	p.mu.Lock()
	p.loading = true
	p.mu.Unlock()
	pg := v.Page()
	devCapture(v)
	pg.OnDidFinishLoad(func() {
		p.mu.Lock()
		p.loading = false
		p.mu.Unlock()
		if p.OnChange != nil {
			p.OnChange()
		}
	})
	pg.OnDidFailLoad(func(e *mygo.LoadError) {
		p.mu.Lock()
		p.loading = false
		p.loadErr = fmt.Sprint(e)
		p.mu.Unlock()
		if p.OnChange != nil {
			p.OnChange()
		}
	})
}

// Reload reloads the page.
func (p *WebPane) Reload() {
	if p.view != nil {
		p.view.Page().Reload()
	}
}

// Eval runs JavaScript in the page and returns its value (the body of an
// async function: use return). Safe from goroutines.
func (p *WebPane) Eval(code string) (any, error) {
	if p.view == nil {
		return nil, ErrNoWebView
	}
	return p.view.Page().Eval(code)
}

// Build draws the web view filling its container, or the fallback. Pass the
// URL to open in the system browser from the fallback (usually URL()).
func (p *WebPane) Build(c *ui.Context, browserURL string) {
	t := Current()
	if p.view != nil && p.LoadError() == "" {
		ui.WebView(c, p.view).Grow(1).AlignSelf(ui.Stretch).Radius(t.RadiusSM).Border(1, t.Border)
		return
	}
	title := "The embedded web view is unavailable"
	detail := p.Err()
	if detail == "" {
		title = "The page could not be loaded"
		detail = p.LoadError()
	}
	if p.err != nil {
		detail += "\nOn Linux the embedded view needs WebKitGTK (libwebkit2gtk-4.1) at run time."
	}
	ui.Column(c).Grow(1).AlignSelf(ui.Stretch).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Padding(24).
		Border(1, t.Border).Radius(t.RadiusSM).Background(t.BgCard).Children(func() {
		Icon(c, IconWarning).FontSize(28).TextColor(t.FgFaint)
		ui.Text(c, title).FontSize(t.TextMD).FontWeight(600).TextAlign(ui.Center)
		if detail != "" {
			ui.Text(c, detail).FontSize(t.TextSM).TextColor(t.FgMuted).TextAlign(ui.Center).MaxWidth(520)
		}
		if browserURL != "" {
			if Button(c.Key("webpane-browser"), ButtonOpts{Variant: Primary, Icon: IconExternal, Label: "Open in browser"}).Clicked() {
				_ = mygo.Shell.OpenExternal(browserURL)
			}
		}
	})
}

// OpenWebWindow opens a native window of its own showing url in an embedded
// web view (the design canvas, a preview). apply sets the theme each frame
// (usually kit.Apply(c, a.Theme)). Call it on the UI thread. Without
// WebKitGTK/WebView2 the window shows the fallback with "Open in browser".
func OpenWebWindow(title, url string, apply func(c *ui.Context)) *mygo.Window {
	pane := &WebPane{}
	var win *mygo.Window
	win = mygo.NewWindow(mygo.WindowOptions{
		Title:  title,
		Width:  1200,
		Height: 800,
		Content: ui.View(func(c *ui.Context) {
			if apply != nil {
				apply(c)
			}
			t := Current()
			pane.Host = func() WebViewHost { return win }
			pane.Load(url, false)
			ui.Column(c).Fill().AlignItems(ui.Stretch).Background(t.Bg).Padding(8).Children(func() {
				pane.Build(c, url)
			})
		}),
	})
	return win
}

// devCapture is a development aid: PANDO_NATIVE_CAPTURE_WEB=<dir> writes a
// screenshot of every embedded page 8 s after it is created
// (WebView.CapturePage; the window capture shows a hole there), and
// PANDO_NATIVE_WEB_EVAL=<js> additionally runs the script first and writes its
// result to <dir>/eval.txt.
func devCapture(v *mygo.WebView) {
	dir := os.Getenv("PANDO_NATIVE_CAPTURE_WEB")
	if dir == "" {
		return
	}
	go func() {
		time.Sleep(6 * time.Second)
		if js := os.Getenv("PANDO_NATIVE_WEB_EVAL"); js != "" {
			res, err := v.Page().Eval(js)
			_ = os.WriteFile(filepath.Join(dir, "eval.txt"), []byte(fmt.Sprintf("%v\n%v\n", res, err)), 0o644)
			time.Sleep(time.Second)
		}
		if png, err := v.CapturePage(); err == nil {
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("web-%d.png", time.Now().Unix())), png, 0o644)
		}
	}()
}
