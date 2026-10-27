// Package orchestrator is the native Orchestrator area: Mesnada tasks (list,
// detail, create, cancel, delete), delegation metrics and the CronJobs panel.
// It mirrors web-ui/src/components/orchestrator.
package orchestrator

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const (
	taskPoll = 5 * time.Second
	cronPoll = 30 * time.Second

	iconStop  = "circle-stop"
	iconPlay  = "play"
	iconTrash = "trash"
)

func init() {
	app.Register(app.Area{
		ID: "orchestrator", Path: "/orchestrator", TitleKey: "nav.orchestrator",
		Icon: kit.IconOrchestrate, Section: app.SectionTools, Order: 2,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

// View is the orchestrator area.
type View struct {
	app *app.Context
	tab int // 0 tasks, 1 cronjobs

	// last time Build ran (unix nano): background polls pause when the area
	// is not on screen.
	lastBuild atomic.Int64
	tabNow    atomic.Int32 // mirror of tab for the pollers

	// tasks tab
	tasks     []client.OrchestratorTask
	loaded    bool
	loading   bool
	loadErr   string
	metrics   *client.DelegationMetrics
	tbl       ui.ListState
	selIdx    int
	selID     string
	expanded  map[string]bool
	create    createForm
	models    []string
	modelsReq bool

	// cron tab
	cron cronPanel
}

// New returns the orchestrator view and starts its pollers.
func New(a *app.Context) *View {
	v := &View{app: a, selIdx: -1}
	v.tbl.Key = func(i int) any {
		if i >= 0 && i < len(v.tasks) {
			return v.tasks[i].ID
		}
		return i
	}
	v.cron.init(a)
	v.lastBuild.Store(time.Now().UnixNano())
	if a.Client != nil {
		a.Go(func(ctx context.Context) { v.pollLoop(ctx) })
	}
	return v
}

func (v *View) visible() bool {
	return time.Since(time.Unix(0, v.lastBuild.Load())) < 2*time.Second
}

func (v *View) pollLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var lastTasks, lastCron time.Time
	for {
		now := time.Now()
		if v.visible() {
			tab := v.tabNow.Load()
			if tab == 0 && (lastTasks.IsZero() || now.Sub(lastTasks) >= taskPoll) {
				lastTasks = now
				v.refreshTasks(ctx)
			}
			if tab == 1 && (lastCron.IsZero() || now.Sub(lastCron) >= cronPoll) {
				lastCron = now
				v.cron.refresh(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Refresh reloads the active tab now (also used by tests).
func (v *View) Refresh(ctx context.Context) {
	v.refreshTasks(ctx)
	v.cron.refresh(ctx)
}

func (v *View) refreshTasks(ctx context.Context) {
	v.app.Update(func() { v.loading = true })
	tasks, err := v.app.Client.ListOrchestratorTasks(ctx)
	m, merr := v.app.Client.DelegationMetrics(ctx)
	v.app.Update(func() {
		v.loading, v.loaded = false, true
		if err != nil {
			v.loadErr = err.Error()
			return
		}
		v.loadErr = ""
		v.tasks = tasks
		if merr == nil {
			v.metrics = &m
		}
		v.syncSelection()
	})
}

func (v *View) syncSelection() {
	v.selIdx = -1
	for i, t := range v.tasks {
		if t.ID == v.selID {
			v.selIdx = i
		}
	}
	if v.selIdx < 0 {
		v.selID = ""
	}
}

func (v *View) selected() *client.OrchestratorTask {
	if v.selIdx >= 0 && v.selIdx < len(v.tasks) {
		return &v.tasks[v.selIdx]
	}
	return nil
}

func (v *View) running() int {
	n := 0
	for _, t := range v.tasks {
		if t.Status == "running" {
			n++
		}
	}
	return n
}

// Build implements app.View.
func (v *View) Build(c *ui.Context, r *ui.Route) {
	v.lastBuild.Store(time.Now().UnixNano())
	v.tabNow.Store(int32(v.tab))
	a := v.app
	t := kit.Current()
	if !v.modelsReq && a.Client != nil {
		v.modelsReq = true
		a.Go(func(ctx context.Context) {
			ml, err := a.Client.ListModels(ctx)
			if err != nil {
				return
			}
			ids := make([]string, 0, len(ml.Models))
			for _, m := range ml.Models {
				ids = append(ids, m.ID)
			}
			a.Update(func() { v.models = ids })
		})
	}

	// Section tabs.
	ui.Column(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Padding(12, 20, 0, 20).Gap(2).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.Text(c, a.T("orchestrator.hero.title")).FontSize(t.TextLG).FontWeight(600)
			ui.Text(c, a.T("orchestrator.hero.tagline")).FontSize(t.TextSM).TextColor(t.FgMuted)
			ui.Tabs(c, &v.tab, "Mesnada Tasks", "CronJobs").Margin(8, 0, 0, 0)
		})
		if v.tab == 0 {
			v.buildTasks(c, t)
		} else {
			v.cron.build(c, t)
		}
	})
}

func (v *View) buildTasks(c *ui.Context, t *kit.Tokens) {
	a := v.app
	// Toolbar.
	ui.Row(c).Padding(10, 20).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Text(c, "Mesnada Tasks").FontSize(t.TextMD).FontWeight(600)
		if n := v.running(); n > 0 {
			kit.Badge(c, fmt.Sprintf("%d running", n), kit.ToneSuccess)
		}
		if v.loading {
			kit.Spinner(c).FontSize(14)
		}
		ui.Box(c).Grow(1)
		if kit.IconButton(c, kit.IconRefresh, "Refresh").Clicked() && a.Client != nil {
			a.Go(func(ctx context.Context) { v.refreshTasks(ctx) })
		}
		if kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Icon: kit.IconPlus, Label: "Create Task"}).Clicked() {
			v.create.open = true
		}
	})
	v.metricsBar(c, t)
	if v.loadErr != "" {
		ui.Text(c, v.loadErr).FontSize(t.TextSM).TextColor(t.Danger).Padding(0, 20, 8, 20)
	}

	ui.Row(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			if len(v.tasks) == 0 && v.loaded {
				kit.EmptyState(c, kit.IconOrchestrate, "No tasks yet",
					"Create your first orchestrator task to delegate work to agents.")
				return
			}
			v.table(c, t)
		})
		if sel := v.selected(); sel != nil {
			v.detail(c, t, *sel)
		}
	})
	v.createDialog(c, t)
}

func (v *View) metricsBar(c *ui.Context, t *kit.Tokens) {
	m := v.metrics
	if m == nil || (m.WarmAttempts == 0 && m.Resurrections == 0 && m.LiveInjections == 0) {
		return
	}
	a := v.app
	item := func(key string, val any, accent bool) {
		col := t.Fg
		if accent {
			col = t.Success
		}
		ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, a.T("orchestrator.delegationMetrics."+key)).FontSize(t.TextXS).TextColor(t.FgMuted)
			ui.Text(c, fmt.Sprint(val)).FontSize(t.TextXS).FontWeight(600).TextColor(col)
		})
	}
	ui.Row(c).Wrap().Gap(14).Padding(6, 20).Shrink(0).BorderWidth(1, 0, 1, 0).BorderColor(t.Border).Children(func() {
		item("warmHitRate", fmt.Sprintf("%d%%", int(m.WarmHitRate*100+0.5)), true)
		item("warmHits", m.WarmHits, false)
		if m.ExternalHits > 0 {
			item("externalHits", m.ExternalHits, false)
		}
		item("warmFailures", m.WarmFailures, false)
		item("coldFallbacks", m.ColdFallbacks, false)
		item("capRejections", m.CapRejections, false)
		item("resurrections", m.Resurrections, false)
		item("liveInjections", m.LiveInjections, false)
		if m.ExternalReattachRecovered > 0 {
			item("reattachRecovered", m.ExternalReattachRecovered, false)
		}
		if m.ExternalReattachFailed > 0 {
			item("reattachFailed", m.ExternalReattachFailed, false)
		}
	})
}

func statusTone(s string) kit.Tone {
	switch s {
	case "running":
		return kit.ToneInfo
	case "completed":
		return kit.ToneSuccess
	case "error", "failed":
		return kit.ToneDanger
	case "cancelled":
		return kit.ToneWarning
	}
	return kit.ToneNeutral
}

func (v *View) table(c *ui.Context, t *kit.Tokens) {
	a := v.app
	cols := []ui.TableColumn{
		{Title: "Status", Width: 110},
		{Title: "Name"},
		{Title: "Agent", Width: 110},
		{Title: "Model", Width: 160},
		{Title: "Progress", Width: 110},
		{Title: "Tokens", Width: 80, Align: ui.End},
		{Title: "Actions", Width: 72},
	}
	prev := v.selIdx
	v.tbl.Selected = &v.selIdx
	ui.Table(c, &v.tbl, cols, len(v.tasks), func(row, col int) {
		task := v.tasks[row]
		switch col {
		case 0:
			if row == v.selIdx {
				ui.Text(c, task.Status).FontSize(t.TextXS).FontWeight(600).TextColor(t.AccentFg)
			} else {
				kit.Badge(c, task.Status, statusTone(task.Status))
			}
		case 1:
			ui.Text(c, task.Name).SingleLine().Ellipsis("…").FontWeight(500)
		case 2:
			ui.Text(c, task.Agent).SingleLine().Ellipsis("…").TextColor(t.FgMuted)
		case 3:
			ui.Text(c, task.Model).SingleLine().Ellipsis("…").Font(t.MonoFont).FontSize(t.TextXS)
		case 4:
			ui.Progress(c, float64(task.Progress)/100).Width(80)
		case 5:
			ui.Text(c, groupInt(task.Tokens)).Font(t.MonoFont).FontSize(t.TextXS)
		case 6:
			id := task.ID
			if task.Status == "running" {
				if kit.IconButton(c.Key("cancel:"+id), iconStop, "Cancel task").Clicked() {
					a.Go(func(ctx context.Context) {
						if err := a.Client.CancelOrchestratorTask(ctx, id); err != nil {
							a.Toasts.Error("Cancel task failed", err.Error())
						}
						v.refreshTasks(ctx)
					})
				}
			} else if kit.IconButton(c.Key("del:"+id), iconTrash, "Delete task").Clicked() {
				a.Go(func(ctx context.Context) {
					if err := a.Client.DeleteOrchestratorTask(ctx, id); err != nil {
						a.Toasts.Error("Delete task failed", err.Error())
					}
					v.refreshTasks(ctx)
				})
			}
		}
	}).Grow(1)
	if v.selIdx != prev {
		if v.selIdx >= 0 && v.selIdx < len(v.tasks) {
			v.selID = v.tasks[v.selIdx].ID
		} else {
			v.selID = ""
		}
	}
}

func groupInt(n int) string {
	s := fmt.Sprint(n)
	out := ""
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 && ch != '-' {
			out += ","
		}
		out += string(ch)
	}
	return out
}

func fmtTime(iso string) string {
	if tm, err := time.Parse(time.RFC3339Nano, iso); err == nil {
		return tm.Local().Format("2006-01-02 15:04:05")
	}
	return iso
}
