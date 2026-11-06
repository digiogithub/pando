package dialog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// ProviderModels groups the selectable models of one configured provider.
type ProviderModels struct {
	Provider models.ModelProvider
	Models   []models.Model
}

// ListProviderModels returns the models of every enabled provider, grouped by
// provider and ordered like the model dialog (provider popularity, then model
// name). It is the non-interactive counterpart of the model dialog, used by the
// /model slash command.
func ListProviderModels(cfg *config.Config) []ProviderModels {
	if cfg == nil {
		return nil
	}
	var out []ProviderModels
	for _, provider := range getEnabledProviders(cfg) {
		list := dropAmbiguousStaticModels(getModelsForProvider(provider))
		if len(list) == 0 {
			continue
		}
		out = append(out, ProviderModels{Provider: provider, Models: list})
	}
	return out
}

// FormatProviderModels renders the grouped model list as plain text: one header
// per provider and one exact, copy-pasteable id per model.
func FormatProviderModels(groups []ProviderModels, currentID models.ModelID) string {
	if len(groups) == 0 {
		return "No models available: no provider is configured."
	}
	var b strings.Builder
	b.WriteString("Available models (use /model <id> or /model <provider>/<id>):\n")
	for _, g := range groups {
		fmt.Fprintf(&b, "\n%s\n", g.Provider)
		for _, m := range g.Models {
			marker := "  "
			if m.ID == currentID {
				marker = "* "
			}
			if m.Name != "" && m.Name != string(m.ID) {
				fmt.Fprintf(&b, "%s%s  (%s)\n", marker, m.ID, m.Name)
			} else {
				fmt.Fprintf(&b, "%s%s\n", marker, m.ID)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ResolveModelArg finds the model named by a /model argument. It accepts an
// exact model id, or "provider/id" (the id may also be given without the
// provider prefix its canonical form carries). Matching is case-insensitive.
func ResolveModelArg(groups []ProviderModels, arg string) (models.Model, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return models.Model{}, fmt.Errorf("empty model id")
	}
	lower := strings.ToLower(arg)

	// Exact id first.
	for _, g := range groups {
		for _, m := range g.Models {
			if strings.ToLower(string(m.ID)) == lower {
				return m, nil
			}
		}
	}

	// provider/id.
	if prov, id, ok := strings.Cut(lower, "/"); ok {
		for _, g := range groups {
			if strings.ToLower(string(g.Provider)) != prov {
				continue
			}
			for _, m := range g.Models {
				mid := strings.ToLower(string(m.ID))
				if mid == id || mid == prov+"."+id || strings.TrimPrefix(mid, prov+".") == id {
					return m, nil
				}
			}
		}
	}

	// Unique suffix match as a convenience ("gpt-4o" for "copilot.gpt-4o").
	var hits []models.Model
	for _, g := range groups {
		for _, m := range g.Models {
			if strings.HasSuffix(strings.ToLower(string(m.ID)), "."+lower) {
				hits = append(hits, m)
			}
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) > 1 {
		ids := make([]string, 0, len(hits))
		for _, m := range hits {
			ids = append(ids, string(m.ID))
		}
		slices.Sort(ids)
		return models.Model{}, fmt.Errorf("ambiguous model %q, candidates: %s", arg, strings.Join(ids, ", "))
	}
	return models.Model{}, fmt.Errorf("unknown model %q; run /model to list the available ids", arg)
}
