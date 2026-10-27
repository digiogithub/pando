package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const dmk = "settings.decisionModel."

var dmDefaultURL = map[string]string{"ollama": "http://localhost:11434", "typesafe": "https://api.typesafe.ai", "custom": ""}

var dmPresets = []struct{ id, label, baseURL, model string }{
	{"openrouter", "OpenRouter", "https://openrouter.ai/api", "typesafe/jev-1.13"},
	{"litellm", "LiteLLM", "http://localhost:4000/typesafe", ""},
	{"kev", "Kev", "http://localhost:8009", ""},
}

// isHostedDecisionProvider: anything but the local Ollama sends prompts off
// the machine, unless it points to a loopback host.
func isHostedDecisionProvider(provider, effectiveURL string) bool {
	if provider == "ollama" {
		return false
	}
	if effectiveURL != "" {
		if u, err := url.Parse(effectiveURL); err == nil {
			switch u.Hostname() {
			case "localhost", "127.0.0.1", "::1":
				return false
			}
		}
	}
	return true
}

type kv struct{ k, v string }

// decisionModel is the "Decision model" category (shared by Auto mode,
// persona auto-select and the context filter).
type decisionModel struct {
	status
	s *Settings

	orig         client.DecisionModel
	provider     string
	baseURL      string
	model        string
	keepAlive    string
	apiKey       string
	timeout      string
	headers      []kv
	newHK, newHV string
	clearKey     bool
	info         client.DecisionRouter
	warnings     []string
	fieldErrs    []client.FieldError

	providerSel, presetSel string

	rm        *client.RouterModels
	rmLoading bool
	showAll   bool
	pulls     map[string]client.RouterPullJob
	modelSel  string

	testing    bool
	testResult *client.RouterTest
}

func newDecisionModel(s *Settings) section {
	return &decisionModel{s: s, pulls: map[string]client.RouterPullJob{}}
}

func (d *decisionModel) providerLabels() []string {
	a := d.s.app
	return []string{a.T(dmk + "providers.ollama"), a.T(dmk + "providers.typesafe"), a.T(dmk + "providers.custom")}
}

var dmProviderIDs = []string{"ollama", "typesafe", "custom"}

func (d *decisionModel) Load() {
	if d.loading {
		return
	}
	d.loading = true
	a := d.s.app
	a.Go(func(ctx context.Context) {
		r, err := a.Client.GetDecisionModel(ctx)
		a.Update(func() {
			d.loading = false
			if err != nil {
				d.err, _ = client.ParseAPIError(err)
				return
			}
			d.err = ""
			d.seed(r)
		})
	})
}

func (d *decisionModel) seed(r client.DecisionModel) {
	d.orig = r
	if d.orig.Router.Provider == "" {
		d.orig.Router.Provider = "ollama"
	}
	d.orig.Router.APIKey = ""
	if d.orig.Router.Headers == nil {
		d.orig.Router.Headers = map[string]string{}
	}
	rt := d.orig.Router
	d.provider, d.baseURL, d.model, d.keepAlive, d.apiKey = rt.Provider, rt.BaseURL, rt.Model, rt.KeepAlive, ""
	d.timeout = strconv.Itoa(r.TimeoutMs)
	d.info = rt
	d.headers = d.headers[:0]
	for k, v := range rt.Headers {
		d.headers = append(d.headers, kv{k, v})
	}
	sortKV(d.headers)
	d.warnings, d.clearKey, d.fieldErrs, d.loaded = r.Warnings, false, nil, true
	d.providerSel = d.providerLabels()[indexOf(dmProviderIDs, d.provider)]
	d.modelSel = d.model
}

func sortKV(l []kv) {
	for i := 1; i < len(l); i++ {
		for j := i; j > 0 && l[j].k < l[j-1].k; j-- {
			l[j], l[j-1] = l[j-1], l[j]
		}
	}
}

func indexOf(l []string, s string) int {
	for i, x := range l {
		if x == s {
			return i
		}
	}
	return 0
}

func (d *decisionModel) draft() client.DecisionModel {
	h := map[string]string{}
	for _, e := range d.headers {
		h[e.k] = e.v
	}
	n, _ := strconv.Atoi(strings.TrimSpace(d.timeout))
	return client.DecisionModel{
		Router: client.DecisionRouter{Provider: d.provider, BaseURL: strings.TrimSpace(d.baseURL), Model: strings.TrimSpace(d.model),
			KeepAlive: strings.TrimSpace(d.keepAlive), Headers: h, APIKey: d.apiKey},
		TimeoutMs: n,
	}
}

func (d *decisionModel) Dirty() bool {
	if !d.loaded {
		return false
	}
	cur := d.draft()
	cur.Router.APIKey = ""
	a, _ := json.Marshal(cmpDM(cur))
	b, _ := json.Marshal(cmpDM(d.orig))
	return d.clearKey || d.apiKey != "" || string(a) != string(b)
}

func cmpDM(x client.DecisionModel) any {
	return struct {
		P, B, M, K string
		H          map[string]string
		T          int
	}{x.Router.Provider, x.Router.BaseURL, x.Router.Model, x.Router.KeepAlive, x.Router.Headers, x.TimeoutMs}
}

func (d *decisionModel) Discard() {
	if d.loaded {
		d.seed(d.orig)
	}
}

func (d *decisionModel) Save(done func(bool)) {
	if d.saving || !d.loaded {
		done(false)
		return
	}
	if _, err := strconv.Atoi(strings.TrimSpace(d.timeout)); err != nil {
		d.err = "Timeout must be a whole number"
		done(false)
		return
	}
	d.saving, d.err, d.fieldErrs = true, "", nil
	a := d.s.app
	draft, clear := d.draft(), d.clearKey
	a.Go(func(ctx context.Context) {
		r, err := a.Client.PutDecisionModel(ctx, draft, clear)
		a.Update(func() {
			d.saving = false
			if err != nil {
				msg, fe := client.ParseAPIError(err)
				d.err, d.fieldErrs = msg, fe
				a.Toasts.Error(msg, "")
				done(false)
				return
			}
			d.testResult = nil
			d.seed(r)
			a.Toasts.Success("Decision model settings saved", "")
			done(true)
		})
	})
}

func (d *decisionModel) errs(field string) []string {
	var out []string
	for _, e := range d.fieldErrs {
		if e.Field == "decisionModel."+field || e.Field == field {
			out = append(out, e.Message)
		}
	}
	return out
}

func (d *decisionModel) showErrs(c *ui.Context, errs []string) {
	for _, m := range errs {
		ui.Text(c, m).FontSize(kit.Current().TextXS).TextColor(kit.Current().Danger)
	}
}

func (d *decisionModel) routerDraft() client.DecisionRouter { return d.draft().Router }

func (d *decisionModel) loadModels() {
	d.rmLoading = true
	a, rt, all := d.s.app, d.routerDraft(), d.showAll
	a.Go(func(ctx context.Context) {
		r, err := a.Client.DecisionRouterModels(ctx, rt, all)
		a.Update(func() {
			d.rmLoading = false
			if err != nil {
				msg, _ := client.ParseAPIError(err)
				d.rm = &client.RouterModels{Status: "unsupported", Error: msg}
				return
			}
			d.rm = &r
		})
	})
}

func (d *decisionModel) pull(name string) {
	a, rt := d.s.app, d.routerDraft()
	a.Go(func(ctx context.Context) {
		set := func(j client.RouterPullJob) { a.Update(func() { d.pulls[name] = j }) }
		job, err := a.Client.DecisionRouterPull(ctx, name, rt)
		for err == nil {
			set(job)
			if job.State != "running" {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			job, err = a.Client.DecisionRouterPullStatus(ctx, job.ID)
		}
		if err != nil {
			msg, _ := client.ParseAPIError(err)
			set(client.RouterPullJob{Target: name, State: "error", Error: msg})
			return
		}
		if job.State == "done" {
			a.Update(d.loadModels)
		}
	})
}

func (d *decisionModel) test() {
	d.testing, d.testResult = true, nil
	a, rt := d.s.app, d.routerDraft()
	a.Go(func(ctx context.Context) {
		r, err := a.Client.DecisionRouterTest(ctx, rt)
		a.Update(func() {
			d.testing = false
			if err != nil {
				msg, _ := client.ParseAPIError(err)
				r = client.RouterTest{Error: msg}
			}
			d.testResult = &r
		})
	})
}

func (d *decisionModel) Build(c *ui.Context) {
	s, a, t := d.s, d.s.app, kit.Current()
	s.pageHeader(c, a.T(dmk+"title"), a.T(dmk+"description"))
	if !d.loaded {
		s.loadingView(c, &d.status)
		return
	}
	if d.err != "" && len(d.fieldErrs) == 0 {
		s.errorBanner(c, d.err)
	}
	for _, w := range d.warnings {
		ui.Text(c, w).FontSize(t.TextSM).TextColor(t.Warning)
	}
	defURL := dmDefaultURL[d.provider]
	if d.provider == d.orig.Router.Provider && d.info.EffectiveBaseURL != "" {
		defURL = d.info.EffectiveBaseURL
	}
	eff := d.baseURL
	if eff == "" {
		eff = defURL
	}
	kit.SettingsSection(c, a.T(dmk+"sections.provider"), a.T(dmk+"sections.providerDescription"), func() {
		kit.SettingsRow(c.Key("prov"), a.T(dmk+"provider"), "", func() {
			if kit.Select(c, &d.providerSel, d.providerLabels()).Width(260).Changed() {
				d.provider = dmProviderIDs[indexOf(d.providerLabels(), d.providerSel)]
				d.model, d.modelSel, d.rm = "", "", nil
			}
		})
		if d.provider == "custom" {
			kit.SettingsRow(c.Key("preset"), a.T(dmk+"preset"), a.T(dmk+"presetHint"), func() {
				labels := []string{a.T(dmk + "presetChoose")}
				for _, p := range dmPresets {
					labels = append(labels, p.label)
				}
				if kit.Select(c, &d.presetSel, labels).Width(260).Changed() {
					for _, p := range dmPresets {
						if p.label == d.presetSel {
							d.baseURL = p.baseURL
							if p.model != "" {
								d.model, d.modelSel = p.model, p.model
							}
						}
					}
				}
			})
		}
		kit.SettingsRow(c.Key("url"), a.T(dmk+"baseUrl"), a.T(dmk+"baseUrlHint"), func() {
			ui.Column(c).Gap(2).Children(func() {
				kit.Input(c.Key("urlin"), &d.baseURL, defURL).Width(320)
				d.showErrs(c, d.errs("router.baseURL"))
			})
		})
		if d.provider != "ollama" {
			desc := a.T(dmk + "apiKeyNone")
			if d.info.APIKeySet && !d.clearKey {
				desc = a.T(dmk+"apiKeyStored") + " " + d.info.APIKeyMasked + " " + a.T(dmk+"apiKeyReplace")
			}
			kit.SettingsRow(c.Key("key"), a.T(dmk+"apiKey"), desc, func() {
				ph := a.T(dmk + "apiKeyPlaceholder")
				if d.info.APIKeySet && !d.clearKey {
					ph = d.info.APIKeyMasked
				}
				if kit.Input(c.Key("keyin"), &d.apiKey, ph).Password().Width(240).Changed() && d.apiKey != "" {
					d.clearKey = false
				}
				if d.info.APIKeySet {
					lbl := a.T(dmk + "clearKey")
					if d.clearKey {
						lbl = a.T(dmk + "keepKey")
					}
					if kit.SecondaryButton(c.Key("clearkey"), lbl).Clicked() {
						d.clearKey = !d.clearKey
						if d.clearKey {
							d.apiKey = ""
						}
					}
				}
			})
			d.showErrs(c, d.errs("router.apiKey"))
		}
		if d.provider == "custom" {
			d.headersRow(c)
		}
		if d.provider == "ollama" {
			kit.SettingsRow(c.Key("ka"), a.T(dmk+"keepAlive"), a.T(dmk+"keepAliveHint"), func() {
				kit.Input(c, &d.keepAlive, "30m").Width(120)
			})
		}
		d.modelRow(c)
		if isHostedDecisionProvider(d.provider, eff) {
			ui.Text(c, a.T(dmk+"inUse.hosted")).FontSize(t.TextSM).TextColor(t.Warning).Padding(12, 16)
		}
		kit.SettingsRow(c.Key("to"), a.T(dmk+"timeout"), a.T(dmk+"timeoutHint"), func() {
			kit.Input(c, &d.timeout, "0").Width(120)
		})
		d.d_errs(c, "timeoutMs")
		kit.SettingsRow(c.Key("conn"), a.T(dmk+"connection"), "", func() {
			if kit.Button(c.Key("test"), kit.ButtonOpts{Label: a.T(dmk + "testConnection"), Variant: kit.Secondary, Disabled: d.testing}).Clicked() {
				d.test()
			}
		})
		d.testReport(c)
	})
}

func (d *decisionModel) d_errs(c *ui.Context, f string) { d.showErrs(c, d.errs(f)) }

func (d *decisionModel) headersRow(c *ui.Context) {
	a := d.s.app
	kit.SettingsRow(c.Key("hdr"), a.T(dmk+"headers"), a.T(dmk+"headersHint"), func() {
		ui.Column(c).Gap(6).Children(func() {
			for i := range d.headers {
				e := &d.headers[i]
				cc := c.Key("h:" + e.k)
				ui.Row(cc).Gap(6).Children(func() {
					kit.Input(cc.Key("n"), &e.k, "").ReadOnly(true).Width(140)
					kit.Input(cc.Key("v"), &e.v, "").Width(180)
					if kit.IconButton(cc.Key("rm"), kit.IconTrash, a.T(dmk+"removeHeader", "name", e.k)).Clicked() {
						d.headers = append(d.headers[:i:i], d.headers[i+1:]...)
					}
				})
			}
			ui.Row(c).Gap(6).Children(func() {
				kit.Input(c.Key("nhk"), &d.newHK, a.T(dmk+"headerName")).Width(140)
				kit.Input(c.Key("nhv"), &d.newHV, a.T(dmk+"headerValue")).Width(180)
				if kit.Button(c.Key("addh"), kit.ButtonOpts{Label: a.T(dmk + "addHeader"), Variant: kit.Secondary, Disabled: strings.TrimSpace(d.newHK) == ""}).Clicked() {
					k := strings.TrimSpace(d.newHK)
					replaced := false
					for i := range d.headers {
						if d.headers[i].k == k {
							d.headers[i].v, replaced = d.newHV, true
						}
					}
					if !replaced {
						d.headers = append(d.headers, kv{k, d.newHV})
						sortKV(d.headers)
					}
					d.newHK, d.newHV = "", ""
				}
			})
		})
	})
}

func (d *decisionModel) modelRow(c *ui.Context) {
	a, t := d.s.app, kit.Current()
	kit.SettingsRow(c.Key("model"), a.T(dmk+"model"), "", func() {
		ui.Column(c).Gap(6).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if d.rm != nil && d.rm.Status != "unsupported" {
					none := a.T(dmk + "modelSelect")
					labels := []string{none}
					found := d.model == ""
					for _, m := range d.rm.Models {
						labels = append(labels, m.ID)
						found = found || m.ID == d.model
					}
					if !found {
						labels = append(labels, d.model)
					}
					if d.modelSel == "" {
						d.modelSel = none
					}
					if kit.Select(c.Key("msel"), &d.modelSel, labels).Width(260).Changed() {
						d.model = d.modelSel
						if d.modelSel == none {
							d.model = ""
						}
					}
				} else if kit.Input(c.Key("min"), &d.model, a.T(dmk+"modelPlaceholder")).Width(260).Changed() {
					d.modelSel = d.model
				}
				if kit.Button(c.Key("load"), kit.ButtonOpts{Label: a.T(dmk + "loadModels"), Variant: kit.Secondary, Disabled: d.rmLoading}).Clicked() {
					d.loadModels()
				}
			})
			d.d_errs(c, "router.model")
			rm := d.rm
			if rm == nil {
				return
			}
			switch rm.Status {
			case "unsupported":
				ui.Text(c, a.T(dmk+"unsupportedHint")).FontSize(t.TextXS).TextColor(t.FgMuted)
			case "unfiltered":
				ui.Text(c, a.T(dmk+"unfilteredHint")).FontSize(t.TextXS).TextColor(t.FgMuted)
			case "filtered":
				before := d.showAll
				kit.Switch(c.Key("showall"), &d.showAll, a.T(dmk+"showAll"))
				if d.showAll != before {
					d.loadModels()
				}
			}
			if len(rm.Models) == 0 && d.provider == "ollama" && rm.Status != "unsupported" {
				ui.Text(c, a.T(dmk+"noModels")+" ollama pull tev1:0.8b.").FontSize(t.TextXS).TextColor(t.FgMuted)
			}
			if d.provider == "ollama" && len(rm.Suggestions) > 0 {
				ui.Text(c, a.T(dmk+"suggested")).FontSize(t.TextXS).TextColor(t.FgMuted)
				for _, name := range rm.Suggestions {
					job, has := d.pulls[name]
					running := has && job.State == "running"
					cc := c.Key("pull:" + name)
					ui.Row(cc).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(cc, name).FontSize(t.TextSM).Width(180)
						if kit.Button(cc.Key("b"), kit.ButtonOpts{Label: a.T(dmk + "pull"), Variant: kit.Secondary, Size: kit.SizeSM, Disabled: running}).Clicked() {
							d.pull(name)
						}
						switch {
						case running:
							txt := job.Status
							if txt == "" {
								txt = a.T(dmk + "pullStarting")
							}
							if job.Total > 0 {
								txt += fmt.Sprintf(" %d%%", job.Completed*100/job.Total)
							}
							ui.Text(cc, txt).FontSize(t.TextXS).TextColor(t.FgMuted)
						case has && job.State == "error":
							ui.Text(cc, job.Error).FontSize(t.TextXS).TextColor(t.Danger)
						}
					})
				}
			}
			if rm.Hint != "" {
				ui.Text(c, rm.Hint).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
			if rm.Error != "" {
				ui.Text(c, rm.Error).FontSize(t.TextXS).TextColor(t.Danger)
			}
		})
	})
}

func (d *decisionModel) testReport(c *ui.Context) {
	r := d.testResult
	if r == nil {
		return
	}
	a, t := d.s.app, kit.Current()
	check := func(ok bool, label string) {
		mark, col := "✓", t.Success
		if !ok {
			mark, col = "✗", t.Danger
		}
		ui.Row(c).Gap(8).Padding(2, 16).Children(func() {
			ui.Text(c, mark).TextColor(col).FontWeight(700)
			ui.Text(c, label).FontSize(t.TextSM)
		})
	}
	if r.Error != "" {
		ui.Text(c, r.Error).FontSize(t.TextSM).TextColor(t.Danger).Padding(2, 16)
	}
	if rep := r.Report; rep != nil {
		check(rep.Reachable, a.T(dmk+"report.reachable"))
		check(rep.Authorized, a.T(dmk+"report.authorized"))
		if rep.Kind == "ollama" {
			l := a.T(dmk + "report.version")
			if rep.Version != "" {
				l += " (" + a.T(dmk+"report.found", "version", rep.Version) + ")"
			}
			check(rep.VersionOK, l)
		}
		check(rep.ModelPresent, a.T(dmk+"report.modelPresent"))
		check(rep.IsDecisionModel, a.T(dmk+"report.decisionCapable"))
		ui.Text(c, fmt.Sprintf("%s: %d ms", a.T(dmk+"report.latency"), rep.LatencyMs)).FontSize(t.TextSM).TextColor(t.FgMuted).Padding(2, 16)
	}
	for _, p := range r.Problems {
		ui.Text(c, p).FontSize(t.TextSM).TextColor(t.Danger).Padding(2, 16)
	}
	if r.OK {
		kit.Badge(c, a.T(dmk+"health.healthy"), kit.ToneSuccess)
	}
}

// ---- "decision model in use" row shared by Auto mode ----

type dmInUse struct {
	loaded bool
	cfg    client.DecisionModel
	health *client.HealthReport
	herr   string
}

func (u *dmInUse) load(s *Settings) {
	a := s.app
	a.Go(func(ctx context.Context) {
		cfg, err := a.Client.GetDecisionModel(ctx)
		if err != nil {
			return
		}
		a.Update(func() { u.cfg, u.loaded = cfg, true })
		if strings.TrimSpace(cfg.Router.Model) == "" {
			return
		}
		h, herr := a.Client.DecisionRouterHealth(ctx)
		a.Update(func() {
			u.health, u.herr = h.Report, h.Error
			if herr != nil {
				u.health, u.herr = nil, herr.Error()
			}
		})
	})
}

func (u *dmInUse) build(c *ui.Context, s *Settings, consumerEnabled bool, missingNote string) {
	a, t := s.app, kit.Current()
	model := strings.TrimSpace(u.cfg.Router.Model)
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, a.T(dmk+"inUse.label")+":").FontSize(t.TextSM).TextColor(t.FgMuted)
			switch {
			case !u.loaded:
				ui.Text(c, "…").FontSize(t.TextSM)
			case model != "":
				ui.Text(c, u.cfg.Router.Provider+"/"+model).FontSize(t.TextSM)
				if u.health != nil {
					tone, lbl := kit.ToneSuccess, a.T(dmk+"health.healthy")
					if !u.health.OK {
						tone, lbl = kit.ToneDanger, a.T(dmk+"health.unhealthy")
					}
					kit.Badge(c, lbl, tone)
				} else if u.herr != "" {
					kit.Badge(c, u.herr, kit.ToneDanger)
				}
			default:
				ui.Text(c, a.T(dmk+"inUse.none")).FontSize(t.TextSM).TextColor(t.FgMuted)
			}
			if kit.SecondaryButton(c.Key("dm-config"), a.T(dmk+"inUse.configure")+" →").Clicked() {
				s.Select("decision-model")
			}
		})
		if u.loaded && model == "" && consumerEnabled {
			ui.Text(c, a.T(dmk+"inUse.notConfigured")+" "+missingNote).FontSize(t.TextSM).TextColor(t.Warning)
		}
		if model != "" && u.health != nil && !u.health.OK && len(u.health.Problems) > 0 {
			ui.Text(c, strings.Join(u.health.Problems, "; ")).FontSize(t.TextXS).TextColor(t.FgMuted)
		}
		if model != "" && isHostedDecisionProvider(u.cfg.Router.Provider, u.cfg.Router.EffectiveBaseURL) {
			ui.Text(c, a.T(dmk+"inUse.hosted")).FontSize(t.TextXS).TextColor(t.Warning)
		}
	})
}
