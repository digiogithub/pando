package settings

import (
	"context"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func newContainer(s *Settings) section {
	return docSection("settings.categories.containerRuntime", "", containerDoc)(s)
}

func newSandbox(s *Settings) section {
	return docSection("settings.categories.sandbox", "", sandboxDoc)(s)
}

func newTokenOpt(s *Settings) section {
	return docSection("settings.categories.tokenOptimization", "", tokenOptDoc)(s)
}

func newSnapshots(s *Settings) section {
	return docSection("settings.categories.snapshots", "", snapshotsDoc)(s)
}

// newAPIServer adds the regenerate-token action to the server document.
func newAPIServer(s *Settings) section {
	d := apiServerDoc(s)
	var token string
	var confirm bool
	d.Tail = func(c *ui.Context, d *Doc) {
		if !d.Bool("requireAuth") {
			return
		}
		kit.SettingsSection(c, "", "", func() {
			kit.SettingsRow(c, "Auth token", "Click \"Regenerate\" to create a new secure token. The token is shown once.", func() {
				if token != "" {
					kit.Input(c, &token, "").ReadOnly(true).Width(280)
				}
				if kit.SecondaryButton(c.Key("regen"), "Regenerate").Clicked() {
					confirm = true
				}
			})
		})
		kit.Dialog(c, &confirm, kit.DialogOpts{
			Title:       "Regenerate Auth Token",
			Description: "This will invalidate the current token. Any clients using the old token will lose access. Continue?",
			Footer: func() {
				if kit.SecondaryButton(c.Key("rg-cancel"), "Cancel").Clicked() {
					confirm = false
				}
				if kit.Button(c.Key("rg-ok"), kit.ButtonOpts{Label: "Regenerate", Variant: kit.Danger}).Clicked() {
					confirm = false
					s.app.Go(func(ctx context.Context) {
						tk, err := s.app.Client.RegenerateAPIToken(ctx)
						s.app.Update(func() {
							if err != nil {
								s.app.Toasts.Error("Failed to regenerate token", err.Error())
								return
							}
							token = tk
							s.app.Toasts.Success("Token regenerated", "")
						})
					})
				}
			},
		})
	}
	return &docPage{Doc: d, title: "settings.categories.apiServer"}
}
