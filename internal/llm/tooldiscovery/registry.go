package tooldiscovery

import (
	"fmt"
	"sync"

	"github.com/digiogithub/pando/internal/llm/tools"
)

// Entry holds a registered tool together with its resolved metadata.
type Entry struct {
	Tool     tools.BaseTool
	Metadata ToolMetadata
}

// Registry indexes all available tools, resolves aliases, and tracks
// per-session discovered/visible state.
type Registry struct {
	mu       sync.RWMutex
	entries  []Entry
	byName   map[string]*Entry // canonical name → entry
	aliases  map[string]string // alias → canonical name
	// discovered tracks tool names made visible during the current session
	// via tool_search results.
	discovered map[string]bool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byName:     make(map[string]*Entry),
		aliases:    make(map[string]string),
		discovered: make(map[string]bool),
	}
}

// Register adds a tool to the registry. source and nonDeferred are used when
// the tool does not implement ToolMetadataProvider.
func (r *Registry) Register(t tools.BaseTool, source ToolSource, nonDeferred bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info := t.Info()
	name := info.Name

	meta := ToolMetadata{
		CanonicalName: name,
		Source:        source,
		NonDeferred:   nonDeferred,
	}

	if mp, ok := t.(ToolMetadataProvider); ok {
		provided := mp.ToolMetadata()
		if provided.CanonicalName != "" {
			meta.CanonicalName = provided.CanonicalName
		}
		meta.Aliases = provided.Aliases
		meta.ServerName = provided.ServerName
		meta.ToolName = provided.ToolName
		if provided.Source != "" {
			meta.Source = provided.Source
		}
		meta.Category = provided.Category
		meta.NonDeferred = provided.NonDeferred
		meta.Priority = provided.Priority
	}

	// Alias collisions are a hard error in strict mode.
	for _, alias := range meta.Aliases {
		if existing, ok := r.aliases[alias]; ok && existing != name {
			return fmt.Errorf("alias %q already registered for tool %q, conflict with %q", alias, existing, name)
		}
		r.aliases[alias] = name
	}

	entry := &Entry{Tool: t, Metadata: meta}
	r.entries = append(r.entries, *entry)
	r.byName[name] = entry
	return nil
}

// Resolve returns the tool for name or alias, nil if not found.
func (r *Registry) Resolve(nameOrAlias string) tools.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if e, ok := r.byName[nameOrAlias]; ok {
		return e.Tool
	}
	if canonical, ok := r.aliases[nameOrAlias]; ok {
		if e, ok := r.byName[canonical]; ok {
			return e.Tool
		}
	}
	return nil
}

// GetEntry returns the full Entry for a canonical name.
func (r *Registry) GetEntry(name string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.byName[name]; ok {
		return *e, true
	}
	return Entry{}, false
}

// All returns all registered entries.
func (r *Registry) All() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

// AllTools returns all registered tools.
func (r *Registry) AllTools() []tools.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]tools.BaseTool, len(r.entries))
	for i, e := range r.entries {
		out[i] = e.Tool
	}
	return out
}

// MarkDiscovered marks a tool as discovered in the current session.
func (r *Registry) MarkDiscovered(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discovered[name] = true
}

// IsDiscovered reports whether a tool has been surfaced via tool_search.
func (r *Registry) IsDiscovered(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.discovered[name]
}

// DiscoveredTools returns all tools marked as discovered.
func (r *Registry) DiscoveredTools() []tools.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []tools.BaseTool
	for _, e := range r.entries {
		if r.discovered[e.Metadata.CanonicalName] {
			out = append(out, e.Tool)
		}
	}
	return out
}
