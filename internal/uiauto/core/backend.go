package core

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// AppInfo is a lightweight description of a running application, cheap
// enough to list without walking any accessibility tree.
type AppInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	PID     int    `json:"pid"`
	Windows int    `json:"windows"`
}

// WindowInfo is a lightweight description of a top-level window.
type WindowInfo struct {
	ID      string `json:"id"`
	AppID   string `json:"appId"`
	Title   string `json:"title"`
	Bounds  Bounds `json:"bounds"`
	Focused bool   `json:"focused"`
}

// Scope restricts a Find/traversal operation to a subtree, so a backend
// never has to walk more of the tree than the caller asked for.
type Scope struct {
	AppID    string
	WindowID string
	// Root, when set, restricts the search to the subtree rooted at this
	// element instead of the whole app/window.
	Root *Element
	// Depth caps how many levels below Root/window the backend should
	// descend. Zero means "backend default".
	Depth int
}

// Backend is the platform-specific accessibility/automation driver. It must
// never be forced to build a whole accessibility tree: Find/Children let
// each platform optimize traversal (cached subtree fetch, batched
// attribute reads, incremental queries, ...).
type Backend interface {
	// Name returns the backend identifier, e.g. "atspi", "uia", "ax",
	// "cdp", "null".
	Name() string

	// Available reports the Capabilities this backend can offer in the
	// current session. It may perform cheap probing but must not block on
	// user interaction.
	Available(ctx context.Context) (Capabilities, error)

	// Apps lists running applications visible to this backend.
	Apps(ctx context.Context) ([]AppInfo, error)

	// Windows lists the top-level windows of appID, or of all apps when
	// appID is empty.
	Windows(ctx context.Context, appID string) ([]WindowInfo, error)

	// Find resolves a selector within scope, returning at most limit
	// matches (limit <= 0 means "backend default cap"). Implementations
	// should stop traversing as soon as they have enough matches.
	Find(ctx context.Context, scope Scope, sel *Selector, limit int) ([]*Element, error)

	// Children returns the direct children of el.
	Children(ctx context.Context, el *Element) ([]*Element, error)

	// Properties reads a set of extra properties for el. When props is
	// empty, the backend returns whatever extra properties it can cheaply
	// provide.
	Properties(ctx context.Context, el *Element, props []string) (map[string]any, error)

	// Perform executes action against el.
	Perform(ctx context.Context, el *Element, action Action) error

	// Close releases any resources (connections, handles) held by the
	// backend.
	Close() error
}

// BackendFactory constructs a Backend instance.
type BackendFactory func() (Backend, error)

// Registry maps backend names to constructors, so callers can resolve
// "auto" (platform default) or a specific backend name without importing
// every platform package.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]BackendFactory
	// autoOrder lists backend names to try, in order, when resolving
	// "auto". Registered backends not listed here are tried after, in
	// registration order.
	autoOrder []string
}

// NewRegistry creates an empty backend Registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]BackendFactory)}
}

// Register adds or replaces the factory for the given backend name.
func (r *Registry) Register(name string, factory BackendFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

// SetAutoOrder sets the preference order used when resolving "auto".
func (r *Registry) SetAutoOrder(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.autoOrder = append([]string(nil), names...)
}

// Names returns the registered backend names, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.factories))
	for n := range r.factories {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Resolve constructs the backend registered under name. name == "auto"
// (or "") tries the configured auto-order, then any remaining registered
// backends, returning the first one that constructs successfully. Resolve
// never itself returns NullBackend; callers should fall back to it
// explicitly when no backend is available.
func (r *Registry) Resolve(name string) (Backend, error) {
	r.mu.RLock()
	factories := make(map[string]BackendFactory, len(r.factories))
	for k, v := range r.factories {
		factories[k] = v
	}
	autoOrder := append([]string(nil), r.autoOrder...)
	r.mu.RUnlock()

	if name != "" && name != "auto" {
		factory, ok := factories[name]
		if !ok {
			return nil, NewPlatformNotSupportedError(fmt.Sprintf("backend %q is not registered", name))
		}
		return factory()
	}

	tried := make(map[string]bool)
	var lastErr error
	for _, n := range autoOrder {
		factory, ok := factories[n]
		if !ok {
			continue
		}
		tried[n] = true
		b, err := factory()
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	// Fall back to any remaining registered backend, in stable order.
	remaining := make([]string, 0, len(factories))
	for n := range factories {
		if !tried[n] {
			remaining = append(remaining, n)
		}
	}
	sort.Strings(remaining)
	for _, n := range remaining {
		b, err := factories[n]()
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, NewPlatformNotSupportedError("no uiauto backend is registered")
}

// NullBackend is a Backend implementation used when no platform backend is
// available (unsupported OS, missing permissions, etc). Every operation
// fails with PLATFORM_NOT_SUPPORTED except Available, which reports an
// all-false Capabilities so callers can degrade gracefully instead of
// crashing.
type NullBackend struct{}

// NewNullBackend constructs a NullBackend.
func NewNullBackend() *NullBackend { return &NullBackend{} }

// Name implements Backend.
func (n *NullBackend) Name() string { return "null" }

// Available implements Backend.
func (n *NullBackend) Available(ctx context.Context) (Capabilities, error) {
	return Capabilities{}, nil
}

// Apps implements Backend.
func (n *NullBackend) Apps(ctx context.Context) ([]AppInfo, error) {
	return nil, NewPlatformNotSupportedError("no uiauto backend is available on this platform/session")
}

// Windows implements Backend.
func (n *NullBackend) Windows(ctx context.Context, appID string) ([]WindowInfo, error) {
	return nil, NewPlatformNotSupportedError("no uiauto backend is available on this platform/session")
}

// Find implements Backend.
func (n *NullBackend) Find(ctx context.Context, scope Scope, sel *Selector, limit int) ([]*Element, error) {
	return nil, NewPlatformNotSupportedError("no uiauto backend is available on this platform/session")
}

// Children implements Backend.
func (n *NullBackend) Children(ctx context.Context, el *Element) ([]*Element, error) {
	return nil, NewPlatformNotSupportedError("no uiauto backend is available on this platform/session")
}

// Properties implements Backend.
func (n *NullBackend) Properties(ctx context.Context, el *Element, props []string) (map[string]any, error) {
	return nil, NewPlatformNotSupportedError("no uiauto backend is available on this platform/session")
}

// Perform implements Backend.
func (n *NullBackend) Perform(ctx context.Context, el *Element, action Action) error {
	return NewPlatformNotSupportedError("no uiauto backend is available on this platform/session")
}

// Close implements Backend.
func (n *NullBackend) Close() error { return nil }
