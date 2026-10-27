// Package evaluator is the self-improvement area: metrics cards, the daily
// evaluations chart, the UCB ranking of prompt variants, learned skills with
// approve/reject, the evaluated sessions with their score explanation and
// the doctor banner. It mirrors web-ui/src/components/evaluator.
package evaluator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
)

func init() {
	app.Register(app.Area{
		ID: "evaluator", Path: "/evaluator", TitleKey: "nav.selfImprovement",
		Icon: kit.IconEvaluator, Section: app.SectionTools, Order: 6,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

// Evaluator is the area's view.
type Evaluator struct {
	app *app.Context

	started bool
	loading bool
	loaded  bool
	err     string

	metrics  *client.EvaluatorMetrics
	sections []client.TemplateSection
	skills   []client.Skill
	sessions []client.EvaluatorSessionScore
	doctor   *client.EvaluatorDoctor

	tab        int // 0 variants and skills, 1 sessions
	skillTab   int // 0 pending, 1 approved, 2 rejected
	skillTabOK bool
	busySkill  string
	showReport bool
	open       map[string]bool
}

// New returns the evaluator view.
func New(a *app.Context) *Evaluator {
	return &Evaluator{app: a, open: map[string]bool{}}
}

var skillStatuses = []string{"pending", "approved", "rejected"}

// Refresh reloads everything from the server.
func (e *Evaluator) Refresh() {
	if e.loading {
		return
	}
	e.loading = true
	e.app.Go(func(ctx context.Context) {
		cl := e.app.Client
		m, merr := cl.EvaluatorMetrics(ctx)
		sec, _ := cl.EvaluatorTemplates(ctx)
		sk, _ := cl.EvaluatorSkills(ctx)
		ss, _ := cl.EvaluatorSessions(ctx)
		doc, derr := cl.EvaluatorDoctor(ctx)
		e.app.Update(func() {
			e.loading, e.loaded = false, true
			e.err = ""
			if merr != nil {
				e.err = merr.Error()
				e.metrics = nil
			} else {
				e.metrics = &m
			}
			e.sections, e.skills, e.sessions = sec, sk, ss
			e.doctor = nil
			if derr == nil {
				e.doctor = &doc
			}
			if !e.skillTabOK {
				e.skillTabOK = true
				e.skillTab = 1
				for _, s := range sk {
					if s.Status == "pending" {
						e.skillTab = 0
						break
					}
				}
			}
		})
	})
}

func (e *Evaluator) review(id, decision string) {
	e.busySkill = id
	e.app.Go(func(ctx context.Context) {
		err := e.app.Client.ReviewSkill(ctx, id, decision)
		e.app.Update(func() {
			e.busySkill = ""
			if err != nil {
				e.app.Toasts.Error("Skill review failed", err.Error())
				return
			}
			e.loading = false
			e.Refresh()
		})
	})
}

// HeaderActions adds the refresh button.
func (e *Evaluator) HeaderActions(c *ui.Context) {
	if kit.IconButton(c, kit.IconRefresh, "Refresh").Clicked() {
		e.Refresh()
	}
}

// Build draws the view.
func (e *Evaluator) Build(c *ui.Context, _ *ui.Route) {
	if !e.started {
		e.started = true
		e.Refresh()
	}
	t := kit.Current()
	if !e.loaded {
		ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Children(func() { kit.Spinner(c).FontSize(26) })
		return
	}
	ui.Scroll(c).Grow(1).Padding(16, 24, 24, 24).Gap(12).Children(func() {
		if e.err != "" {
			ui.Text(c, e.err).FontSize(t.TextSM).TextColor(t.Danger)
		}
		e.doctorBanner(c, t)
		e.metricsCards(c, t)
		if e.metrics != nil && len(e.metrics.Daily) > 0 {
			e.dailyChart(c, t, e.metrics.Daily)
		}
		ui.Segmented(c, &e.tab, "Variants and skills", fmt.Sprintf("Sessions (%d)", len(e.sessions))).Label("Self-improvement sections")
		if e.tab == 0 {
			ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
				e.ucbTable(c, t)
				e.skillsPanel(c, t)
			})
		} else {
			e.sessionsList(c, t)
		}
	})
}

func (e *Evaluator) doctorBanner(c *ui.Context, t *kit.Tokens) {
	d := e.doctor
	if d == nil {
		return
	}
	disabled := !d.Report.Enabled
	if d.Problem == "" && !disabled {
		return
	}
	msg := d.Problem
	if disabled {
		msg = "The self-improvement evaluator is disabled."
		if len(d.Report.DisabledReasons) > 0 {
			msg = d.Report.DisabledReasons[0]
		}
	}
	fg, bg := kit.ToneColors(kit.ToneDanger)
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Gap(12).Padding(10, 14).Radius(t.RadiusMD).Background(bg).Border(1, fg).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, kit.IconWarning).TextColor(fg).FontSize(16)
			ui.Text(c, msg).FontSize(t.TextSM).TextColor(fg).Grow(1).Shrink(1)
			label := "Show report"
			if e.showReport {
				label = "Hide report"
			}
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Label: label}).Clicked() {
				e.showReport = !e.showReport
			}
		})
		if e.showReport {
			ui.Column(c).Padding(12).Radius(t.RadiusMD).Background(t.BgShell).Border(1, t.Border).Children(func() {
				ui.Text(c, d.Text).FontSize(t.TextXS).Font(t.MonoFont)
			})
		}
	})
}

func (e *Evaluator) metricsCards(c *ui.Context, t *kit.Tokens) {
	m := e.metrics
	if m == nil {
		m = &client.EvaluatorMetrics{}
	}
	judgeK := float64(m.JudgePromptTokens+m.JudgeCompletionTokens) / 1000
	cards := []struct{ icon, label, value, desc string }{
		{kit.IconEvaluator, "Sessions Evaluated", fmt.Sprint(m.TotalSessions), "Total sessions scored by self-improvement"},
		{kit.IconOrchestrate, "Prompt Templates", fmt.Sprint(m.TotalTemplates), "Prompt variants with statistics"},
		{kit.IconSuccess, "Avg Reward Score", fmt.Sprintf("%.2f", m.AvgReward), "Mean reward across all evaluated sessions"},
		{kit.IconBot, "Judge Usage (14d)", fmt.Sprint(m.JudgeCalls), fmt.Sprintf("%.1fk tokens in judge calls", judgeK)},
	}
	ui.Row(c).Gap(12).AlignItems(ui.Stretch).Children(func() {
		for _, cd := range cards {
			kit.Card(c, func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					kit.Icon(c, cd.icon).FontSize(16).TextColor(t.Accent)
					ui.Text(c, cd.label).FontSize(t.TextSM).TextColor(t.FgMuted)
				})
				ui.Text(c, cd.value).FontSize(26).FontWeight(700)
				ui.Text(c, cd.desc).FontSize(t.TextXS).TextColor(t.FgFaint)
			}).Grow(1).Basis(0)
		}
	})
}

func (e *Evaluator) dailyChart(c *ui.Context, t *kit.Tokens, daily []client.EvaluatorDaily) {
	labels := make([]string, len(daily))
	vals := make([]float64, len(daily))
	for i, d := range daily {
		labels[i] = d.Day
		if len(d.Day) >= 10 {
			labels[i] = d.Day[5:]
		}
		vals[i] = float64(d.Evaluations)
	}
	_, hi := chart.Extent(vals...)
	if hi < 1 {
		hi = 1
	}
	x := chart.NewBand(labels...).Padding(0.25, 0.1)
	y := chart.NewLinear(0, hi).Nice(4)
	yt := y.Ticks(4)
	// Show about 7 x labels.
	var xt []chart.Tick
	step := max(1, len(daily)/7)
	for i, tk := range x.Ticks() {
		if i%step == 0 {
			xt = append(xt, tk)
		}
	}
	kit.Card(c, func() {
		ui.Text(c, fmt.Sprintf("Evaluations per day (last %d days). Hover a bar for the day's mean reward.", len(daily))).
			FontSize(t.TextXS).FontWeight(600).TextColor(t.FgMuted)
		chart.Plot(c, chart.Axes{Left: yt, Bottom: xt, Label: func(_ chart.Side, tk chart.Tick) {
			ui.Text(c, tk.Label).NoWrap().FontSize(10).TextColor(t.FgFaint)
		}}, func(area ui.Element) {
			area.Draw(func(p *ui.Painter, r ui.Rect) {
				for _, tk := range yt {
					p.Line(r.X, chart.Y(r, tk.At), r.X+r.W, chart.Y(r, tk.At), 1, t.Border)
				}
			})
			for i, d := range daily {
				h := y.Span(0, vals[i])
				bar := chart.Rect(c, x.Band(i), h).Radius(3).Background(t.Accent)
				if d.Evaluations == 0 {
					bar = chart.Rect(c, x.Band(i), y.Span(0, hi*0.02)).Radius(2).Background(t.Accent.Alpha(0.25))
				}
				bar.Tooltip(fmt.Sprintf("%s: %d evaluations, mean reward %.2f, judge %d calls", d.Day, d.Evaluations, d.AvgReward, d.JudgeCalls))
			}
		}).Label("Evaluations per day").Height(130).Gap(6)
	})
}

// cell widths of the UCB table.
var ucbCols = []struct {
	title string
	w     float32
}{{"Section", 110}, {"Variant", 0}, {"UCB Score", 80}, {"Avg Reward", 90}, {"Uses", 50}, {"File", 0}}

func (e *Evaluator) ucbTable(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Grow(2).Basis(0).MinWidth(0).Radius(t.RadiusMD).Border(1, t.Border).Background(t.BgCard).Children(func() {
		panelHeader(c, t, "Prompt Variants — UCB Ranking")
		if len(e.sections) == 0 {
			kit.EmptyState(c, kit.IconOrchestrate, "No prompt variants yet",
				"Add files under .pando/prompts/variants/<section>/<variant>.md.tpl to A/B test a prompt section.")
			return
		}
		row := func(cells []string, head bool) {
			ui.Row(c).Gap(8).Padding(6, 14).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
				for i, s := range cells {
					tx := ui.Text(c, s).FontSize(t.TextXS).SingleLine().Ellipsis("…")
					if head {
						tx.FontWeight(600).TextColor(t.FgMuted)
					}
					if w := ucbCols[i].w; w > 0 {
						tx.Width(w).Shrink(0)
					} else {
						tx.Grow(1).Basis(0).MinWidth(0)
					}
				}
			})
		}
		heads := make([]string, len(ucbCols))
		for i, cc := range ucbCols {
			heads[i] = cc.title
		}
		row(heads, true)
		for _, sec := range e.sections {
			vs := append([]client.TemplateVariant(nil), sec.Variants...)
			sort.SliceStable(vs, func(i, j int) bool { return vs[i].UCBScore > vs[j].UCBScore })
			for _, v := range vs {
				name := v.Name
				if v.Missing {
					name += " (file removed)"
				}
				ucb, avg := "-", "-"
				if v.TimesUsed > 0 {
					ucb = fmt.Sprintf("%.2f", v.UCBScore)
					avg = fmt.Sprintf("%.0f%%", v.AvgReward*100)
				}
				file := v.Path
				if v.IsDefault {
					file = "embedded"
				}
				row([]string{sec.Section, name, ucb, avg, fmt.Sprint(v.TimesUsed), file}, false)
			}
		}
	})
}

func panelHeader(c *ui.Context, t *kit.Tokens, title string, actions ...func()) {
	ui.Row(c).Gap(8).Padding(10, 14).AlignItems(ui.Center).Background(t.BgShell).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, title).FontSize(t.TextSM).FontWeight(600).Grow(1)
		for _, a := range actions {
			a()
		}
	})
}

func (e *Evaluator) skillsPanel(c *ui.Context, t *kit.Tokens) {
	pending := 0
	for _, s := range e.skills {
		if s.Status == "pending" {
			pending++
		}
	}
	status := skillStatuses[e.skillTab]
	var shown []client.Skill
	for _, s := range e.skills {
		if s.Status == status {
			shown = append(shown, s)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool {
		if shown[i].SuccessRate != shown[j].SuccessRate {
			return shown[i].SuccessRate > shown[j].SuccessRate
		}
		return shown[i].Uses > shown[j].Uses
	})
	ui.Column(c).Grow(1).Basis(0).MinWidth(280).Radius(t.RadiusMD).Border(1, t.Border).Background(t.BgCard).Children(func() {
		panelHeader(c, t, "Learned Skills", func() {
			for i, st := range skillStatuses {
				label := st
				if st == "pending" && pending > 0 {
					label = fmt.Sprintf("%s (%d)", st, pending)
				}
				v := kit.Ghost
				if e.skillTab == i {
					v = kit.Secondary
				}
				if kit.Button(c.Key("skilltab:"+st), kit.ButtonOpts{Variant: v, Size: kit.SizeSM, Label: label}).Clicked() {
					e.skillTab = i
				}
			}
		})
		if len(shown) == 0 {
			kit.EmptyState(c, kit.IconEvaluator, "No "+status+" skills",
				"The judge proposes skills from decisive sessions. Approved skills reach new sessions only.")
			return
		}
		for _, s := range shown {
			e.skillCard(c, t, s)
		}
	})
}

func (e *Evaluator) skillCard(c *ui.Context, t *kit.Tokens, s client.Skill) {
	tone := kit.ToneNeutral
	switch s.Status {
	case "pending":
		tone = kit.ToneWarning
	case "approved":
		tone = kit.ToneSuccess
	}
	ui.Column(c.Key("skill:"+s.ID)).Gap(6).Padding(12, 14).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, s.Name).FontSize(t.TextXS).FontWeight(600).Font(t.MonoFont).SingleLine().Ellipsis("…").Grow(1).Shrink(1).Tooltip(s.ID)
			kit.Badge(c, s.Status, tone)
			kit.Badge(c, fmt.Sprintf("%d uses", s.Uses), kit.ToneNeutral)
		})
		if s.Status == "approved" && s.EvalCount > 0 {
			ui.Progress(c, clamp01(s.SuccessRate)).Label("Success rate")
		}
		if s.Description != "" {
			ui.Text(c, s.Description).FontSize(t.TextXS).TextColor(t.FgMuted).MaxLines(3)
		}
		busy := e.busySkill == s.ID
		if s.Status == "pending" || s.Status == "approved" {
			ui.Row(c).Gap(8).Children(func() {
				if s.Status == "pending" {
					if kit.Button(c.Key("approve:"+s.ID), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Label: "Approve", Disabled: busy}).Clicked() {
						e.review(s.ID, "approve")
					}
				}
				if kit.Button(c.Key("reject:"+s.ID), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: "Reject", Disabled: busy}).Clicked() {
					e.review(s.ID, "reject")
				}
			})
		}
	})
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func rewardTone(r float64) kit.Tone {
	switch {
	case r >= 0.7:
		return kit.ToneSuccess
	case r >= 0.4:
		return kit.ToneWarning
	}
	return kit.ToneDanger
}

func (e *Evaluator) sessionsList(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Radius(t.RadiusMD).Border(1, t.Border).Background(t.BgCard).Children(func() {
		panelHeader(c, t, "Recent evaluated sessions")
		if len(e.sessions) == 0 {
			kit.EmptyState(c, kit.IconEvaluator, "No sessions evaluated yet",
				"Sessions are scored when you switch away from them, after they go idle, and by the startup backfill. See the warning above or run `pando evaluator doctor`.")
			return
		}
		for _, s := range e.sessions {
			e.sessionRow(c, t, s)
		}
	})
}

func (e *Evaluator) sessionRow(c *ui.Context, t *kit.Tokens, s client.EvaluatorSessionScore) {
	open := e.open[s.ID]
	title := s.Title
	if title == "" {
		title = s.SessionID
	}
	ui.Column(c.Key("sess:"+s.ID)).Padding(10, 14).Gap(8).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		head := ui.Row(c).Gap(12).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(title)
		if head.Clicked() {
			e.open[s.ID] = !open
		}
		head.Children(func() {
			icon := kit.IconChevronR
			if open {
				icon = kit.IconChevronDown
			}
			kit.Icon(c, icon).FontSize(14).TextColor(t.FgFaint)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, title).FontSize(t.TextSM).FontWeight(600).SingleLine().Ellipsis("…")
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					meta := fmt.Sprintf("%s  ·  %d messages  ·  %d correction", time.Unix(s.EvaluatedAt, 0).Format("2006-01-02 15:04"), s.MessageCount, s.Corrections)
					if s.Corrections != 1 {
						meta += "s"
					}
					ui.Text(c, meta).FontSize(t.TextXS).TextColor(t.FgMuted)
					if s.Feedback != "" {
						tone := kit.ToneDanger
						if s.Feedback == "good" {
							tone = kit.ToneSuccess
						}
						kit.Badge(c, "feedback: "+s.Feedback, tone)
					}
					if s.JudgeModel != "" {
						kit.Badge(c, "judged", kit.ToneNeutral)
					}
				})
			})
			kit.Badge(c, fmt.Sprintf("%.2f", s.Reward), rewardTone(s.Reward))
			if kit.Button(c.Key("open:"+s.ID), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: "Open"}).Clicked() {
				e.app.Navigate("/chat/s/" + s.SessionID)
			}
		})
		if open {
			e.sessionDetail(c, t, s)
		}
	})
}

func (e *Evaluator) sessionDetail(c *ui.Context, t *kit.Tokens, s client.EvaluatorSessionScore) {
	heading := func(txt string) { ui.Text(c, txt).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgMuted) }
	ui.Column(c).Gap(10).Padding(0, 0, 0, 26).Children(func() {
		heading("Score components (score x weight)")
		if len(s.Components.Components) == 0 {
			ui.Text(c, "No decomposition stored for this session (scored by an older version).").FontSize(t.TextXS).TextColor(t.FgMuted)
		} else {
			names := make([]string, 0, len(s.Components.Components))
			for n := range s.Components.Components {
				names = append(names, n)
			}
			sort.Strings(names)
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, n := range names {
					kit.Badge(c, fmt.Sprintf("%s %.2f x %.2f", n, s.Components.Components[n], s.Components.Weights[n]), kit.ToneNeutral)
				}
			})
		}
		if len(s.PatternHits) > 0 {
			heading("Correction pattern hits")
			for _, h := range s.PatternHits {
				ui.Text(c, fmt.Sprintf("%s on \"%s\" (weight %.2f)", h.Pattern, h.Snippet, h.Weight)).FontSize(t.TextXS).Font(t.MonoFont)
			}
		}
		if s.FeedbackNote != "" {
			ui.Text(c, "Feedback note: "+s.FeedbackNote).FontSize(t.TextXS)
		}
		ui.Row(c).Gap(32).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				heading("Prompt variants used")
				if len(s.Variants) == 0 {
					ui.Text(c, "none (embedded defaults)").FontSize(t.TextXS).TextColor(t.FgMuted)
				}
				for _, v := range s.Variants {
					ui.Text(c, v.Section+": "+v.Variant).FontSize(t.TextXS).Font(t.MonoFont)
				}
			})
			ui.Column(c).Gap(2).Children(func() {
				heading("Skills injected")
				if len(s.Skills) == 0 {
					ui.Text(c, "none").FontSize(t.TextXS).TextColor(t.FgMuted)
				}
				for _, k := range s.Skills {
					name := k.Title
					if name == "" {
						name = k.ID
					}
					ui.Text(c, name).FontSize(t.TextXS)
				}
			})
		})
		if j := s.JudgeAnalysis; j != nil {
			h := fmt.Sprintf("Judge (%s, %d tokens)", s.JudgeModel, s.JudgePromptTokens+s.JudgeCompletionTokens)
			if j.TaskType != "" {
				h += ", task " + j.TaskType
			}
			heading(h)
			ui.Text(c, j.Reasoning).FontSize(t.TextXS)
			for _, p := range j.KeyPoints {
				ui.Text(c, "• "+strings.TrimSpace(p)).FontSize(t.TextXS)
			}
		}
	})
}
