package overlays

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// autoModelID is the synthetic "auto" entry (model auto mode, EP-0015).
const autoModelID = "auto"

// modelSwitcher is the model overlay (Ctrl/Cmd+O): the models of every
// provider account, grouped by provider with Auto first. Selecting one sets
// the model of the open chat session only (the default model of every
// session is changed in Settings).
type modelSwitcher struct {
	a       *app.Context
	pick    picker
	wasOpen bool

	loading bool
	models  []client.Model
	auto    bool   // Auto is the active selection
	active  string // active concrete model ID
	err     string
	gen     int // discards answers of a previous opening
}

func newModelSwitcher(a *app.Context) *modelSwitcher { return &modelSwitcher{a: a} }

// Build implements app.Overlay.
func (m *modelSwitcher) Build(c *ui.Context) {
	sh := m.a.Shell()
	if !sh.ModelSwitcherOpen {
		m.wasOpen = false
		return
	}
	if !m.wasOpen {
		m.wasOpen = true
		m.pick.reset()
		m.load()
	}
	rows := m.rows()
	empty := "No models found"
	if m.loading {
		empty = "Loading models…"
	} else if m.err != "" {
		empty = m.err
	}
	kit.Dialog(c, &sh.ModelSwitcherOpen, kit.DialogOpts{
		Title: "Switch Model",
		Width: 560,
		Body: func() {
			m.pick.build(c, "Search models…", empty, rows, 360, func(r pickRow) {
				sh.ModelSwitcherOpen = false
				r.Run()
			})
			t := kit.Current()
			ui.Text(c, "↑↓ navigate  ·  Enter select  ·  Esc close").FontSize(t.TextXS).TextColor(t.FgFaint)
		},
	})
}

// load fetches the model list and the active selection.
func (m *modelSwitcher) load() {
	a := m.a
	m.loading, m.err = true, ""
	m.gen++
	gen := m.gen
	session := a.ActiveSessionID()
	a.Go(func(ctx context.Context) {
		list, err := a.Client.ListModels(ctx)
		var (
			active string
			auto   = list.AutoSelected
		)
		if err == nil {
			if session != "" {
				if sm, e := a.Client.SessionModel(ctx, session); e == nil {
					active, auto = sm.Model, sm.AutoSelected
				}
			} else if s, e := a.Client.GetDesktopSettings(ctx); e == nil {
				active = s.DefaultModel
			}
		}
		a.Update(func() {
			if gen != m.gen {
				return
			}
			m.loading = false
			if err != nil {
				m.err = client.DescribeError(err)
				if client.IsAuthError(err) {
					a.ReportError("Could not load models", err)
				}
				return
			}
			m.models, m.auto, m.active = list.Models, auto, active
		})
	})
}

var badgeTone = map[string]kit.Tone{
	"fast": kit.ToneSuccess, "cost": kit.ToneWarning, "capable": kit.ToneInfo,
	"vision": kit.ToneAccent, "reasoning": kit.ToneAccent, "thinking": kit.ToneAccent,
}

func (m *modelSwitcher) rows() []pickRow {
	models := append([]client.Model(nil), m.models...)
	// Auto leads, then providers alphabetically; stable within a provider.
	sort.SliceStable(models, func(i, j int) bool {
		ai, aj := models[i].Provider == autoModelID, models[j].Provider == autoModelID
		if ai != aj {
			return ai
		}
		return models[i].Provider < models[j].Provider
	})
	rows := make([]pickRow, 0, len(models))
	for _, md := range models {
		md := md
		isAuto := md.ID == autoModelID
		active := !m.auto && md.ID == m.active
		if isAuto {
			active = m.auto
		}
		r := pickRow{ID: md.ID + "@" + md.AccountID, Group: md.Provider, Icon: modelIcon(isAuto, active), Title: md.Name, Desc: md.Description,
			Meta: modelMeta(md), Active: active, Search: md.ID + " " + md.Provider, Run: func() { m.choose(md) }}
		if isAuto {
			r.Badges = append(r.Badges, healthBadge(md))
		}
		if md.CanReason {
			r.Badges = append(r.Badges, badge{"thinking", kit.ToneAccent})
		}
		for _, b := range md.Badges {
			tone, ok := badgeTone[b]
			if !ok {
				tone = kit.ToneNeutral
			}
			r.Badges = append(r.Badges, badge{b, tone})
		}
		rows = append(rows, r)
	}
	return rows
}

func modelIcon(isAuto, active bool) string {
	switch {
	case isAuto:
		return kit.IconEvaluator
	case active:
		return kit.IconSuccess
	}
	return kit.IconBot
}

func healthBadge(md client.Model) badge {
	if md.RouterHealthy {
		return badge{"decision ok", kit.ToneSuccess}
	}
	return badge{"decision unhealthy", kit.ToneWarning}
}

func modelMeta(md client.Model) string {
	var parts []string
	if md.ContextWindow > 0 {
		parts = append(parts, tokenLimit(md.ContextWindow)+" ctx")
	}
	if md.CostPer1MIn > 0 || md.CostPer1MOut > 0 {
		parts = append(parts, fmt.Sprintf("$%g/$%g per 1M", md.CostPer1MIn, md.CostPer1MOut))
	}
	return strings.Join(parts, " · ")
}

func tokenLimit(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%gM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%gK", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

// choose selects md for the open session, as the web does: without a session
// the choice travels with the first prompt (Shell.NextModel) when a default
// model exists, otherwise the server persists it.
func (m *modelSwitcher) choose(md client.Model) {
	a := m.a
	session := a.ActiveSessionID()
	a.Go(func(ctx context.Context) {
		if session == "" && m.active != "" {
			a.Update(func() { a.Shell().NextModel = md.ID })
		} else if err := a.Client.SetActiveModel(ctx, md.ID, session); err != nil {
			a.ReportError("Failed to switch model", err)
			return
		}
		a.Update(func() {
			m.auto, m.active = md.ID == autoModelID, md.ID
			if f := a.Shell().ModelChanged; f != nil {
				f(md.ID, session)
			}
		})
		a.Toasts.Success("Model switched", md.ID)
	})
}
