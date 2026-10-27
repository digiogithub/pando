package app

import (
	"sort"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/shell"
	"github.com/egoist/mygo/ui"
)

// Host is what the window shell (internal/nativeui/shell) offers the app:
// *shell.Controller implements it. It is nil in headless tests, so callers
// go through the Shell helpers, which tolerate that.
type Host interface {
	// Focused reports whether the window has the keyboard focus.
	Focused() bool
	// Show brings the window back (tray, minimised, behind others).
	Show()
	// Quit quits the app, whatever the tray setting.
	Quit()
	// SetTitle sets the window title.
	SetTitle(title string)
	// SetTrayOnClose mirrors Desktop.TrayOnMinimizeClose.
	SetTrayOnClose(on bool)
	// SetTrayProjects lists the open project workspaces in the tray menu.
	SetTrayProjects(p []shell.TrayProject)
	// Notify shows an OS notification.
	Notify(id, title, body string)
}

// Shell is the window-level state shared by the chrome and the overlays:
// which overlay is open, the sidebar, the host. UI thread only.
type Shell struct {
	// Host is the window shell (nil headless).
	Host Host

	// QuickMenuOpen and ModelSwitcherOpen are the overlays (Ctrl/Cmd+K or
	// Ctrl/Cmd+P, and Ctrl/Cmd+O).
	QuickMenuOpen     bool
	ModelSwitcherOpen bool

	// SidebarCollapsed is the icon-rail sidebar (persisted in the UI
	// preferences).
	SidebarCollapsed bool

	// StartExplicit is set when the first route was chosen (--simple, a
	// path option); otherwise the persisted chat mode may pick it.
	StartExplicit bool

	// NextModel is a model picked in the switcher while no chat session was
	// open: the chat sends it with the first prompt (the web's
	// sessionModelStore). Empty when none.
	NextModel string
	// ModelChanged, when set (the chat sets it), is told after the model
	// switcher changed the model of a session ("" = none open).
	ModelChanged func(model, sessionID string)

	// FocusProject focuses a project workspace tab by project ID (tray
	// menu). The projects area sets it; the default only opens /projects.
	FocusProject func(id string)

	title   string // last window title sent to the host
	trayKey string // last tray project list sent to the host
}

// Shell returns the shared window state of the root context (a context made
// by ForClient shares its parent's).
func (a *Context) Shell() *Shell {
	if a.parent != nil {
		return a.parent.Shell()
	}
	return a.shell
}

// OpenQuickMenu opens the command palette.
func (a *Context) OpenQuickMenu() {
	s := a.Shell()
	s.ModelSwitcherOpen = false
	s.QuickMenuOpen = true
	a.Invalidate()
}

// OpenModelSwitcher opens the model switcher overlay.
func (a *Context) OpenModelSwitcher() {
	s := a.Shell()
	s.QuickMenuOpen = false
	s.ModelSwitcherOpen = true
	a.Invalidate()
}

// ActiveSessionID returns the chat session the route shows ("" outside a
// session), from /chat/s/{id} or /chat/simple/s/{id}.
func (a *Context) ActiveSessionID() string {
	p := a.Router.Path()
	prefix := "/chat/s/"
	if strings.HasPrefix(p, "/chat/simple/s/") {
		prefix = "/chat/simple/s/"
	}
	if len(p) > len(prefix) && p[:len(prefix)] == prefix {
		id := p[len(prefix):]
		for i := 0; i < len(id); i++ {
			if id[i] == '/' || id[i] == '?' {
				return id[:i]
			}
		}
		return id
	}
	return ""
}

// ReportError tells the user a background call failed. Authorization
// failures get a clear message of their own, shown at most once a minute.
func (a *Context) ReportError(title string, err error) {
	if err == nil {
		return
	}
	a.Log.Warn(title, "error", err)
	if isAuthErr(err) && !a.authToastAllowed() {
		return
	}
	a.Toasts.Error(title, describeErr(err))
}

// Overlay is a layer drawn above the whole window (dialogs, palettes). Build
// runs every frame on the main tab; it draws nothing while closed.
type Overlay interface {
	Build(c *ui.Context)
}

// Banner is a strip drawn above the header of the main tab (config init,
// update notice). Build runs every frame and draws nothing when not needed.
type Banner = Overlay

// HeaderSlot is a control of the header bar, on the right of the view's own
// actions. The persona selector registers one with ID "persona".
type HeaderSlot struct {
	ID string
	// Order sorts slots ascending; built-ins use 100+ (simple chat 100,
	// docs 110, theme 120, settings 130).
	Order int
	Build func(c *ui.Context, a *Context)
}

// Command is an entry of the quick menu (command palette).
type Command struct {
	ID    string
	Title string
	// TitleKey, when set, is the i18n key of the title (Title is then the
	// fallback when the key is missing).
	TitleKey    string
	Description string
	// Group is "command" (default) or "account".
	Group    string
	Icon     string
	Keywords []string
	Run      func(a *Context)
}

var (
	slotMu      sync.RWMutex
	overlayFs   []func(a *Context) Overlay
	bannerFs    []bannerFactory
	headerSlots []HeaderSlot
	commands    []Command
)

type bannerFactory struct {
	order int
	f     func(a *Context) Banner
}

// RegisterOverlay registers a factory called once per window root.
func RegisterOverlay(f func(a *Context) Overlay) {
	slotMu.Lock()
	overlayFs = append(overlayFs, f)
	slotMu.Unlock()
}

// RegisterBanner registers a banner factory; banners stack by order.
func RegisterBanner(order int, f func(a *Context) Banner) {
	slotMu.Lock()
	bannerFs = append(bannerFs, bannerFactory{order, f})
	slotMu.Unlock()
}

// RegisterHeaderSlot adds a header control, replacing one with the same ID.
func RegisterHeaderSlot(s HeaderSlot) {
	slotMu.Lock()
	defer slotMu.Unlock()
	for i := range headerSlots {
		if headerSlots[i].ID == s.ID {
			headerSlots[i] = s
			return
		}
	}
	headerSlots = append(headerSlots, s)
}

// RegisterCommand adds a quick menu command, replacing one with the same ID.
func RegisterCommand(cmd Command) {
	slotMu.Lock()
	defer slotMu.Unlock()
	for i := range commands {
		if commands[i].ID == cmd.ID {
			commands[i] = cmd
			return
		}
	}
	commands = append(commands, cmd)
}

// Commands returns the registered quick menu commands.
func Commands() []Command {
	slotMu.RLock()
	defer slotMu.RUnlock()
	return append([]Command(nil), commands...)
}

func newOverlays(a *Context) []Overlay {
	slotMu.RLock()
	fs := append([]func(*Context) Overlay(nil), overlayFs...)
	slotMu.RUnlock()
	out := make([]Overlay, 0, len(fs))
	for _, f := range fs {
		if o := f(a); o != nil {
			out = append(out, o)
		}
	}
	return out
}

func newBanners(a *Context) []Banner {
	slotMu.RLock()
	fs := append([]bannerFactory(nil), bannerFs...)
	slotMu.RUnlock()
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].order < fs[j].order })
	out := make([]Banner, 0, len(fs))
	for _, f := range fs {
		if b := f.f(a); b != nil {
			out = append(out, b)
		}
	}
	return out
}

func sortedHeaderSlots() []HeaderSlot {
	slotMu.RLock()
	s := append([]HeaderSlot(nil), headerSlots...)
	slotMu.RUnlock()
	sort.SliceStable(s, func(i, j int) bool { return s[i].Order < s[j].Order })
	return s
}
