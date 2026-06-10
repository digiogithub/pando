package tooldiscovery

import "github.com/digiogithub/pando/internal/llm/tools"

// RegistrySearchAdapter wraps *Registry to satisfy tools.ToolSearchProvider.
// This breaks the import cycle: tools imports no tooldiscovery, tooldiscovery
// imports tools, and the adapter lives in tooldiscovery.
type RegistrySearchAdapter struct {
	reg *Registry
}

// NewRegistrySearchAdapter wraps reg as a tools.ToolSearchProvider.
func NewRegistrySearchAdapter(reg *Registry) *RegistrySearchAdapter {
	return &RegistrySearchAdapter{reg: reg}
}

// SearchTools implements tools.ToolSearchProvider.
func (a *RegistrySearchAdapter) SearchTools(query string, limit int) []tools.ToolSearchEntry {
	results := a.reg.Search(query, limit)
	out := make([]tools.ToolSearchEntry, len(results))
	for i, r := range results {
		out[i] = tools.ToolSearchEntry{
			CanonicalName: r.CanonicalName,
			Aliases:       r.Aliases,
			ServerName:    r.ServerName,
			Source:        string(r.Source),
			Description:   r.Description,
		}
	}
	return out
}

// MarkDiscovered implements tools.ToolSearchProvider.
func (a *RegistrySearchAdapter) MarkDiscovered(name string) {
	a.reg.MarkDiscovered(name)
}
