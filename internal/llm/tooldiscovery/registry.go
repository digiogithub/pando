package tooldiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/digiogithub/pando/internal/llm/tools"
)

// Entry holds a registered tool together with its resolved metadata.
// Tool is nil for catalog-only entries (e.g. MCP tools reachable exclusively
// through the gateway's remote executor).
type Entry struct {
	Tool     tools.BaseTool
	Metadata ToolMetadata
}

// RemoteToolExecutor executes tools that have no local BaseTool, such as MCP
// catalog entries that live behind the gateway. It receives the server name,
// the tool name on that server, and the raw parameters.
type RemoteToolExecutor func(ctx context.Context, serverName, toolName string, params map[string]interface{}) (string, error)

// Registry indexes all available tools, resolves aliases, and tracks
// per-session discovered/visible state.
type Registry struct {
	mu      sync.RWMutex
	entries []*Entry
	byName  map[string]*Entry // canonical name → entry
	aliases map[string]string // alias → canonical name
	// discovered tracks tool names made visible during the current session
	// via tool_search results.
	discovered map[string]bool
	// remoteExecutor runs catalog-only (Tool == nil) entries, typically wired
	// to the MCP gateway.
	remoteExecutor RemoteToolExecutor
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		byName:     make(map[string]*Entry),
		aliases:    make(map[string]string),
		discovered: make(map[string]bool),
	}
}

// Register adds or updates a tool in the registry (upsert by canonical name).
// source and nonDeferred are used when the tool does not implement
// ToolMetadataProvider.
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

	return r.registerLocked(&Entry{Tool: t, Metadata: meta})
}

// RegisterCatalogEntry adds a metadata-only entry (no local BaseTool). These
// entries are searchable and executable through the remote executor.
func (r *Registry) RegisterCatalogEntry(meta ToolMetadata) error {
	if meta.CanonicalName == "" {
		return fmt.Errorf("catalog entry requires a canonical name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registerLocked(&Entry{Tool: nil, Metadata: meta})
}

// SyncCatalogEntries replaces every catalog-only (Tool == nil) entry with the
// provided set. Names that already have a live tool registered are skipped so
// directly-exposed tools (e.g. gateway favorites) always win. Discovered
// state is preserved because it is keyed by canonical name.
func (r *Registry) SyncCatalogEntries(metas []ToolMetadata) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Drop previous catalog-only entries.
	kept := r.entries[:0]
	for _, e := range r.entries {
		if e.Tool == nil {
			delete(r.byName, e.Metadata.CanonicalName)
			continue
		}
		kept = append(kept, e)
	}
	r.entries = kept

	for _, meta := range metas {
		if meta.CanonicalName == "" {
			continue
		}
		if existing, ok := r.byName[meta.CanonicalName]; ok && existing.Tool != nil {
			continue // a live tool shadows this catalog entry
		}
		_ = r.registerLocked(&Entry{Tool: nil, Metadata: meta})
	}
}

// registerLocked performs the shared upsert logic. Caller must hold the lock.
func (r *Registry) registerLocked(entry *Entry) error {
	name := entry.Metadata.CanonicalName

	// Alias collisions are a hard error in strict mode.
	for _, alias := range entry.Metadata.Aliases {
		if existing, ok := r.aliases[alias]; ok && existing != name {
			return fmt.Errorf("alias %q already registered for tool %q, conflict with %q", alias, existing, name)
		}
		r.aliases[alias] = name
	}

	if existing, ok := r.byName[name]; ok {
		// Upsert in place: replace tool and metadata, keep position.
		existing.Tool = entry.Tool
		existing.Metadata = entry.Metadata
		return nil
	}

	r.entries = append(r.entries, entry)
	r.byName[name] = entry
	return nil
}

// SetRemoteExecutor wires the executor used for catalog-only entries.
func (r *Registry) SetRemoteExecutor(fn RemoteToolExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.remoteExecutor = fn
}

// HasRemoteEntries reports whether the registry holds catalog-only entries
// that are only reachable through tool_search + the remote executor.
func (r *Registry) HasRemoteEntries() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.Tool == nil {
			return true
		}
	}
	return false
}

// Resolve returns the tool for name or alias, nil if not found or if the
// entry is catalog-only (no local BaseTool).
func (r *Registry) Resolve(nameOrAlias string) tools.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if e, ok := r.byName[nameOrAlias]; ok && e.Tool != nil {
		return e.Tool
	}
	if canonical, ok := r.aliases[nameOrAlias]; ok {
		if e, ok := r.byName[canonical]; ok && e.Tool != nil {
			return e.Tool
		}
	}
	return nil
}

// resolveEntry returns the Entry for a canonical name or alias.
func (r *Registry) resolveEntry(nameOrAlias string) (Entry, bool) {
	if e, ok := r.byName[nameOrAlias]; ok {
		return *e, true
	}
	if canonical, ok := r.aliases[nameOrAlias]; ok {
		if e, ok := r.byName[canonical]; ok {
			return *e, true
		}
	}
	return Entry{}, false
}

// ExecuteTool runs any registered tool by canonical name or alias. Live tools
// are executed directly; catalog-only entries are routed to the remote
// executor (typically the MCP gateway). The executed tool is marked as
// discovered so subsequent turns can expose it directly.
func (r *Registry) ExecuteTool(ctx context.Context, nameOrAlias string, params map[string]interface{}) (tools.ToolResponse, error) {
	r.mu.RLock()
	entry, ok := r.resolveEntry(nameOrAlias)
	executor := r.remoteExecutor
	r.mu.RUnlock()

	if !ok {
		return tools.NewTextErrorResponse(fmt.Sprintf("tool not found: %s", nameOrAlias)), nil
	}

	r.MarkDiscovered(entry.Metadata.CanonicalName)

	if entry.Tool != nil {
		input, err := json.Marshal(params)
		if err != nil {
			return tools.NewTextErrorResponse(fmt.Sprintf("marshal parameters: %s", err)), nil
		}
		return entry.Tool.Run(ctx, tools.ToolCall{
			Name:  entry.Metadata.CanonicalName,
			Input: string(input),
		})
	}

	if entry.Metadata.Source == SourceMCP && executor != nil {
		out, err := executor(ctx, entry.Metadata.ServerName, entry.Metadata.ToolName, params)
		if err != nil {
			return tools.NewTextErrorResponse(err.Error()), nil
		}
		return tools.NewTextResponse(out), nil
	}

	return tools.NewTextErrorResponse(
		fmt.Sprintf("tool %q has no local implementation and no remote executor is configured", nameOrAlias),
	), nil
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
	for i, e := range r.entries {
		out[i] = *e
	}
	return out
}

// AllTools returns all registered live tools (catalog-only entries are skipped).
func (r *Registry) AllTools() []tools.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]tools.BaseTool, 0, len(r.entries))
	for _, e := range r.entries {
		if e.Tool != nil {
			out = append(out, e.Tool)
		}
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

// DiscoveredTools returns all live tools marked as discovered.
func (r *Registry) DiscoveredTools() []tools.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []tools.BaseTool
	for _, e := range r.entries {
		if e.Tool != nil && r.discovered[e.Metadata.CanonicalName] {
			out = append(out, e.Tool)
		}
	}
	return out
}
