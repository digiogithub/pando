// Package logs is the native Logs area: the server log buffer as a table with
// level and text filters, auto-scroll and a detail panel. It mirrors
// web-ui/src/components/logs (which polls GET /api/v1/logs every 5 s).
package logs

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const pollEvery = 5 * time.Second

var levels = []string{"all", "debug", "info", "warn", "error"}

func init() {
	app.Register(app.Area{
		ID: "logs", Path: "/logs", TitleKey: "nav.logs",
		Icon: kit.IconLogs, Section: app.SectionTools, Order: 4,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

// View is the logs area.
type View struct {
	app       *app.Context
	lastBuild atomic.Int64

	entries []client.LogEntry
	loaded  bool
	loading bool
	err     string

	level      int // index in levels
	search     string
	autoScroll bool
	tbl        ui.ListState
	selIdx     int
	selID      string
	shown      []client.LogEntry // filtered entries of this frame
}

// New returns the logs view and starts its poller.
func New(a *app.Context) *View {
	v := &View{app: a, autoScroll: true, selIdx: -1}
	v.tbl.FollowEnd = true
	v.tbl.Key = func(i int) any {
		if i >= 0 && i < len(v.shown) {
			return v.shown[i].ID
		}
		return i
	}
	v.lastBuild.Store(time.Now().UnixNano())
	if a.Client != nil {
		a.Go(func(ctx context.Context) { v.pollLoop(ctx) })
	}
	return v
}

func (v *View) pollLoop(ctx context.Context) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		if time.Since(time.Unix(0, v.lastBuild.Load())) < 2*time.Second {
			v.Refresh(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Refresh reloads the last 200 entries.
func (v *View) Refresh(ctx context.Context) {
	v.app.Update(func() { v.loading = true })
	page, err := v.app.Client.ListLogs(ctx, client.LogQuery{Limit: 200})
	v.app.Update(func() {
		v.loading, v.loaded = false, true
		if err != nil {
			v.err = err.Error()
			return
		}
		v.err = ""
		v.entries = page.Logs
	})
}

func (v *View) filtered() []client.LogEntry {
	lv := levels[v.level]
	q := strings.ToLower(strings.TrimSpace(v.search))
	out := make([]client.LogEntry, 0, len(v.entries))
	for _, e := range v.entries {
		if lv != "all" && e.Level != lv {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Message), q) &&
			!strings.Contains(strings.ToLower(e.Source), q) && !strings.Contains(strings.ToLower(e.Level), q) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func levelTone(l string) kit.Tone {
	switch l {
	case "info":
		return kit.ToneInfo
	case "warn", "warning":
		return kit.ToneWarning
	case "error":
		return kit.ToneDanger
	}
	return kit.ToneNeutral
}

func clock(ts string) string {
	if tm, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return tm.Local().Format("15:04:05")
	}
	return ts
}

func full(ts string) string {
	if tm, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return tm.Local().Format("2006-01-02 15:04:05")
	}
	return ts
}

// Build implements app.View.
func (v *View) Build(c *ui.Context, r *ui.Route) {
	v.lastBuild.Store(time.Now().UnixNano())
	a := v.app
	t := kit.Current()
	v.shown = v.filtered()
	// Keep the selection on the same entry across refreshes.
	v.selIdx = -1
	for i, e := range v.shown {
		if e.ID == v.selID {
			v.selIdx = i
		}
	}

	ui.Column(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		// Filters.
		ui.Row(c).Padding(10, 16).Gap(12).AlignItems(ui.Center).Shrink(0).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.Segmented(c, &v.level, "All", "Debug", "Info", "Warn", "Error").Label("Filter by level")
			ui.Row(c).Grow(1).MinWidth(160).Gap(6).AlignItems(ui.Center).Children(func() {
				kit.Icon(c, kit.IconSearch).FontSize(14).TextColor(t.FgMuted)
				kit.Input(c, &v.search, "Search logs…").Grow(1)
			})
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: kit.IconArrowDown, Active: v.autoScroll,
				Tooltip: map[bool]string{true: "Auto-scroll on", false: "Auto-scroll off"}[v.autoScroll]}).Clicked() {
				v.autoScroll = !v.autoScroll
			}
			if v.loading {
				kit.Spinner(c).FontSize(14)
			}
			if kit.IconButton(c, kit.IconRefresh, "Refresh logs").Clicked() && a.Client != nil {
				a.Go(func(ctx context.Context) { v.Refresh(ctx) })
			}
		})
		if v.err != "" {
			ui.Text(c, v.err).FontSize(t.TextSM).TextColor(t.Danger).Padding(6, 16)
		}
		ui.Column(c).Grow(1).Shrink(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if len(v.shown) == 0 {
				kit.EmptyState(c, kit.IconLogs, "No log entries found.", "")
				return
			}
			v.tbl.FollowEnd = v.autoScroll
			prev := v.selIdx
			v.tbl.Selected = &v.selIdx
			cols := []ui.TableColumn{
				{Title: "Time", Width: 90},
				{Title: "Level", Width: 80},
				{Title: "Source", Width: 130},
				{Title: "Message"},
			}
			ui.Table(c, &v.tbl, cols, len(v.shown), func(row, col int) {
				e := v.shown[row]
				switch col {
				case 0:
					ui.Text(c, clock(e.Timestamp)).Font(t.MonoFont).FontSize(t.TextXS).TextColor(t.FgMuted)
				case 1:
					if row == v.selIdx {
						ui.Text(c, strings.ToUpper(e.Level)).FontSize(t.TextXS).FontWeight(600).TextColor(t.AccentFg)
					} else {
						kit.Badge(c, strings.ToUpper(e.Level), levelTone(e.Level))
					}
				case 2:
					ui.Text(c, e.Source).SingleLine().Ellipsis("…").FontSize(t.TextXS).TextColor(t.FgMuted)
				case 3:
					ui.Text(c, e.Message).SingleLine().Ellipsis("…").FontSize(t.TextXS)
				}
			}).Grow(1)
			if v.selIdx != prev {
				if v.selIdx >= 0 && v.selIdx < len(v.shown) {
					v.selID = v.shown[v.selIdx].ID
				} else {
					v.selID = ""
				}
			}
		})
		if v.selIdx >= 0 && v.selIdx < len(v.shown) {
			v.detail(c, t, v.shown[v.selIdx])
		}
	})
}

func (v *View) detail(c *ui.Context, t *kit.Tokens, e client.LogEntry) {
	ui.Column(c).Shrink(0).MaxHeight(260).AlignItems(ui.Stretch).Background(t.BgShell).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Height(40).Padding(0, 16).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, "Log Detail").FontWeight(600).Grow(1)
			if kit.IconButton(c, kit.IconClose, "Close detail").Clicked() {
				v.selID, v.selIdx = "", -1
			}
		})
		ui.Scroll(c).Grow(1).Padding(0, 16, 12, 16).Gap(8).Children(func() {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				ui.Text(c, full(e.Timestamp)).Font(t.MonoFont).FontSize(t.TextXS).TextColor(t.FgFaint)
				kit.Badge(c, strings.ToUpper(e.Level), levelTone(e.Level))
				ui.Text(c, "Source: "+e.Source).FontSize(t.TextXS).TextColor(t.FgMuted)
			})
			ui.Text(c, e.Message).FontSize(t.TextSM).Selectable()
			if e.Details != "" {
				ui.Text(c, e.Details).Font(t.MonoFont).FontSize(t.TextXS).TextColor(t.FgMuted).Selectable().
					Padding(8, 12).Background(t.BgCard).Radius(t.RadiusSM)
			}
		})
	})
}
