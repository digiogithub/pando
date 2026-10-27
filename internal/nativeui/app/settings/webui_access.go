package settings

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// webuiAccess mirrors WebUIAccessSettings.tsx: basic-auth users of the
// WebUI. Every change is applied immediately (no Save bar).
type webuiAccess struct {
	status
	s        *Settings
	st       client.BasicAuthStatus
	enabled  bool
	user     string
	pass     string
	revealed map[string]string
	del      string
	delOpen  bool
}

func newWebUIAccess(s *Settings) section { return &webuiAccess{s: s, revealed: map[string]string{}} }

func (w *webuiAccess) SelfManaged()         {}
func (w *webuiAccess) Dirty() bool          { return false }
func (w *webuiAccess) Save(done func(bool)) { done(true) }
func (w *webuiAccess) Discard()             {}

func (w *webuiAccess) apply(st client.BasicAuthStatus) { w.st, w.enabled = st, st.Enabled }

func (w *webuiAccess) Load() {
	if w.loading {
		return
	}
	w.loading = true
	a := w.s.app
	a.Go(func(ctx context.Context) {
		st, err := a.Client.BasicAuth(ctx)
		a.Update(func() {
			w.loading = false
			if err != nil {
				w.err = err.Error()
				a.Toasts.Error(s2(a.T("settings.webuiAccess.loadFailed")), err.Error())
				return
			}
			w.err, w.loaded = "", true
			w.apply(st)
		})
	})
}

func s2(s string) string { return s }

// act runs a call returning the new status, with the error toast key.
func (w *webuiAccess) act(failKey string, ok string, call func(ctx context.Context) (client.BasicAuthStatus, error), after func()) {
	a := w.s.app
	a.Go(func(ctx context.Context) {
		st, err := call(ctx)
		a.Update(func() {
			if err != nil {
				a.Toasts.Error(a.T(failKey), err.Error())
				w.enabled = w.st.Enabled
				return
			}
			w.apply(st)
			if ok != "" {
				a.Toasts.Success(a.T(ok), "")
			}
			if after != nil {
				after()
			}
		})
	})
}

func (w *webuiAccess) Build(c *ui.Context) {
	s, t := w.s, kit.Current()
	a := s.app
	s.pageHeader(c, a.T("settings.webuiAccess.title"), "")
	if !w.loaded {
		s.loadingView(c, &w.status)
		return
	}
	msg := a.T("settings.webuiAccess.notEnforced", "host", orStr(w.st.BindHost, "localhost"))
	col := t.Warning
	if w.st.Enforced {
		msg, col = a.T("settings.webuiAccess.enforced", "host", w.st.BindHost), t.Success
	}
	ui.Row(c).Padding(10, 12).Radius(t.RadiusMD).Background(col.Alpha(0.12)).Border(1, col).Children(func() {
		ui.Text(c, msg).FontSize(t.TextSM).TextColor(col)
	})
	kit.SettingsSection(c, "", "", func() {
		kit.SettingsRow(c, a.T("settings.webuiAccess.enabled"), a.T("settings.webuiAccess.enabledDescription"), func() {
			if kit.Switch(c, &w.enabled, "").Changed() {
				on := w.enabled
				w.act("settings.webuiAccess.saveFailed", "", func(ctx context.Context) (client.BasicAuthStatus, error) {
					return a.Client.SetBasicAuthEnabled(ctx, on)
				}, nil)
			}
		})
	})
	kit.SettingsSection(c, a.T("settings.webuiAccess.users"), "", func() {
		if len(w.st.Users) == 0 {
			ui.Text(c, a.T("settings.webuiAccess.noUsers")).Padding(12, 16).FontSize(t.TextSM).TextColor(t.FgMuted)
		}
		for _, u := range w.st.Users {
			name := u.Username
			cc := c.Key("u:" + name)
			kit.SettingsRow(cc, name, "", func() {
				shown, ok := w.revealed[name]
				if !ok {
					shown = "••••••••"
				}
				ui.Text(cc, shown).FontSize(t.TextSM)
				lbl := a.T("settings.webuiAccess.reveal")
				if ok {
					lbl = a.T("settings.webuiAccess.hide")
				}
				if kit.IconButton(cc.Key("eye"), "eye", lbl).Clicked() {
					w.reveal(name)
				}
				if kit.IconButton(cc.Key("rm"), kit.IconTrash, a.T("settings.webuiAccess.delete")).Clicked() {
					w.del, w.delOpen = name, true
				}
			})
		}
		ui.Column(c).Padding(12, 16).Gap(8).Children(func() {
			ui.Row(c).Gap(12).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, a.T("settings.webuiAccess.username")).FontSize(t.TextXS).TextColor(t.FgMuted)
					kit.Input(c.Key("nu"), &w.user, "admin").Width(220)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, a.T("settings.webuiAccess.password")).FontSize(t.TextXS).TextColor(t.FgMuted)
					kit.Input(c.Key("np"), &w.pass, "").Password().Width(220)
				})
			})
			if kit.Button(c.Key("add"), kit.ButtonOpts{Label: a.T("settings.webuiAccess.addUser"), Variant: kit.Primary,
				Disabled: strings.TrimSpace(w.user) == "" || w.pass == ""}).Clicked() {
				u, p := strings.TrimSpace(w.user), w.pass
				w.act("settings.webuiAccess.saveFailed", "settings.webuiAccess.userSaved", func(ctx context.Context) (client.BasicAuthStatus, error) {
					return a.Client.AddBasicAuthUser(ctx, u, p)
				}, func() { w.user, w.pass = "", "" })
			}
			ui.Text(c, a.T("settings.webuiAccess.storageNote")).FontSize(t.TextXS).TextColor(t.FgMuted)
		})
	})
	kit.Dialog(c, &w.delOpen, kit.DialogOpts{
		Title:       a.T("settings.webuiAccess.deleteTitle"),
		Description: a.T("settings.webuiAccess.deleteMessage", "username", w.del),
		Footer: func() {
			if kit.SecondaryButton(c.Key("d-no"), a.T("settings.unsaved.cancel")).Clicked() {
				w.delOpen = false
			}
			if kit.Button(c.Key("d-yes"), kit.ButtonOpts{Label: a.T("settings.webuiAccess.delete"), Variant: kit.Danger}).Clicked() {
				w.delOpen = false
				u := w.del
				w.act("settings.webuiAccess.deleteFailed", "", func(ctx context.Context) (client.BasicAuthStatus, error) {
					return a.Client.DeleteBasicAuthUser(ctx, u)
				}, func() { delete(w.revealed, u) })
			}
		},
	})
}

func (w *webuiAccess) reveal(name string) {
	a := w.s.app
	if _, ok := w.revealed[name]; ok {
		delete(w.revealed, name)
		return
	}
	a.Go(func(ctx context.Context) {
		pw, err := a.Client.RevealBasicAuthPassword(ctx, name)
		a.Update(func() {
			if err != nil {
				a.Toasts.Error(a.T("settings.webuiAccess.revealFailed"), err.Error())
				return
			}
			w.revealed[name] = pw
		})
	})
}
