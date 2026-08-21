package extension

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry holds extension factories keyed by ID. Extensions register into the
// package-level default registry from init(); a separate Registry is mainly
// useful in tests, which must not see whatever the rest of the binary
// registered.
type Registry struct {
	mu      sync.RWMutex
	entries map[ID]Info
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[ID]Info)}
}

var defaultRegistry = NewRegistry()

// Register adds an extension to the default registry. It is meant to be called
// from init() and panics on programmer error — an invalid ID, a missing
// factory, or a duplicate registration — because there is no sensible way to
// recover from a malformed extension at that point, and failing loudly at
// startup beats a silently missing feature.
func Register(e Extension) { defaultRegistry.Register(e) }

// Register adds an extension to this registry. See the package-level Register
// for the panic contract.
func (r *Registry) Register(e Extension) {
	if e == nil {
		panic("extension: Register called with nil extension")
	}
	info := e.ExtensionInfo()
	if !info.ID.Valid() {
		panic(fmt.Sprintf("extension: invalid ID %q (want dot-separated [a-z0-9_-] segments)", info.ID))
	}
	if info.New == nil {
		panic(fmt.Sprintf("extension: %s has no New factory", info.ID))
	}
	if info.License == "" {
		info.License = LicenseMIT
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[info.ID]; exists {
		panic(fmt.Sprintf("extension: duplicate registration of %s", info.ID))
	}
	r.entries[info.ID] = info
}

// Get returns the registered Info for an ID.
func Get(id ID) (Info, bool) { return defaultRegistry.Get(id) }

// Get returns the registered Info for an ID.
func (r *Registry) Get(id ID) (Info, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.entries[id]
	return info, ok
}

// List returns every registered extension, sorted by ID.
func List() []Info { return defaultRegistry.List() }

// List returns every registered extension, sorted by ID.
func (r *Registry) List() []Info {
	r.mu.RLock()
	out := make([]Info, 0, len(r.entries))
	for _, info := range r.entries {
		out = append(out, info)
	}
	r.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ByNamespace returns every extension whose ID equals the namespace or sits
// under it, sorted by ID. ByNamespace("tools") matches "tools.acme.jira".
func ByNamespace(ns string) []Info { return defaultRegistry.ByNamespace(ns) }

// ByNamespace returns every extension whose ID equals the namespace or sits
// under it, sorted by ID.
func (r *Registry) ByNamespace(ns string) []Info {
	prefix := ns + "."

	r.mu.RLock()
	var out []Info
	for id, info := range r.entries {
		if string(id) == ns || strings.HasPrefix(string(id), prefix) {
			out = append(out, info)
		}
	}
	r.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len reports how many extensions are registered.
func Len() int { return defaultRegistry.Len() }

// Len reports how many extensions are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}
