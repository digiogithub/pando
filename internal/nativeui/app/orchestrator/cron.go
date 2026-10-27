package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// cronPanel is the CronJobs tab (web CronJobsPanel).
type cronPanel struct {
	app      *app.Context
	jobs     []client.CronJob
	loaded   bool
	loading  bool
	enabled  bool
	err      string
	tbl      ui.ListState
	sel      int
	running  map[string]bool
	showForm bool
	form     client.CronJobCreate
	busy     bool
	formErr  string
	delOpen  bool
	delName  string
}

func (p *cronPanel) init(a *app.Context) {
	p.app = a
	p.sel = -1
	p.form.Enabled = true
	p.running = map[string]bool{}
	p.tbl.Key = func(i int) any {
		if i >= 0 && i < len(p.jobs) {
			return p.jobs[i].Name
		}
		return i
	}
}

func (p *cronPanel) refresh(ctx context.Context) {
	if p.app.Client == nil {
		return
	}
	p.app.Update(func() { p.loading = true })
	jobs, en, err := p.app.Client.ListCronJobs(ctx)
	p.app.Update(func() {
		p.loading, p.loaded = false, true
		if err != nil {
			p.err = err.Error()
			return
		}
		p.err, p.jobs, p.enabled = "", jobs, en
	})
}

func fmtNext(s string) string {
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || tm.Year() <= 1970 {
		return "—"
	}
	return tm.Local().Format("2006-01-02 15:04")
}

func (p *cronPanel) build(c *ui.Context, t *kit.Tokens) {
	a := p.app
	ui.Row(c).Padding(10, 20).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Text(c, "Scheduled CronJobs").FontSize(t.TextMD).FontWeight(600)
		if p.loading {
			kit.Spinner(c).FontSize(14)
		}
		if p.loaded && !p.enabled {
			kit.Badge(c, "scheduler disabled in config", kit.ToneWarning)
		}
		ui.Box(c).Grow(1)
		ico, label, variant := kit.IconPlus, "New CronJob", kit.Primary
		if p.showForm {
			ico, label, variant = kit.IconClose, "Cancel", kit.Secondary
		}
		if kit.Button(c, kit.ButtonOpts{Variant: variant, Icon: ico, Label: label}).Clicked() {
			p.showForm = !p.showForm
		}
	})
	if p.showForm {
		p.buildForm(c, t)
	}
	if p.err != "" {
		ui.Text(c, p.err).FontSize(t.TextSM).TextColor(t.Danger).Padding(0, 20, 8, 20)
	}
	ui.Column(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		if len(p.jobs) == 0 && p.loaded {
			kit.EmptyState(c, kit.IconClock, "No cronjobs configured",
				"Create your first scheduled job to run prompts automatically on a cron schedule.")
			return
		}
		cols := []ui.TableColumn{
			{Title: "Name"},
			{Title: "Schedule", Width: 140},
			{Title: "Enabled", Width: 80},
			{Title: "Engine / Model", Width: 180},
			{Title: "Next Run", Width: 150},
			{Title: "Actions", Width: 80},
		}
		ui.Table(c, &p.tbl, cols, len(p.jobs), func(row, col int) {
			j := p.jobs[row]
			switch col {
			case 0:
				ui.Column(c).Children(func() {
					ui.Text(c, j.Name).FontWeight(600).SingleLine().Ellipsis("…")
					if j.Prompt != "" {
						ui.Text(c, j.Prompt).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
					}
				})
			case 1:
				ui.Text(c, j.Schedule).Font(t.MonoFont).FontSize(t.TextXS).Padding(2, 6).Background(t.BgRaised).Radius(t.RadiusXS)
			case 2:
				on := j.Enabled
				ui.Switch(c.Key("cron-en:"+j.Name), &on).Label("Enable " + j.Name)
				if on != j.Enabled {
					name := j.Name
					p.jobs[row].Enabled = on // optimistic
					a.Go(func(ctx context.Context) {
						if err := a.Client.SetCronJobEnabled(ctx, name, on); err != nil {
							a.Toasts.Error("Failed to update cronjob \""+name+"\"", err.Error())
						}
						p.refresh(ctx)
					})
				}
			case 3:
				v := strings.Join(nonEmpty(j.Engine, j.Model), " / ")
				if v == "" {
					v = "—"
				}
				ui.Text(c, v).TextColor(t.FgMuted).SingleLine().Ellipsis("…")
			case 4:
				ui.Text(c, fmtNext(j.NextRun)).TextColor(t.FgMuted)
			case 5:
				ui.Row(c).Gap(2).Children(func() {
					name := j.Name
					if kit.Button(c.Key("run:"+name), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconPlay, Tooltip: "Run now", Disabled: p.running[name]}).Clicked() {
						p.running[name] = true
						a.Go(func(ctx context.Context) {
							_, err := a.Client.RunCronJob(ctx, name)
							a.Update(func() { delete(p.running, name) })
							if err != nil {
								a.Toasts.Error("Failed to run cronjob \""+name+"\"", err.Error())
							} else {
								a.Toasts.Success("CronJob \""+name+"\" triggered", "")
							}
						})
					}
					if kit.IconButton(c.Key("cdel:"+name), iconTrash, "Delete").Clicked() {
						p.delOpen, p.delName = true, name
					}
				})
			}
		}).Grow(1)
	})
	kit.Dialog(c, &p.delOpen, kit.DialogOpts{
		Title:       "Delete cronjob",
		Description: "Delete cronjob \"" + p.delName + "\"?",
		Footer: func() {
			if kit.Button(c, kit.ButtonOpts{Label: "Cancel"}).Clicked() {
				p.delOpen = false
			}
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Danger, Label: "Delete"}).Clicked() {
				name := p.delName
				p.delOpen = false
				a.Go(func(ctx context.Context) {
					if err := a.Client.DeleteCronJob(ctx, name); err != nil {
						a.Toasts.Error("Failed to delete cronjob", err.Error())
					} else {
						a.Toasts.Success("CronJob \""+name+"\" deleted", "")
					}
					p.refresh(ctx)
				})
			}
		},
	})
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func (p *cronPanel) buildForm(c *ui.Context, t *kit.Tokens) {
	a := p.app
	f := &p.form
	field := func(label string, grow float32, body func()) {
		ui.Column(c).Gap(4).Grow(grow).MinWidth(160).Children(func() {
			ui.Text(c, label).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgMuted)
			body()
		})
	}
	ui.Column(c).Padding(12, 20).Gap(10).Shrink(0).Background(t.BgShell).BorderWidth(1, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			field("Name *", 1, func() { kit.Input(c.Key("cf:name"), &f.Name, "e.g. daily-summary") })
			field("Schedule * (cron: min hr dom mon dow)", 1, func() { kit.Input(c.Key("cf:sched"), &f.Schedule, "e.g. 0 9 * * 1-5") })
		})
		field("Prompt *", 0, func() {
			ui.TextArea(c.Key("cf:prompt"), &f.Prompt).Lines(3, 6).Placeholder("Describe what the agent should do when this job fires...").
				Background(t.BgInput).Border(1, t.BorderStrong).Radius(t.RadiusSM).FontSize(t.TextSM).Padding(8)
		})
		ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
			field("Engine", 1, func() { kit.Input(c.Key("cf:engine"), &f.Engine, "e.g. claude") })
			field("Model", 1, func() { kit.Input(c.Key("cf:model"), &f.Model, "e.g. sonnet") })
			field("Timeout", 1, func() { kit.Input(c.Key("cf:timeout"), &f.Timeout, "e.g. 5m") })
			kit.Switch(c.Key("cf:enabled"), &f.Enabled, "Enabled").Margin(0, 0, 6, 0)
		})
		if p.formErr != "" {
			ui.Text(c, p.formErr).FontSize(t.TextXS).TextColor(t.Danger)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if kit.Button(c, kit.ButtonOpts{Label: "Cancel"}).Clicked() {
				p.showForm, p.formErr = false, ""
			}
			ready := strings.TrimSpace(f.Name) != "" && strings.TrimSpace(f.Schedule) != "" && strings.TrimSpace(f.Prompt) != ""
			label := "Create"
			if p.busy {
				label = "Creating…"
			}
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Icon: kit.IconCheck, Label: label, Disabled: p.busy || !ready}).Clicked() {
				job := *f
				p.busy, p.formErr = true, ""
				a.Go(func(ctx context.Context) {
					err := a.Client.CreateCronJob(ctx, job)
					a.Update(func() {
						p.busy = false
						if err != nil {
							p.formErr = err.Error()
							return
						}
						p.form = client.CronJobCreate{Enabled: true}
						p.showForm = false
					})
					if err == nil {
						a.Toasts.Success("CronJob \""+job.Name+"\" created", "")
						p.refresh(ctx)
					}
				})
			}
		})
	})
}
