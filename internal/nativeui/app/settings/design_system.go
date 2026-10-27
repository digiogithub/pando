package settings

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

var designSources = []Opt{{"code", "Code"}, {"url", "URL"}, {"image", "Image"}, {"text", "Style guide"}}
var designHints = map[string]string{"code": "Path of a project directory", "url": "https://example.com", "image": "Path to a screenshot or logo", "text": "File path, or a bundled example name"}

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// designSystem mirrors DesignSystemSettings.tsx: name + token editor and
// extraction from code, a URL, an image or a style guide.
type designSystem struct {
	status
	s        *Settings
	payload  client.DesignSystemPayload
	name     string
	tokens   map[string]map[string]string
	origJSON string
	examples []client.DesignExample
	source   int
	target   string
	busy     bool
	notes    []string
}

func newDesignSystem(s *Settings) section { return &designSystem{s: s} }

func (d *designSystem) snap() string {
	b, _ := json.Marshal([]any{d.name, d.tokens})
	return string(b)
}

func (d *designSystem) seed(p client.DesignSystemPayload) {
	d.payload, d.name = p, p.System.Name
	d.tokens = map[string]map[string]string{}
	for g, m := range p.System.Tokens {
		d.tokens[g] = map[string]string{}
		for k, v := range m {
			d.tokens[g][k] = v
		}
	}
	d.origJSON = d.snap()
}

func (d *designSystem) Load() {
	if d.loading {
		return
	}
	d.loading = true
	a := d.s.app
	a.Go(func(ctx context.Context) {
		p, err := a.Client.DesignSystem(ctx)
		ex, _ := a.Client.DesignSystemExamples(ctx)
		a.Update(func() {
			d.loading = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.err, d.loaded, d.examples = "", true, ex
			d.seed(p)
		})
	})
}

func (d *designSystem) Dirty() bool { return d.loaded && d.snap() != d.origJSON }

func (d *designSystem) Discard() {
	if d.loaded {
		d.seed(d.payload)
	}
}

func (d *designSystem) Save(done func(bool)) {
	a := d.s.app
	d.saving, d.busy, d.err = true, true, ""
	name, tokens := d.name, d.tokens
	a.Go(func(ctx context.Context) {
		p, err := a.Client.SaveDesignSystem(ctx, name, tokens)
		a.Update(func() {
			d.saving, d.busy = false, false
			if err != nil {
				d.err = err.Error()
				a.Toasts.Error("Save failed", err.Error())
				done(false)
				return
			}
			d.seed(p)
			a.Toasts.Success("Design system saved", "")
			done(true)
		})
	})
}

func (d *designSystem) extract(dry bool) {
	a := d.s.app
	d.busy = true
	src := designSources[d.source].V
	target, name := d.target, d.name
	a.Go(func(ctx context.Context) {
		r, err := a.Client.ExtractDesignSystem(ctx, src, target, name, dry)
		a.Update(func() {
			d.busy = false
			if err != nil {
				a.Toasts.Error("Extraction failed", err.Error())
				return
			}
			d.notes = r.Result.Notes
			if r.System != nil {
				d.seed(*r.System)
			}
			switch {
			case r.MirrorError != "":
				a.Toasts.Error(r.MirrorError, "")
			case r.Saved:
				a.Toasts.Success(orStr("Extracted and mirrored to "+r.Mirrored, "Design system extracted"), "")
			}
		})
	})
}

func (d *designSystem) Build(c *ui.Context) {
	s, t := d.s, kit.Current()
	desc := ""
	if d.loaded && !d.payload.Exists {
		desc = "This project has not committed a design system yet. The values below are the neutral defaults; saving or extracting writes them."
	}
	s.pageHeader(c, s.app.T("settings.categories.designSystem"), desc)
	if !d.loaded {
		s.loadingView(c, &d.status)
		return
	}
	for _, l := range []string{"tokens: " + d.payload.TokensPath, "stylesheet: " + d.payload.StylesheetPath, "contract: " + d.payload.ContractPath} {
		ui.Text(c, l).FontSize(t.TextXS).TextColor(t.FgMuted)
	}
	kit.SettingsSection(c, "", "", func() {
		kit.SettingsRow(c, "Name", "", func() { kit.Input(c, &d.name, "").Width(260) })
	})
	kit.SettingsSection(c, "Extract from", "", func() {
		ui.Column(c).Padding(12, 16).Gap(10).Children(func() {
			ui.Segmented(c, &d.source, optLabels(designSources)...).Label("Extract from")
			ui.Row(c).Gap(8).Children(func() {
				kit.Input(c, &d.target, designHints[designSources[d.source].V]).Grow(1)
				if kit.Button(c.Key("ex"), kit.ButtonOpts{Label: "Extract", Variant: kit.Primary, Disabled: d.busy}).Clicked() {
					d.extract(false)
				}
				if kit.Button(c.Key("pv"), kit.ButtonOpts{Label: "Preview", Variant: kit.Secondary, Disabled: d.busy}).Clicked() {
					d.extract(true)
				}
			})
			if designSources[d.source].V == "text" && len(d.examples) > 0 {
				ui.Row(c).Gap(6).Wrap().Children(func() {
					for _, e := range d.examples {
						if kit.Button(c.Key("exm:"+e.Name), kit.ButtonOpts{Label: e.Name, Size: kit.SizeSM, Tooltip: e.Title}).Clicked() {
							d.target = e.Name
						}
					}
				})
			}
			for _, n := range d.notes {
				ui.Text(c, "• "+n).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
		})
	})
	groups := make([]string, 0, len(d.tokens))
	for g := range d.tokens {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	for _, g := range groups {
		toks := make([]string, 0, len(d.tokens[g]))
		for k := range d.tokens[g] {
			toks = append(toks, k)
		}
		sort.Strings(toks)
		gc := c.Key("grp:" + g)
		kit.SettingsSection(gc, g, "", func() {
			for _, k := range toks {
				v := d.tokens[g][k]
				rc := gc.Key(k)
				kit.SettingsRow(rc, "--"+g+"-"+k, "", func() {
					if g == "color" && hexColorRe.MatchString(strings.TrimSpace(v)) {
						ui.Box(rc).Width(24).Height(24).Radius(4).Background(ui.Hex(strings.TrimSpace(v))).Border(1, t.Border)
					}
					in := v
					kit.Input(rc, &in, "").Width(220)
					d.tokens[g][k] = in
				})
			}
		})
	}
}
