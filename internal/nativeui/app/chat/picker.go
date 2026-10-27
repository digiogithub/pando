package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// autoModelID is the synthetic model of model auto mode.
const autoModelID = "auto"

// formatModel is a human label for a model id ("claude-sonnet-4-6" ->
// "Claude Sonnet 4.6"), as the Web UI's formatModel.
func formatModel(id string) string {
	switch {
	case id == "":
		return ""
	case strings.HasPrefix(id, "copilot."):
		return "Copilot " + formatModel(id[8:])
	case strings.HasPrefix(id, "claude-"):
		rest := id[7:]
		name, ver, ok := strings.Cut(rest, "-")
		if !ok {
			return "Claude " + strings.ToUpper(rest[:1]) + rest[1:]
		}
		return "Claude " + strings.ToUpper(name[:1]) + name[1:] + " " + strings.ReplaceAll(ver, "-", ".")
	case strings.HasPrefix(id, "gpt-"):
		return "GPT-" + id[4:]
	case strings.HasPrefix(id, "gemini-"):
		return "Gemini " + id[7:]
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// modelState is the model picker and the model of the open session.
type modelState struct {
	open    bool
	query   string
	models  []client.Model
	loading bool
	err     string
	// def is the configured default model; auto the global auto mode flag.
	def     string
	auto    bool
	defLoad bool
	// session holds the per-session selection (id -> model).
	session map[string]client.SessionModel
	// pending is the model picked before the first prompt of a new chat in
	// a project workspace; the main window keeps it in Shell().NextModel,
	// shared with the model switcher overlay (see nextModel).
	pending string
	// lastRouted is the model the last prompt was routed to in auto mode.
	lastRouted string
}

// activeLabel is the composer chip text for the open conversation.
func (ch *Chat) modelLabel(cv *Conversation) string {
	ms := &ch.model
	model, auto := ms.def, ms.auto
	if sm, ok := ms.session[cv.ID]; ok && cv.ID != "" {
		model, auto = sm.Model, sm.AutoSelected
	} else if p := ch.nextModel(); cv.ID == "" && p != "" {
		model, auto = p, p == autoModelID
	}
	if auto || model == autoModelID {
		if ms.lastRouted != "" {
			return "Auto → " + formatModel(ms.lastRouted)
		}
		return "Auto"
	}
	return formatModel(model)
}

// loadModelInfo fetches the default model and the open session's model.
func (ch *Chat) loadModelInfo(cv *Conversation) {
	if ch.app.Client == nil {
		return
	}
	ms := &ch.model
	if !ms.defLoad {
		ms.defLoad = true
		ch.app.Go(func(ctx context.Context) {
			s, err := ch.app.Client.GetDesktopSettings(ctx)
			if err == nil {
				ch.app.Update(func() { ms.def = s.DefaultModel })
			}
		})
	}
	if cv.ID == "" {
		return
	}
	if _, ok := ms.session[cv.ID]; ok {
		return
	}
	if ch.modelFetching[cv.ID] {
		return
	}
	ch.modelFetching[cv.ID] = true
	id := cv.ID
	ch.app.Go(func(ctx context.Context) {
		sm, err := ch.app.Client.SessionModel(ctx, id)
		ch.app.Update(func() {
			delete(ch.modelFetching, id)
			if err == nil {
				ms.session[id] = sm
			}
		})
	})
}

func (ch *Chat) modelChip(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	ch.loadModelInfo(cv)
	label := ch.modelLabel(cv)
	if label == "" {
		label = ch.app.T("common.clickToSwitchModel")
	}
	chip := ui.Row(c.Key("modelchip")).Gap(4).Padding(3, 8).Radius(t.RadiusSM).Border(1, t.Border).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Focusable().Label(label).Tooltip(ch.app.T("common.clickToSwitchModel"))
	if chip.Hovered() {
		chip.Background(t.BgHover)
	}
	if chip.Clicked() {
		ch.openModels()
	}
	chip.Children(func() {
		if strings.HasPrefix(label, "Auto") {
			kit.Icon(c, icoSparkles).FontSize(12).TextColor(t.Accent)
		}
		ui.Text(c, label).FontSize(t.TextXS).SingleLine().Ellipsis("…").MaxWidth(200)
		kit.Icon(c, kit.IconChevronDown).FontSize(12).TextColor(t.FgFaint)
	})
}

func (ch *Chat) openModels() {
	ms := &ch.model
	ms.open, ms.query, ms.err = true, "", ""
	if ms.loading || ch.app.Client == nil {
		return
	}
	ms.loading = true
	ch.app.Go(func(ctx context.Context) {
		list, err := ch.app.Client.ListModels(ctx)
		ch.app.Update(func() {
			ms.loading = false
			if err != nil {
				ms.err = err.Error()
				return
			}
			ms.models, ms.auto = list.Models, list.AutoSelected
		})
	})
}

// filteredModels groups the models matching the query by provider, the
// Auto entry first.
func (ms *modelState) filtered() (rows []modelRow) {
	q := strings.ToLower(ms.query)
	var providers []string
	by := map[string][]client.Model{}
	for _, m := range ms.models {
		if q != "" && !strings.Contains(strings.ToLower(m.Name), q) && !strings.Contains(strings.ToLower(m.ID), q) &&
			!strings.Contains(strings.ToLower(m.Provider), q) {
			continue
		}
		if _, ok := by[m.Provider]; !ok {
			providers = append(providers, m.Provider)
		}
		by[m.Provider] = append(by[m.Provider], m)
	}
	for _, auto := range []bool{true, false} {
		for _, p := range providers {
			if (p == autoModelID) != auto {
				continue
			}
			rows = append(rows, modelRow{header: p})
			for _, m := range by[p] {
				m := m
				rows = append(rows, modelRow{model: &m})
			}
		}
	}
	return rows
}

type modelRow struct {
	header string
	model  *client.Model
}

func (ch *Chat) modelDialog(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	ms := &ch.model
	kit.Dialog(c, &ms.open, kit.DialogOpts{
		Title: "Switch Model", Width: 520,
		Body: func() {
			in := kit.Input(c.Key("mq"), &ms.query, "Search models...").AutoFocus()
			rows := ms.filtered()
			if in.Submitted() {
				for _, r := range rows {
					if r.model != nil {
						ch.selectModel(cv, r.model.ID)
						break
					}
				}
			}
			switch {
			case ms.loading:
				ui.Text(c, "Loading models...").FontSize(t.TextSM).TextColor(t.FgMuted)
			case ms.err != "":
				ui.Text(c, ms.err).FontSize(t.TextSM).TextColor(t.Danger)
			case len(rows) == 0:
				ui.Text(c, "No models found").FontSize(t.TextSM).TextColor(t.FgMuted)
			}
			ui.Scroll(c).MaxHeight(380).Children(func() {
				for i, r := range rows {
					if r.model == nil {
						ui.Text(c.Key(fmt.Sprint("mh", i)), strings.ToUpper(r.header)).FontSize(10).FontWeight(600).
							LetterSpacing(0.5).TextColor(t.FgFaint).Padding(8, 8, 2, 8)
						continue
					}
					m := r.model
					active := ch.modelActive(cv, m.ID)
					row := ui.Row(c.Key("m:"+m.ID)).Gap(10).Padding(6, 8).Radius(t.RadiusSM).AlignItems(ui.Start).
						Cursor(ui.CursorPointer).Focusable().Label(m.Name)
					if active {
						row.Background(t.AccentSoft)
					} else if row.Hovered() {
						row.Background(t.BgHover)
					}
					if row.Clicked() {
						ch.selectModel(cv, m.ID)
					}
					row.Children(func() {
						icon, col := icoCircle, t.FgFaint
						if m.ID == autoModelID {
							icon, col = icoSparkles, t.Accent
						} else if active {
							icon, col = kit.IconSuccess, t.Accent
						}
						kit.Icon(c, icon).FontSize(14).TextColor(col).Margin(2, 0, 0, 0)
						ui.Column(c).Grow(1).Shrink(1).Children(func() {
							name := m.Name
							if m.ID == autoModelID && ms.lastRouted != "" {
								name += " · " + ms.lastRouted
							}
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								x := ui.Text(c, name).FontSize(t.TextSM).FontWeight(500)
								if active {
									x.TextColor(t.Accent)
								}
								if m.ID == autoModelID {
									dot := t.Warning
									if m.RouterHealthy {
										dot = t.Success
									}
									ui.Box(c).Width(8).Height(8).Radius(4).Background(dot)
								}
							})
							if m.Description != "" {
								ui.Text(c, m.Description).FontSize(t.TextXS).TextColor(t.FgMuted)
							}
						})
						if m.CanReason {
							kit.Badge(c, "thinking", kit.ToneAccent)
						}
						for _, b := range m.Badges {
							kit.Badge(c.Key("b:"+b), b, kit.ToneNeutral)
						}
					})
				}
			})
		},
		Footer: func() {
			if kit.SecondaryButton(c, ch.app.T("projects.view.common.cancel")).Clicked() {
				ms.open = false
			}
		},
	})
}

func (ch *Chat) modelActive(cv *Conversation, id string) bool {
	ms := &ch.model
	model, auto := ms.def, ms.auto
	if sm, ok := ms.session[cv.ID]; ok && cv.ID != "" {
		model, auto = sm.Model, sm.AutoSelected
	} else if p := ch.nextModel(); cv.ID == "" && p != "" {
		model, auto = p, p == autoModelID
	}
	if id == autoModelID {
		return auto
	}
	return !auto && model == id
}

// selectModel switches the model of the open session (per-session PUT with
// sessionId). Without a session the choice travels with the first prompt.
func (ch *Chat) selectModel(cv *Conversation, id string) {
	ms := &ch.model
	if cv.ID == "" {
		ch.setNextModel(id)
		ms.open = false
		return
	}
	sid := cv.ID
	ch.app.Go(func(ctx context.Context) {
		res, err := ch.app.Client.SelectModel(ctx, id, sid)
		ch.app.Update(func() {
			if err != nil {
				ch.app.Toasts.Error("Failed to switch model", err.Error())
				return
			}
			ms.open = false
			if res.Scope == "session" || res.Scope == "" {
				ms.session[sid] = client.SessionModel{SessionID: sid, Model: id, Override: true, AutoSelected: id == autoModelID}
			}
			for _, m := range ms.models {
				if m.ID == id && m.ContextWindow > 0 {
					cv.ContextWindow = m.ContextWindow
				}
			}
			ch.app.Toasts.Success("Model switched to "+id, "")
		})
	})
}

// ---- persona ----

type personaState struct {
	open     bool
	list     []string
	active   client.ActivePersona
	loaded   bool
	loading  bool
	sessFor  string
	lastBusy bool
}

func formatPersona(name string) string {
	if name == "" {
		return "Auto"
	}
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func (ch *Chat) personaLabel(name string) string {
	if name != "" {
		return formatPersona(name)
	}
	if ch.persona.active.Auto && ch.persona.active.Applied != "" {
		return "Auto (" + ch.persona.active.Applied + ")"
	}
	return "Auto"
}

func (ch *Chat) refreshPersona(sessionID string) {
	if ch.app.Client == nil {
		return
	}
	ch.app.Go(func(ctx context.Context) {
		if !ch.persona.loaded {
			if l, err := ch.app.Client.Personas(ctx); err == nil {
				ch.app.Update(func() { ch.persona.list = l })
			}
		}
		a, err := ch.app.Client.GetActivePersona(ctx, sessionID)
		ch.app.Update(func() {
			ch.persona.loaded, ch.persona.loading = true, false
			if err == nil {
				ch.persona.active = a
			}
		})
	})
}

func (ch *Chat) personaChip(c *ui.Context, t *kit.Tokens) {
	ps := &ch.persona
	cv := ch.conv()
	// Re-read the selection when the session changes or a run ends (the
	// auto-selected persona is applied per turn).
	if !ps.loaded && !ps.loading || ps.sessFor != cv.ID || ps.lastBusy && !cv.Running {
		ps.sessFor, ps.loading = cv.ID, true
		ch.refreshPersona(cv.ID)
	}
	ps.lastBusy = cv.Running
	label := ch.personaLabel(ps.active.Active)
	chip := ui.Row(c.Key("personachip")).Gap(4).Padding(3, 8).Radius(t.RadiusSM).Border(1, t.Border).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Focusable().Label("Persona: " + label).Tooltip("Persona: " + label)
	if chip.Hovered() {
		chip.Background(t.BgHover)
	}
	if chip.Clicked() {
		ps.open = true
	}
	chip.Children(func() {
		kit.Icon(c, icoUserRnd).FontSize(12).TextColor(t.FgFaint)
		ui.Text(c, label).FontSize(t.TextXS).SingleLine().Ellipsis("…").MaxWidth(160)
		kit.Icon(c, kit.IconChevronDown).FontSize(12).TextColor(t.FgFaint)
	})
}

func (ch *Chat) personaDialog(c *ui.Context, t *kit.Tokens) {
	ps := &ch.persona
	kit.Dialog(c, &ps.open, kit.DialogOpts{
		Title: "Persona", Width: 380,
		Body: func() {
			ui.Scroll(c).MaxHeight(380).Children(func() {
				for _, name := range append([]string{""}, ps.list...) {
					active := name == ps.active.Active
					row := ui.Row(c.Key("p:"+name)).Gap(10).Padding(6, 8).Radius(t.RadiusSM).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Focusable().Label(ch.personaLabel(name))
					if active {
						row.Background(t.AccentSoft)
					} else if row.Hovered() {
						row.Background(t.BgHover)
					}
					if row.Clicked() {
						ch.selectPersona(name)
					}
					row.Children(func() {
						icon, col := icoCircle, t.FgFaint
						if active {
							icon, col = kit.IconSuccess, t.Accent
						}
						kit.Icon(c, icon).FontSize(14).TextColor(col)
						ui.Text(c, ch.personaLabel(name)).FontSize(t.TextSM)
					})
				}
			})
		},
		Footer: func() {
			if kit.SecondaryButton(c, ch.app.T("projects.view.common.cancel")).Clicked() {
				ps.open = false
			}
		},
	})
}

func (ch *Chat) selectPersona(name string) {
	ch.persona.open = false
	sid := ch.conv().ID
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.SetActivePersona(ctx, name); err != nil {
			ch.app.Toasts.Error("Failed to change persona", err.Error())
			return
		}
		ch.refreshPersona(sid)
	})
}
