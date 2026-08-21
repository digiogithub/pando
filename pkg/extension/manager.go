package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"sync"
)

// Entry is the per-extension configuration the host passes to the manager. It
// mirrors [Extensions.Entries."<id>"] in pando.toml.
type Entry struct {
	// Enabled switches the extension on. An extension is also loaded when it
	// has a non-empty Config, so that configuring one is enough to enable it.
	Enabled bool
	// Config is the extension's own configuration subtree.
	Config map[string]any
}

// Options configures a Manager.
type Options struct {
	// Registry to load from. Defaults to the package-level registry.
	Registry *Registry
	// Entries is the per-extension configuration, keyed by ID.
	Entries map[string]Entry
	// Disabled lists IDs that must never load, whatever Entries says. It is the
	// stronger switch: it also turns off extensions that would otherwise load
	// by default.
	Disabled []string
	// Host is the base HostServices handed to each extension. The manager fills
	// in Raw and Logger per extension; the rest is passed through untouched.
	Host HostServices
	// Logger receives manager-level messages. Defaults to slog.Default().
	Logger *slog.Logger
}

// Manager owns the lifecycle of the extensions loaded into this process.
//
// The zero value is not usable; build one with NewManager. A Manager is safe
// for concurrent use.
type Manager struct {
	registry *Registry
	opts     Options
	log      *slog.Logger

	mu       sync.RWMutex
	loaded   map[ID]Extension
	order    []ID // load order, for deterministic shutdown in reverse
	statuses map[ID]Status
}

// NewManager builds a Manager. It does not load anything: call Load.
func NewManager(opts Options) *Manager {
	reg := opts.Registry
	if reg == nil {
		reg = defaultRegistry
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		registry: reg,
		opts:     opts,
		log:      log,
		loaded:   make(map[ID]Extension),
		statuses: make(map[ID]Status),
	}
}

// Load provisions every registered extension that configuration allows, in
// dependency order.
//
// A failing extension does not abort the others: its error is recorded in its
// Status and the rest still load. That is deliberate — one broken optional
// feature must not prevent Pando from starting. Load returns the joined errors
// so the caller can log them; callers normally do not treat that as fatal.
func (m *Manager) Load(ctx context.Context) error {
	infos := m.registry.List()

	disabled := make(map[ID]struct{}, len(m.opts.Disabled))
	for _, id := range m.opts.Disabled {
		disabled[ID(id)] = struct{}{}
	}

	var errs []error
	for _, info := range m.resolveOrder(infos) {
		if _, off := disabled[info.ID]; off {
			m.setStatus(Status{Info: info, Disabled: true})
			m.log.Debug("extension disabled by configuration", "id", info.ID)
			continue
		}

		entry, configured := m.opts.Entries[string(info.ID)]
		if configured && !entry.Enabled && len(entry.Config) == 0 {
			m.setStatus(Status{Info: info, Disabled: true})
			m.log.Debug("extension not enabled", "id", info.ID)
			continue
		}
		if !configured && !m.defaultEnabled(info) {
			m.setStatus(Status{Info: info, Disabled: true})
			continue
		}

		if err := m.missingDependencies(info); err != nil {
			m.setStatus(Status{Info: info, Err: err})
			errs = append(errs, err)
			m.log.Warn("extension not loaded", "id", info.ID, "error", err)
			continue
		}

		if err := m.load(ctx, info, entry.Config); err != nil {
			m.setStatus(Status{Info: info, Err: err})
			errs = append(errs, err)
			m.log.Warn("extension failed to load", "id", info.ID, "error", err)
			continue
		}

		m.setStatus(Status{Info: info, Loaded: true})
		m.log.Info("extension loaded", "id", info.ID, "version", info.Version, "license", info.License)
	}

	return errors.Join(errs...)
}

// defaultEnabled decides what happens to an extension with no configuration
// entry at all. Bundled MIT extensions are on by default; anything else must be
// switched on explicitly, so that adding a private module to a build never
// silently changes behaviour.
func (m *Manager) defaultEnabled(info Info) bool {
	return info.License == "" || info.License == LicenseMIT
}

func (m *Manager) missingDependencies(info Info) error {
	for _, dep := range info.RequiresExtensions {
		m.mu.RLock()
		_, ok := m.loaded[dep]
		m.mu.RUnlock()
		if !ok {
			return fmt.Errorf("extension %s requires %s, which is not loaded", info.ID, dep)
		}
	}
	return nil
}

// load runs one extension through its lifecycle. Every call into extension code
// is guarded: a panic in a third-party or enterprise extension must not take
// the process down.
func (m *Manager) load(ctx context.Context, info Info, raw map[string]any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("extension %s panicked during load: %v", info.ID, r)
		}
	}()

	inst := info.New()
	if inst == nil {
		return fmt.Errorf("extension %s: New returned nil", info.ID)
	}

	if raw == nil {
		raw = map[string]any{}
	}
	host := m.opts.Host
	host.Raw = raw
	host.Logger = m.log.With("extension", string(info.ID))

	if p, ok := inst.(Provisioner); ok {
		if err := p.Provision(ctx, host); err != nil {
			return fmt.Errorf("extension %s: provision: %w", info.ID, err)
		}
	}
	if v, ok := inst.(Validator); ok {
		if err := v.Validate(); err != nil {
			m.cleanup(info.ID, inst)
			return fmt.Errorf("extension %s: validate: %w", info.ID, err)
		}
	}

	m.mu.Lock()
	m.loaded[info.ID] = inst
	m.order = append(m.order, info.ID)
	m.mu.Unlock()
	return nil
}

// resolveOrder sorts extensions so that dependencies come before dependents.
// The input is already sorted by ID, and the sort below is stable, so the
// result is deterministic. A dependency cycle is not resolved here: the
// extensions involved keep their ID order and fail the RequiresExtensions
// check at load time, which reports a clear error instead of hanging.
func (m *Manager) resolveOrder(infos []Info) []Info {
	index := make(map[ID]int, len(infos))
	for i, info := range infos {
		index[info.ID] = i
	}

	depth := make(map[ID]int, len(infos))
	var resolve func(info Info, seen map[ID]bool) int
	resolve = func(info Info, seen map[ID]bool) int {
		if d, ok := depth[info.ID]; ok {
			return d
		}
		if seen[info.ID] {
			return 0 // cycle: stop descending, load order falls back to ID order
		}
		seen[info.ID] = true

		d := 0
		for _, dep := range info.RequiresExtensions {
			i, ok := index[dep]
			if !ok {
				continue
			}
			if dd := resolve(infos[i], seen); dd+1 > d {
				d = dd + 1
			}
		}
		delete(seen, info.ID)
		depth[info.ID] = d
		return d
	}
	for _, info := range infos {
		resolve(info, map[ID]bool{})
	}

	out := make([]Info, len(infos))
	copy(out, infos)
	sort.SliceStable(out, func(i, j int) bool { return depth[out[i].ID] < depth[out[j].ID] })
	return out
}

// Start runs the Lifecycle hook of every loaded extension that has one.
func (m *Manager) Start(ctx context.Context) error {
	var errs []error
	for _, inst := range m.instances() {
		lc, ok := inst.(Lifecycle)
		if !ok {
			continue
		}
		id := inst.ExtensionInfo().ID
		if err := safeCall(func() error { return lc.Start(ctx) }); err != nil {
			errs = append(errs, fmt.Errorf("extension %s: start: %w", id, err))
			m.log.Warn("extension failed to start", "id", id, "error", err)
		}
	}
	return errors.Join(errs...)
}

// Stop runs the Lifecycle stop hook in reverse load order.
func (m *Manager) Stop(ctx context.Context) error {
	var errs []error
	for _, inst := range reverse(m.instances()) {
		lc, ok := inst.(Lifecycle)
		if !ok {
			continue
		}
		id := inst.ExtensionInfo().ID
		if err := safeCall(func() error { return lc.Stop(ctx) }); err != nil {
			errs = append(errs, fmt.Errorf("extension %s: stop: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// Unload stops and cleans up a single extension.
func (m *Manager) Unload(id ID) error {
	m.mu.Lock()
	inst, ok := m.loaded[id]
	if ok {
		delete(m.loaded, id)
		for i, cur := range m.order {
			if cur == id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("extension %s is not loaded", id)
	}
	m.cleanup(id, inst)

	m.mu.Lock()
	if st, exists := m.statuses[id]; exists {
		st.Loaded = false
		st.Disabled = true
		m.statuses[id] = st
	}
	m.mu.Unlock()
	return nil
}

// Cleanup unloads every extension in reverse load order. It is safe to call
// more than once.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	order := make([]ID, len(m.order))
	copy(order, m.order)
	instances := make(map[ID]Extension, len(m.loaded))
	maps.Copy(instances, m.loaded)
	m.loaded = make(map[ID]Extension)
	m.order = nil
	m.mu.Unlock()

	for i := len(order) - 1; i >= 0; i-- {
		id := order[i]
		if inst, ok := instances[id]; ok {
			m.cleanup(id, inst)
		}
	}
}

func (m *Manager) cleanup(id ID, inst Extension) {
	cu, ok := inst.(CleanerUpper)
	if !ok {
		return
	}
	if err := safeCall(cu.Cleanup); err != nil {
		m.log.Warn("extension cleanup failed", "id", id, "error", err)
	}
}

// Loaded reports whether an extension is currently loaded.
func (m *Manager) Loaded(id ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.loaded[id]
	return ok
}

// Statuses returns the outcome for every registered extension, sorted by ID.
func (m *Manager) Statuses() []Status {
	m.mu.RLock()
	out := make([]Status, 0, len(m.statuses))
	for _, st := range m.statuses {
		out = append(out, st)
	}
	m.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].Info.ID < out[j].Info.ID })
	return out
}

func (m *Manager) setStatus(st Status) {
	m.mu.Lock()
	m.statuses[st.Info.ID] = st
	m.mu.Unlock()
}

// instances returns the loaded extensions in load order.
func (m *Manager) instances() []Extension {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Extension, 0, len(m.order))
	for _, id := range m.order {
		if inst, ok := m.loaded[id]; ok {
			out = append(out, inst)
		}
	}
	return out
}

// Capability returns every loaded extension implementing T, in load order. It
// is how core subsystems find what extends them:
//
//	for _, p := range extension.Capability[extension.ToolProvider](mgr) { ... }
func Capability[T any](m *Manager) []T {
	if m == nil {
		return nil
	}
	var out []T
	for _, inst := range m.instances() {
		if v, ok := inst.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// safeCall runs fn, converting a panic into an error so that a misbehaving
// extension cannot bring the host down.
func safeCall(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}

func reverse[T any](in []T) []T {
	out := make([]T, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
