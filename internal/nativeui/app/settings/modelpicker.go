package settings

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// autoModelID is the synthetic model of model auto mode: never offered by a
// settings field, which needs a concrete model.
const autoModelID = "auto"

// maxSuggestions caps the options a suggest popup builds per frame.
const maxSuggestions = 200

// suggestion is an option of a suggest field: the value stored (ID) and
// what the popup shows of it.
type suggestion struct {
	ID, Title, Meta, Desc, Group string
}

// modelCatalog is GET /api/v1/models, loaded once per settings view and
// shared by every model field.
type modelCatalog struct {
	models  []client.Model
	errors  map[string]string
	loading bool
	loaded  bool
	err     string
}

// models returns the configured providers' models (without "auto"),
// loading them on first use.
func (s *Settings) models() *modelCatalog {
	mc := &s.catalog
	if mc.loaded || mc.loading || s.app.Client == nil {
		return mc
	}
	mc.loading = true
	a := s.app
	a.Go(func(ctx context.Context) {
		list, err := a.Client.ListModels(ctx)
		a.Update(func() {
			mc.loading, mc.loaded = false, true
			if err != nil {
				mc.err = err.Error()
				return
			}
			mc.err, mc.errors = "", list.Errors
			mc.models = mc.models[:0]
			for _, m := range list.Models {
				if m.ID != autoModelID {
					mc.models = append(mc.models, m)
				}
			}
		})
	})
	return mc
}

// formatTokenLimit is 200000 -> "200K", 1000000 -> "1M" (the Web UI's
// formatTokenLimit).
func formatTokenLimit(n int64) string {
	switch {
	case n >= 1_000_000:
		if n%1_000_000 == 0 {
			return fmt.Sprintf("%dM", n/1_000_000)
		}
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", (n+500)/1_000)
	}
	return fmt.Sprint(n)
}

// modelMeta is the metadata line of a model: context window and prices,
// each omitted when unknown (a missing price must not read as "free").
func modelMeta(m client.Model) string {
	var parts []string
	if m.ContextWindow > 0 {
		parts = append(parts, formatTokenLimit(m.ContextWindow)+" ctx")
	}
	if m.MaxOutputTokens > 0 {
		parts = append(parts, formatTokenLimit(m.MaxOutputTokens)+" out")
	}
	if m.CostPer1MIn > 0 || m.CostPer1MOut > 0 {
		parts = append(parts, fmt.Sprintf("$%g/$%g per 1M", m.CostPer1MIn, m.CostPer1MOut))
	}
	if m.CanReason {
		parts = append(parts, "thinking")
	}
	return strings.Join(parts, " · ")
}

func modelSuggestions(models []client.Model) []suggestion {
	out := make([]suggestion, 0, len(models))
	for _, m := range models {
		out = append(out, suggestion{ID: m.ID, Title: orStr(m.Name, m.ID), Meta: modelMeta(m), Desc: m.Description, Group: m.Provider})
	}
	return out
}

// matchSuggestions keeps the options matching query: id, title or group
// contain it; "provider.model" narrows to a provider first, as the Web
// UI's ModelCombobox does. Those whose id starts with the query come first.
func matchSuggestions(items []suggestion, query string) []suggestion {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return items
	}
	prov, rest, dotted := strings.Cut(q, ".")
	var starts, contains []suggestion
	for _, it := range items {
		id, title, group := strings.ToLower(it.ID), strings.ToLower(it.Title), strings.ToLower(it.Group)
		hit := strings.Contains(id, q) || strings.Contains(title, q) || strings.Contains(group, q)
		if !hit && dotted && prov != "" && strings.Contains(group, prov) {
			hit = rest == "" || strings.Contains(id, rest) || strings.Contains(title, rest)
		}
		switch {
		case !hit:
		case strings.HasPrefix(id, q):
			starts = append(starts, it)
		default:
			contains = append(contains, it)
		}
	}
	return append(starts, contains...)
}

// suggestField is a text input editing *value whose popup suggests items
// (grouped, with a metadata line), as the user types, clicks it or presses
// Down. Any text can be typed; a chosen suggestion stores its ID. It
// returns the input and the suggestion chosen in this frame.
func suggestField(c *ui.Context, value *string, w float32, placeholder string, items []suggestion, status string) (ui.Element, *suggestion) {
	t := kit.Current()
	cb := ui.ComboboxBase(c, value)
	in := cb.Input.Placeholder(placeholder).Height(t.ControlH).Width(w).Padding(0, 10).
		Background(t.BgInput).Border(1, t.BorderStrong).Radius(t.RadiusSM).FontSize(t.TextSM).Label(placeholder)
	shown := items
	if cb.Filtering() {
		shown = matchSuggestions(items, *value)
	}
	if len(shown) > maxSuggestions {
		shown = shown[:maxSuggestions]
	}
	byID := map[string]*suggestion{}
	cb.Popup(func(panel ui.Element) {
		panel.MinWidth(max(w, 380)).MaxWidth(560).Padding(4).Radius(t.RadiusMD).Background(t.BgOverlay).
			Border(1, t.Border).Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.18))
		ui.Scroll(c.Key("sugg")).MaxHeight(340).Children(func() {
			if status != "" {
				ui.Text(c, status).FontSize(t.TextXS).TextColor(t.FgMuted).Padding(6, 8)
			}
			group := "\x00"
			for i := range shown {
				it := &shown[i]
				byID[it.ID] = it
				if it.Group != group {
					group = it.Group
					if group != "" {
						ui.Text(c.Key("g:"+group), strings.ToUpper(group)).FontSize(10).FontWeight(600).
							LetterSpacing(0.5).TextColor(t.FgFaint).Padding(8, 8, 2, 8)
					}
				}
				cc := c.Key("s:" + it.ID)
				item := cb.Item(it.ID).Padding(6, 8).Radius(t.RadiusSM).Gap(8).AlignItems(ui.Start).Cursor(ui.CursorPointer)
				active := it.ID == strings.TrimSpace(*value)
				switch {
				case item.Highlighted():
					item.Background(t.BgHover)
				case active:
					item.Background(t.AccentSoft)
				}
				item.Children(func() {
					ui.Column(cc).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
						ui.Row(cc).Gap(8).AlignItems(ui.Center).Children(func() {
							title := ui.Text(cc, it.Title).FontSize(t.TextSM).FontWeight(500).SingleLine().Ellipsis("…").Shrink(1)
							if active {
								title.TextColor(t.Accent)
							}
							if it.Title != it.ID {
								ui.Text(cc, it.ID).FontSize(t.TextXS).TextColor(t.FgFaint).Font(t.MonoFont).SingleLine().Ellipsis("…").Shrink(1)
							}
						})
						if it.Meta != "" {
							ui.Text(cc, it.Meta).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine()
						}
						if it.Desc != "" {
							ui.Text(cc, it.Desc).FontSize(t.TextXS).TextColor(t.FgMuted)
						}
					})
				})
			}
		})
	})
	if id, ok := cb.Chosen(); ok {
		*value = id
		return in, byID[id]
	}
	return in, nil
}

// modelCombo is a model field: free text with suggestions from the
// configured providers' models, showing context window, prices and the
// description. picked runs when a suggestion is chosen.
func (s *Settings) modelCombo(c *ui.Context, value *string, w float32, placeholder string, picked func(client.Model)) ui.Element {
	mc := s.models()
	if placeholder == "" {
		placeholder = "e.g. claude-sonnet-4-6"
	}
	status := ""
	switch {
	case mc.loading:
		status = "Loading models..."
	case mc.err != "":
		status = "Could not list models: " + mc.err
	case len(mc.models) == 0 && mc.loaded:
		status = "No models found. Configure a provider first, or type a model id."
	}
	in, chosen := suggestField(c, value, w, placeholder, modelSuggestions(mc.models), status)
	if chosen != nil && picked != nil {
		for _, m := range mc.models {
			if m.ID == chosen.ID {
				picked(m)
			}
		}
	}
	return in
}

// modelInfo is the metadata line of the model id, "" when unknown.
func (s *Settings) modelInfo(id string) string {
	for _, m := range s.catalog.models {
		if m.ID == strings.TrimSpace(id) {
			return modelMeta(m)
		}
	}
	return ""
}
