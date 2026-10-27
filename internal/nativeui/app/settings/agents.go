package settings

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

var agentNames = []string{"coder", "summarizer", "task", "title", "cli-assist", "persona-selector", "context-enricher"}

var agentLabels = map[string]string{"coder": "Coder", "summarizer": "Summarizer", "task": "Task", "title": "Title",
	"cli-assist": "CLI Assist", "persona-selector": "Persona Selector", "context-enricher": "Content Enricher"}

var agentDescs = map[string]string{
	"coder": "Main coding and problem-solving agent", "summarizer": "Summarizes sessions and content",
	"task": "Manages and executes tasks", "title": "Generates session titles",
	"cli-assist": "Assists with CLI and terminal tasks", "persona-selector": "Selects and switches personas automatically",
	"context-enricher": "Plans and enriches prompts with relevant remembered context before retrieval",
}

var agentAutoBudget = map[string]int{"title": 80, "persona-selector": 64, "cli-assist": 256, "context-enricher": 256, "task": 2048, "summarizer": 4096, "coder": 8192}

var reasoningOpts = []Opt{{"", "Default"}, {"none", "None"}, {"low", "Low"}, {"medium", "Medium"}, {"high", "High"}}
var thinkingOpts = []Opt{{"", "Default"}, {"disabled", "Disabled"}, {"low", "Low (20% budget)"}, {"medium", "Medium (50% budget)"}, {"high", "High (80% budget)"}}

// agentEdit is the editable state of one agent. raw keeps unknown fields.
type agentEdit struct {
	raw                     client.Obj
	name, model             string
	maxTokens, threshold    string
	reasoning, thinking     string // option labels
	autoCompact, useDecison bool
	open                    bool
}

type agentsSection struct {
	status
	s      *Settings
	agents []*agentEdit
	orig   string
	models []string
	dmInfo string
}

func newAgents(s *Settings) section { return &agentsSection{s: s} }

func optLabel(opts []Opt, v string) string {
	for _, o := range opts {
		if o.V == v {
			return o.L
		}
	}
	return v
}
func optValue(opts []Opt, l string) string {
	for _, o := range opts {
		if o.L == l {
			return o.V
		}
	}
	return l
}

func numStr(v any) string {
	if v == nil {
		return "0"
	}
	return scalarString(v)
}

func (a *agentsSection) snapshot() string {
	var sb strings.Builder
	for _, e := range a.agents {
		b, _ := json.Marshal([]any{e.name, e.model, strings.TrimSpace(e.maxTokens), strings.TrimSpace(e.threshold), e.reasoning, e.thinking, e.autoCompact, e.useDecison})
		sb.Write(b)
	}
	return sb.String()
}

func (a *agentsSection) seed(list []client.Obj) {
	byName := map[string]client.Obj{}
	for _, o := range list {
		byName[strings.ToLower(ostr(o, "name"))] = o
	}
	a.agents = nil
	for _, n := range agentNames {
		o, ok := byName[n]
		if !ok {
			o = client.Obj{"name": n, "contextControls": n == "coder"}
		}
		e := &agentEdit{raw: o, name: n, model: ostr(o, "model"), maxTokens: numStr(o["maxTokens"]),
			threshold: numStr(o["autoCompactThreshold"]), reasoning: optLabel(reasoningOpts, ostr(o, "reasoningEffort")),
			thinking: optLabel(thinkingOpts, ostr(o, "thinkingMode")), autoCompact: obool(o, "autoCompact"), useDecison: obool(o, "useDecisionModel")}
		a.agents = append(a.agents, e)
	}
	a.orig = a.snapshot()
}

func (a *agentsSection) Load() {
	if a.loading {
		return
	}
	a.loading = true
	app := a.s.app
	app.Go(func(ctx context.Context) {
		list, err := app.Client.Agents(ctx)
		ml, _ := app.Client.ListModels(ctx)
		prov, model, _ := app.Client.DecisionModelRouter(ctx)
		app.Update(func() {
			a.loading = false
			if err != nil {
				a.err = err.Error()
				return
			}
			a.err, a.loaded = "", true
			a.seed(list)
			a.models = []string{""}
			for _, m := range ml.Models {
				a.models = append(a.models, m.ID)
			}
			if model != "" {
				a.dmInfo = prov + "/" + model
			} else {
				a.dmInfo = ""
			}
		})
	})
}

func (a *agentsSection) Dirty() bool { return a.loaded && a.snapshot() != a.orig }

func (a *agentsSection) Discard() {
	list := make([]client.Obj, len(a.agents))
	for i, e := range a.agents {
		list[i] = e.raw
	}
	a.seed(list)
}

func (a *agentsSection) payload() ([]client.Obj, error) {
	var out []client.Obj
	for _, e := range a.agents {
		o := deepCopy(e.raw)
		o["name"], o["model"] = e.name, e.model
		o["reasoningEffort"], o["thinkingMode"] = optValue(reasoningOpts, e.reasoning), optValue(thinkingOpts, e.thinking)
		n, err := strconv.ParseInt(strings.TrimSpace(e.maxTokens), 10, 64)
		if err != nil {
			return nil, errBad(e.name + ": max tokens")
		}
		th, err := strconv.ParseFloat(strings.TrimSpace(e.threshold), 64)
		if err != nil {
			return nil, errBad(e.name + ": compact threshold")
		}
		o["maxTokens"], o["autoCompact"], o["autoCompactThreshold"] = n, e.autoCompact, th
		if e.name == "persona-selector" {
			o["useDecisionModel"] = e.useDecison
		}
		out = append(out, o)
	}
	return out, nil
}

type errBad string

func (e errBad) Error() string { return "invalid number: " + string(e) }

func (a *agentsSection) Save(done func(bool)) {
	app := a.s.app
	body, err := a.payload()
	if err != nil {
		a.err = err.Error()
		app.Toasts.Error(a.err, "")
		done(false)
		return
	}
	a.saving, a.err = true, ""
	app.Go(func(ctx context.Context) {
		list, err := app.Client.SaveAgents(ctx, body)
		app.Update(func() {
			a.saving = false
			if err != nil {
				a.err = err.Error()
				app.Toasts.Error("Save failed", err.Error())
				done(false)
				return
			}
			a.seed(list)
			app.Toasts.Success("Agents saved", "")
			done(true)
		})
	})
}

func (a *agentsSection) Build(c *ui.Context) {
	s, t := a.s, kit.Current()
	s.pageHeader(c, s.app.T("settings.categories.agents"), "Configure model and behavior for each built-in agent. Changes apply to new sessions.")
	if !a.loaded {
		s.loadingView(c, &a.status)
		return
	}
	for _, e := range a.agents {
		a.card(c.Key("ag:"+e.name), e, t)
	}
}

func (a *agentsSection) card(c *ui.Context, e *agentEdit, t *kit.Tokens) {
	s := a.s
	showCtx := obool(e.raw, "contextControls")
	if _, has := e.raw["contextControls"]; !has {
		showCtx = e.name == "coder"
	}
	ui.Column(c).Radius(t.RadiusMD).Background(t.BgCard).Border(1, t.Border).Children(func() {
		head := ui.ButtonBase(c.Key("head")).Label(agentLabels[e.name]).Padding(12, 16).Gap(12).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		head.Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, agentLabels[e.name]).FontWeight(600).FontSize(t.TextMD)
				ui.Text(c, agentDescs[e.name]).FontSize(t.TextXS).TextColor(t.FgMuted)
			})
			if e.model != "" {
				kit.Badge(c, e.model, kit.ToneNeutral)
			}
			ic := kit.IconChevronDown
			if e.open {
				ic = kit.IconChevronUp
			}
			kit.Icon(c, ic)
		})
		if head.Clicked() {
			e.open = !e.open
		}
		if !e.open {
			return
		}
		ui.Column(c).Padding(12, 16, 16, 16).Gap(12).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			label := func(l string, ctl func()) {
				ui.Column(c).Gap(4).Children(func() { ui.Text(c, l).FontSize(t.TextXS).TextColor(t.FgMuted); ctl() })
			}
			if e.name == "persona-selector" {
				kit.Switch(c.Key("udm"), &e.useDecison, "Use decision model")
				ui.Text(c, "Pick the persona with the shared decision model instead of this agent's model.").FontSize(t.TextXS).TextColor(t.FgMuted)
				if e.useDecison {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						if a.dmInfo != "" {
							ui.Text(c, s.t("Decision model", "settings.decisionModel.inUse.label")+": "+a.dmInfo).FontSize(t.TextSM)
						} else {
							ui.Text(c, s.t("No decision model is configured.", "settings.decisionModel.inUse.notConfigured")+" The fallback model is used instead.").FontSize(t.TextSM).TextColor(t.Warning)
						}
						if kit.SecondaryButton(c.Key("cfg"), s.t("Configure", "settings.decisionModel.inUse.configure")+" →").Clicked() {
							s.Select("decision-model")
						}
					})
				}
			}
			ml := "Model"
			if e.useDecison {
				ml = "Fallback model"
			}
			label(ml, func() {
				opts := a.models
				if e.model != "" && !contains(opts, e.model) {
					opts = append(append([]string{}, opts...), e.model)
				}
				kit.Select(c.Key("model"), &e.model, opts).Width(360)
			})
			ui.Row(c).Gap(16).Wrap().Children(func() {
				if showCtx {
					label("Max tokens (0 = Auto)", func() {
						kit.Input(c.Key("mt"), &e.maxTokens, "0").Width(140)
						if strings.TrimSpace(e.maxTokens) == "0" {
							eff := agentAutoBudget[e.name]
							if r, ok := e.raw["resolvedMaxTokens"]; ok {
								if n, err := strconv.Atoi(scalarString(r)); err == nil {
									eff = n
								}
							}
							if eff == 0 {
								eff = 4096
							}
							ui.Text(c, "Auto — effective: "+strconv.Itoa(eff)+" tokens").FontSize(t.TextXS).TextColor(t.FgMuted)
						}
					})
				}
				label("Reasoning effort", func() { kit.Select(c.Key("re"), &e.reasoning, optLabels(reasoningOpts)).Width(180) })
				label("Thinking mode", func() { kit.Select(c.Key("tm"), &e.thinking, optLabels(thinkingOpts)).Width(200) })
			})
			if showCtx {
				ui.Row(c).Gap(24).AlignItems(ui.Center).Children(func() {
					kit.Switch(c.Key("ac"), &e.autoCompact, "Auto-compact")
					label("Compact threshold (0-1)", func() { kit.Input(c.Key("ct"), &e.threshold, "0.8").Width(120) })
				})
			}
		})
	})
}

func optLabels(o []Opt) []string {
	out := make([]string, len(o))
	for i, x := range o {
		out[i] = x.L
	}
	return out
}
