package settings

import (
	"context"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/i18n"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// docSection wraps a Doc builder as a category with a page header.
func docSection(title, desc string, mk func(*Settings) *Doc) func(*Settings) section {
	return func(s *Settings) section { return &docPage{Doc: mk(s), title: title, desc: desc} }
}

type docPage struct {
	*Doc
	title, desc string
}

func (p *docPage) Build(c *ui.Context) {
	p.s.pageHeader(c, p.s.app.T(p.title), p.s.app.T(p.desc))
	p.Doc.Build(c)
}

var languageNames = map[string]string{
	"en": "English", "es": "Español", "fr": "Français", "de": "Deutsch", "pt": "Português", "ja": "日本語", "zh": "中文",
}

func newGeneral(s *Settings) section {
	d := generalDoc(s)
	var ver client.VersionStatus
	lang := ""
	d.Loaded = func(d *Doc, root client.Obj) {
		s.app.Go(func(ctx context.Context) {
			v, err := s.app.Client.Version(ctx)
			if err == nil {
				s.app.Update(func() { ver = v })
			}
		})
	}
	d.Head = func(c *ui.Context, d *Doc) {
		t := kit.Current()
		var names []string
		for _, code := range i18n.Languages() {
			n := languageNames[code]
			if n == "" {
				n = code
			}
			names = append(names, n)
		}
		if lang == "" {
			lang = languageNames[i18n.Language()]
		}
		kit.SettingsSection(c, "", "", func() {
			kit.SettingsRow(c, s.app.T(g+"language"), "", func() {
				if kit.Select(c, &lang, names).Width(240).Changed() {
					for code, n := range languageNames {
						if n == lang {
							i18n.SetLanguage(code)
							s.app.Go(func(ctx context.Context) {
								if err := s.app.Client.SetLanguagePreference(ctx, code); err != nil {
									s.app.Toasts.Error("Could not store the language", err.Error())
								}
							})
						}
					}
				}
			})
			if ver.Version != "" {
				desc := ""
				switch {
				case ver.UpdateAvailable && ver.Latest != "":
					desc = s.app.T("version.updateAvailable", "latest", ver.Latest) + " (" + orStr(ver.UpdateCommand, "pando update") + ")"
				case ver.Checkable && ver.Latest != "":
					desc = s.app.T("version.upToDate")
				}
				kit.SettingsRow(c, s.app.T(g+"pandoVersion"), desc, func() {
					ui.Text(c, ver.Version).FontSize(t.TextSM)
				})
			}
		})
	}
	d.Tail = func(c *ui.Context, d *Doc) {
		id, _ := d.raw["telemetry_debug_id"].(string)
		if id == "" {
			return
		}
		kit.SettingsSection(c, "", "", func() {
			kit.SettingsRow(c, s.app.T(g+"telemetryDebugId"), "", func() {
				ui.Text(c, id).FontSize(kit.Current().TextSM)
				if kit.Button(c.Key("regen"), kit.ButtonOpts{Label: s.app.T(g + "telemetryRegenerateId"), Size: kit.SizeSM, Disabled: d.saving}).Clicked() {
					s.app.Go(func(ctx context.Context) {
						nid, err := s.app.Client.RegenerateTelemetryID(ctx)
						s.app.Update(func() {
							if err != nil {
								s.app.Toasts.Error(err.Error(), "")
								return
							}
							d.raw["telemetry_debug_id"] = nid
							s.app.Toasts.Success("Debug ID regenerated", "")
						})
					})
				}
			})
		})
	}
	return &docPage{Doc: d, title: "settings.general.title"}
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
