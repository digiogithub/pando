package overlays

import (
	"context"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// banner draws a one-line strip with a message and actions.
func banner(c *ui.Context, tone kit.Tone, icon, msg string, actions func()) {
	t := kit.Current()
	fg, bg := kit.ToneColors(tone)
	ui.Row(c).Shrink(0).Padding(8, 16).Gap(10).AlignItems(ui.Center).Background(bg).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		kit.Icon(c, icon).FontSize(15).TextColor(fg)
		ui.Text(c, msg).FontSize(t.TextSM).TextColor(t.Fg).Grow(1).Shrink(1)
		actions()
	})
}

// configInit offers to generate .pando.toml when the working directory has
// none (the web's ConfigInitBanner).
type configInit struct {
	a          *app.Context
	started    bool
	status     *client.ConfigInitStatus
	dismissed  bool
	generating bool
}

func newConfigInit(a *app.Context) *configInit { return &configInit{a: a} }

// Build implements app.Banner.
func (b *configInit) Build(c *ui.Context) {
	if !b.started {
		b.started = true
		b.fetch()
	}
	if b.dismissed || b.status == nil || !b.status.ShouldGenerate {
		return
	}
	a := b.a
	msg := "No .pando.toml found in current directory."
	if b.status.HasPandoDir {
		msg = "Project is pre-initialised but has no local config file."
	}
	msg += " Generate one to configure providers and models."
	banner(c, kit.ToneInfo, kit.IconInfo, msg, func() {
		if kit.Button(c.Key("ci-setup"), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Label: "Setup assistant"}).Clicked() {
			a.Navigate("/setup")
		}
		label := "Generate .pando.toml"
		if b.generating {
			label = "Generating…"
		}
		if kit.Button(c.Key("ci-gen"), kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Label: label, Disabled: b.generating}).Clicked() {
			b.generate()
		}
		if kit.Button(c.Key("ci-dismiss"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: a.T("version.dismiss")}).Clicked() {
			b.dismissed = true
		}
	})
}

func (b *configInit) fetch() {
	a := b.a
	a.Go(func(ctx context.Context) {
		s, err := a.Client.ConfigInitStatus(ctx)
		if err != nil {
			if client.IsAuthError(err) {
				a.ReportError("Could not check the configuration", err)
			}
			return
		}
		a.Update(func() { b.status = &s })
	})
}

func (b *configInit) generate() {
	a := b.a
	b.generating = true
	a.Go(func(ctx context.Context) {
		err := a.Client.GenerateConfig(ctx)
		var s client.ConfigInitStatus
		if err == nil {
			s, err = a.Client.ConfigInitStatus(ctx)
		}
		a.Update(func() {
			b.generating = false
			if err != nil {
				a.ReportError("Could not generate .pando.toml", err)
				return
			}
			b.status = &s
			a.Navigate("/settings")
		})
	})
}

// updateNotice announces a newer release (GET /api/v1/version), with the
// release notes link and the update command.
type updateNotice struct {
	a         *app.Context
	started   bool
	status    *client.VersionStatus
	dismissed bool
}

func newUpdateNotice(a *app.Context) *updateNotice { return &updateNotice{a: a} }

// Build implements app.Banner.
func (u *updateNotice) Build(c *ui.Context) {
	if !u.started {
		u.started = true
		u.fetch()
	}
	if u.dismissed || u.status == nil || !u.status.UpdateAvailable {
		return
	}
	v := u.status
	a := u.a
	msg := a.T("version.banner", "latest", v.Latest, "version", v.Version)
	if v.UpdateCommand != "" {
		msg += " " + a.T("version.updateWith", "command", v.UpdateCommand)
	}
	banner(c, kit.ToneAccent, kit.IconUpdate, msg, func() {
		if v.ReleaseURL != "" {
			if kit.Button(c.Key("up-notes"), kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Label: a.T("version.releaseNotes"), Icon: kit.IconExternal}).Clicked() {
				app.OpenExternal(v.ReleaseURL)
			}
		}
		if kit.Button(c.Key("up-dismiss"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: a.T("version.dismiss")}).Clicked() {
			u.dismissed = true
		}
	})
}

func (u *updateNotice) fetch() {
	a := u.a
	a.Go(func(ctx context.Context) {
		v, err := a.Client.Version(ctx)
		if err != nil {
			return
		}
		a.Update(func() { u.status = &v })
	})
}
