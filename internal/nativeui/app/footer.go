package app

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// ExternalAccess mirrors GET/PUT /api/v1/config/api-server/external-access:
// whether the running server listens on 0.0.0.0 (the Web UI's footer toggle).
type ExternalAccess struct {
	Enabled        bool     `json:"enabled"`
	BindHost       string   `json:"bindHost"`
	Port           int      `json:"port"`
	CanToggle      bool     `json:"canToggle"`
	BasicAuthReady bool     `json:"basicAuthReady"`
	URLs           []string `json:"urls"`
}

const externalAccessPath = "/api/v1/config/api-server/external-access"

// footerState is the footer toggle's state of one app context.
type footerState struct {
	mu     sync.Mutex
	status *ExternalAccess // nil: unknown or not supported (older backend)
	busy   bool
}

var (
	footerMu     sync.Mutex
	footerStates = map[*Context]*footerState{}
)

func footerFor(a *Context) *footerState {
	footerMu.Lock()
	defer footerMu.Unlock()
	s, ok := footerStates[a]
	if !ok {
		s = &footerState{}
		footerStates[a] = s
	}
	return s
}

func init() {
	OnStart(func(a *Context) { footerFor(a).load(a) })
	RegisterStatusItem(StatusItem{
		Order:   -10,
		Visible: func(a *Context) bool { return !footerChild(a) && footerFor(a).visible() },
		Build:   func(c *ui.Context, a *Context) { footerFor(a).build(c, a) },
	})
}

// footerChild reports a project workspace, which shares the main window's
// server and so hides the toggle (the web's childMode).
func footerChild(a *Context) bool {
	return a.IsChild() || a.Server.StartupMode == "project-child"
}

func (s *footerState) visible() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status != nil && (s.status.CanToggle || s.status.Enabled)
}

func (s *footerState) load(a *Context) {
	a.Go(func(ctx context.Context) {
		var st ExternalAccess
		err := a.Client.Get(ctx, externalAccessPath, &st)
		s.mu.Lock()
		if err != nil {
			s.status = nil // missing on older backends: the toggle stays hidden
		} else {
			s.status = &st
		}
		s.mu.Unlock()
		a.Invalidate()
	})
}

// toggle flips the bind. Enabling needs a Web UI user first, otherwise it
// warns and opens the settings (as the web).
func (s *footerState) toggle(a *Context) {
	s.mu.Lock()
	st := s.status
	if st == nil || s.busy {
		s.mu.Unlock()
		return
	}
	if !st.Enabled && !st.BasicAuthReady {
		s.mu.Unlock()
		a.Toasts.Push(ui.Toast{Description: a.T("externalAccess.needsCredentials"), Type: kit.ToastWarning})
		a.Router.Push("/settings/webui-access")
		return
	}
	want := !st.Enabled
	s.busy = true
	s.mu.Unlock()
	a.Go(func(ctx context.Context) {
		var next ExternalAccess
		err := a.Client.Put(ctx, externalAccessPath, map[string]bool{"enabled": want}, &next)
		s.mu.Lock()
		s.busy = false
		if err == nil {
			s.status = &next
		}
		s.mu.Unlock()
		switch {
		case err != nil:
			a.Toasts.Error(a.T("externalAccess.toggleFailed"), err.Error())
		case next.Enabled:
			where := ":" + strconv.Itoa(next.Port)
			if len(next.URLs) > 0 {
				where = strings.Join(next.URLs, ", ")
			}
			a.Toasts.Success(a.T("externalAccess.enabledToast", "urls", where), "")
		default:
			a.Toasts.Info(a.T("externalAccess.disabledToast"), "")
		}
		a.Invalidate()
	})
}

func (s *footerState) build(c *ui.Context, a *Context) {
	t := kit.Current()
	s.mu.Lock()
	st, busy := s.status, s.busy
	s.mu.Unlock()
	ui.Row(c).Height(26).Gap(10).AlignItems(ui.Center).Children(func() {
		if v := a.Server.Version; v != "" {
			ui.Text(c, "Pando "+v).FontSize(t.TextXS).TextColor(t.FgFaint)
		}
		ui.Spacer(c)
		if st == nil || (!st.CanToggle && !st.Enabled) {
			return
		}
		label, tip := a.T("externalAccess.off"), a.T("externalAccess.offTooltip")
		col := t.FgMuted
		if st.Enabled {
			urls := strings.Join(st.URLs, "\n")
			if urls == "" {
				urls = st.BindHost + ":" + strconv.Itoa(st.Port)
			}
			label, tip, col = a.T("externalAccess.on"), a.T("externalAccess.onTooltip", "urls", urls), t.Success
		}
		fixed := !st.CanToggle
		if fixed {
			tip = a.T("externalAccess.fixedTooltip")
		}
		b := ui.Row(c.Key("extaccess")).Gap(5).Padding(2, 8).Radius(t.RadiusSM).AlignItems(ui.Center).
			Focusable().Label(label).Tooltip(tip)
		if !fixed && !busy {
			b.Cursor(ui.CursorPointer)
			if b.Hovered() {
				b.Background(t.BgHover)
			}
			if b.Clicked() {
				s.toggle(a)
			}
		}
		b.Children(func() {
			kit.Icon(c, kit.IconGlobe).FontSize(12).TextColor(col)
			ui.Text(c, label).FontSize(t.TextXS).TextColor(col)
		})
	})
}
