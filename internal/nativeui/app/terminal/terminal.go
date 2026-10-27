// Package terminal is the Terminal area of the native desktop: tabs of
// interactive shells running in real PTYs on the server, shown by MyGo's
// terminal plugin (libghostty-vt, loaded at run time without cgo) and fed by
// the WebSocket PTY endpoint /api/v1/terminal/pty, the same one the Web UI's
// xterm.js pane uses (binary frames carry I/O, JSON text frames carry
// ready/exit/resize/close).
//
// libghostty-vt is not linked: the plugin downloads the platform library into
// the user cache the first time a terminal opens (checked against a SHA-256)
// or loads it from $MYGO_GHOSTTY_VT. When it cannot be loaded the area shows
// the reason and offers a retry, and points to `pando app` (Web UI).
package terminal

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

const iconTrash = "trash"

func init() {
	app.Register(app.Area{
		ID: "terminal", Path: "/terminal", TitleKey: "nav.terminal", Icon: kit.IconTerminal,
		Section: app.SectionTools, Order: 1,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

// tab is one shell.
type tab struct {
	id    int
	title string
	term  *terminal.Terminal
	conn  *ptyConn
	// err is set when the shell never became ready.
	err    string
	exited bool
}

// Terminal is the area's state (UI thread only).
type Terminal struct {
	app   *app.Context
	tabs  []*tab
	activ int
	seq   int

	libErr  string
	loaded  bool
	started bool
	themeID *kit.Tokens
}

// New returns the terminal view.
func New(a *app.Context) *Terminal { return &Terminal{app: a, activ: -1} }

// Close hangs up the terminals without killing the shells (they stay on the
// server and can be reattached by id, as the Web UI does on reload).
func (t *Terminal) Close() {
	for _, tb := range t.tabs {
		tb.conn.detachOnClose = true
		if tb.term != nil {
			_ = tb.term.Close()
		}
	}
}

// Title is the header title.
func (t *Terminal) Title() string { return t.app.T("nav.terminal") }

// HeaderActions adds New and Clear.
func (t *Terminal) HeaderActions(c *ui.Context) {
	if kit.Button(c.Key("term:new"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: kit.IconPlus, Label: "New"}).Clicked() {
		t.newTab()
	}
	cur := t.current()
	if kit.Button(c.Key("term:clear"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconTrash, Label: "Clear",
		Disabled: cur == nil || cur.term == nil || cur.exited, Tooltip: "Clear terminal"}).Clicked() {
		cur.term.Send([]byte{0x0c}) // Ctrl+L, as the Web UI does
	}
}

func (t *Terminal) current() *tab {
	if t.activ < 0 || t.activ >= len(t.tabs) {
		return nil
	}
	return t.tabs[t.activ]
}

// ensureLib loads libghostty-vt once; the first call may download it.
func (t *Terminal) ensureLib() bool {
	if t.loaded {
		return true
	}
	if err := terminal.Load(); err != nil {
		t.libErr = err.Error()
		return false
	}
	t.loaded, t.libErr = true, ""
	return true
}

func (t *Terminal) newTab() {
	if t.app.Client == nil || !t.ensureLib() {
		return
	}
	t.seq++
	tb := &tab{id: t.seq, title: fmt.Sprintf("Terminal %d", t.seq)}
	pc := newPtyConn(t.app, tb)
	light, dark := themes()
	term, err := terminal.New(terminal.Options{
		Conn: pc, Theme: light, DarkTheme: dark, Scrollback: 10000,
		OnTitle: func(title string) {
			t.app.Update(func() {
				if title != "" {
					tb.title = title
				}
			})
		},
	})
	if err != nil {
		t.libErr = err.Error()
		return
	}
	tb.term, tb.conn = term, pc
	t.tabs = append(t.tabs, tb)
	t.activ = len(t.tabs) - 1
}

func (t *Terminal) closeTab(i int) {
	tb := t.tabs[i]
	tb.conn.terminateOnClose = true
	if tb.term != nil {
		_ = tb.term.Close()
	}
	t.tabs = append(t.tabs[:i], t.tabs[i+1:]...)
	switch {
	case len(t.tabs) == 0:
		t.activ = -1
	case t.activ >= len(t.tabs):
		t.activ = len(t.tabs) - 1
	case i < t.activ:
		t.activ--
	}
}

// themes derives the terminal colors from the app tokens (light and dark).
func themes() (light, dark *terminal.Theme) {
	cur := kit.Current()
	mk := func(isDark bool, base *terminal.Theme) *terminal.Theme {
		tk := kit.Resolve(cur.Family, isDark, "")
		th := *base
		th.Foreground, th.Background = tk.Fg, tk.Bg
		th.Cursor = tk.Accent
		return &th
	}
	return mk(false, terminal.LightTheme()), mk(true, terminal.DarkTheme())
}

// Build shows the tab strip and the active terminal.
func (t *Terminal) Build(c *ui.Context, r *ui.Route) {
	th := kit.Current()
	if !t.started {
		t.started = true
		t.newTab()
	}
	if t.themeID != th {
		t.themeID = th
		light, dark := themes()
		for _, tb := range t.tabs {
			tb.term.SetTheme(light, dark)
		}
	}
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		if t.libErr != "" && len(t.tabs) == 0 {
			t.unavailable(c, th)
			return
		}
		t.tabStrip(c, th)
		cur := t.current()
		if cur == nil {
			kit.EmptyState(c, kit.IconTerminal, "No terminals", "Create a new terminal tab with New.")
			return
		}
		if cur.err != "" {
			ui.Row(c).Padding(8, 16).Background(th.WarningSoft).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, t.app.T("terminal.ptyUnavailable", "reason", cur.err)).FontSize(th.TextSM).Grow(1)
				if kit.Button(c.Key("term:retry"), kit.ButtonOpts{Size: kit.SizeSM, Icon: kit.IconRefresh, Label: "Retry"}).Clicked() {
					i := t.activ
					t.closeTab(i)
					t.newTab()
				}
			})
		}
		terminal.View(c.Key("term:"+strconv.Itoa(cur.id)), cur.term).Grow(1).FillWidth().MinHeight(0).AutoFocus()
	})
}

func (t *Terminal) unavailable(c *ui.Context, th *kit.Tokens) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Padding(32).Children(func() {
		kit.Icon(c, kit.IconTerminal).FontSize(32).TextColor(th.FgFaint)
		ui.Text(c, "The terminal engine could not be loaded").FontSize(th.TextLG).FontWeight(600)
		ui.Text(c, t.libErr).FontSize(th.TextSM).TextColor(th.FgMuted).MaxWidth(560).TextAlign(ui.Center)
		ui.Text(c, "The native terminal downloads libghostty-vt on first use (set MYGO_GHOSTTY_VT to use a local copy). The browser UI (pando app) has a terminal meanwhile.").
			FontSize(th.TextSM).TextColor(th.FgMuted).MaxWidth(560).TextAlign(ui.Center)
		if kit.Button(c.Key("term:lib-retry"), kit.ButtonOpts{Icon: kit.IconRefresh, Label: "Retry"}).Clicked() {
			t.newTab()
		}
	})
}

func (t *Terminal) tabStrip(c *ui.Context, th *kit.Tokens) {
	ui.Row(c).Height(34).Shrink(0).Background(th.BgShell).BorderWidth(0, 0, 1, 0).BorderColor(th.Border).AlignItems(ui.Stretch).Children(func() {
		for i, tb := range t.tabs {
			active := i == t.activ
			cc := c.Key("tt:" + strconv.Itoa(tb.id))
			el := ui.Row(cc).Gap(6).Padding(0, 10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(tb.title).
				BorderWidth(0, 0, 2, 0).BorderColor(ui.RGBA(0, 0, 0, 0))
			if el.Clicked() {
				t.activ = i
			}
			if active {
				el.Background(th.Bg).BorderColor(th.Accent)
			} else if el.Hovered() {
				el.Background(th.BgHover)
			}
			el.Children(func() {
				kit.Icon(cc, kit.IconTerminal).FontSize(12).TextColor(map[bool]ui.Color{true: th.Accent, false: th.FgFaint}[active])
				ui.Text(cc, tb.title).FontSize(th.TextSM).SingleLine().Ellipsis("…").MaxWidth(180).
					TextColor(map[bool]ui.Color{true: th.Fg, false: th.FgMuted}[active])
				if tb.exited {
					ui.Text(cc, "(exited)").FontSize(th.TextXS).TextColor(th.FgFaint)
				}
				if kit.Button(cc.Key("x"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: kit.IconClose, Tooltip: "Close terminal"}).Width(22).Height(22).Clicked() {
					t.closeTab(i)
				}
			})
		}
	})
}

// ---- the PTY connection as a terminal Conn ----

// ptyConn adapts the server's PTY websocket to the plugin's Conn: output is
// read from a pipe, writes go to the socket, Resize(cols, rows) both starts
// the shell (so it begins at the real size, like the Web UI fitting xterm
// before connecting) and follows later size changes.
type ptyConn struct {
	app *app.Context
	tab *tab

	pr *io.PipeReader
	pw *io.PipeWriter

	mu         sync.Mutex
	pty        *client.PtyConn
	connecting bool
	cols, rows int
	sessionID  string
	ready      bool
	closed     bool

	detachOnClose    bool
	terminateOnClose bool
}

func newPtyConn(a *app.Context, tb *tab) *ptyConn {
	pr, pw := io.Pipe()
	p := &ptyConn{app: a, tab: tb, pr: pr, pw: pw}
	// A terminal whose view never gives a size still starts, at 80x24.
	time.AfterFunc(700*time.Millisecond, func() { p.connect(80, 24) })
	return p
}

func (p *ptyConn) Read(b []byte) (int, error) { return p.pr.Read(b) }

func (p *ptyConn) Write(b []byte) (int, error) {
	p.mu.Lock()
	pc := p.pty
	p.mu.Unlock()
	if pc == nil {
		return len(b), nil // typed before the shell is up: dropped
	}
	return pc.Write(b)
}

// Resize implements the plugin's size interface.
func (p *ptyConn) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	p.mu.Lock()
	pc := p.pty
	p.cols, p.rows = cols, rows
	p.mu.Unlock()
	if pc == nil {
		p.connect(cols, rows)
		return nil
	}
	return pc.Resize(cols, rows)
}

func (p *ptyConn) connect(cols, rows int) {
	p.mu.Lock()
	if p.pty != nil || p.connecting || p.closed {
		p.mu.Unlock()
		return
	}
	p.connecting = true
	sid := p.sessionID
	p.mu.Unlock()
	p.app.Go(func(ctx context.Context) {
		pc, err := p.app.Client.ConnectPty(ctx, sid, cols, rows, client.PtyHandlers{
			OnData: func(b []byte) { _, _ = p.pw.Write(b) },
			OnReady: func(r client.PtyReady) {
				p.mu.Lock()
				p.sessionID, p.ready = r.SessionID, true
				p.mu.Unlock()
			},
			OnExit: func(code int) {
				p.app.Update(func() {
					p.tab.exited = true
					if p.tab.term != nil {
						p.tab.term.Feed([]byte(fmt.Sprintf("\r\n\x1b[90m[process exited with code %d]\x1b[0m\r\n", code)))
					}
				})
			},
			OnClose: func(err error) {
				p.mu.Lock()
				ready := p.ready
				p.mu.Unlock()
				p.app.Update(func() {
					if !ready {
						reason := "terminal connection closed"
						if err != nil {
							reason = err.Error()
						}
						p.tab.err = reason
						return
					}
					if !p.tab.exited && p.tab.term != nil {
						p.tab.term.Feed([]byte("\r\n\x1b[90m[disconnected]\x1b[0m\r\n"))
					}
				})
			},
		})
		p.mu.Lock()
		p.connecting = false
		if err == nil && p.closed {
			_ = pc.Detach()
			err = io.ErrClosedPipe
		}
		if err == nil {
			p.pty = pc
		}
		p.mu.Unlock()
		if err != nil {
			p.app.Update(func() { p.tab.err = err.Error() })
			return
		}
		// The size may have changed while connecting.
		p.mu.Lock()
		c2, r2 := p.cols, p.rows
		p.mu.Unlock()
		if c2 > 0 && (c2 != cols || r2 != rows) {
			_ = pc.Resize(c2, r2)
		}
	})
}

// Close ends the terminal: the shell is killed when the tab was closed by
// the user, left running (detached) otherwise.
func (p *ptyConn) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	pc := p.pty
	p.mu.Unlock()
	_ = p.pw.Close()
	_ = p.pr.Close()
	if pc == nil {
		return nil
	}
	if p.terminateOnClose {
		return pc.Terminate()
	}
	return pc.Detach()
}
