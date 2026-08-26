package extension

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"sync"
	"sync/atomic"
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

	// ungatedLogged keeps the "no license provider" notice to once per manager:
	// it is a build fact, and repeating it per extension buries it.
	ungatedLogged atomic.Bool

	mu       sync.RWMutex
	loaded   map[ID]Extension
	order    []ID // load order, for deterministic shutdown in reverse
	statuses map[ID]Status
	license  LicenseProvider
	// licenseIDs is the set of registered extensions that provide licensing,
	// discovered in resolveOrder before anything is provisioned.
	licenseIDs map[ID]bool
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

	disabled := m.disabledSet()

	var errs []error
	for _, info := range m.resolveOrder(infos) {
		entry := m.opts.Entries[string(info.ID)]
		if !m.enabled(info, disabled) {
			m.setStatus(Status{Info: info, Disabled: true})
			m.log.Debug("extension not enabled", "id", info.ID)
			continue
		}

		if err := m.entitled(info); err != nil {
			m.setStatus(Status{Info: info, Unlicensed: true, Err: err})
			errs = append(errs, err)
			m.log.Error("extension not licensed, refusing to load",
				"id", info.ID, "license", info.License, "reason", err)
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

		m.adoptLicenseProvider(info.ID)
		m.setStatus(Status{Info: info, Loaded: true})
		m.log.Info("extension loaded", "id", info.ID, "version", info.Version, "license", info.License)
	}

	return errors.Join(errs...)
}

// entitled asks the license provider whether this extension may load.
//
// Three rules, in order:
//
//   - MIT extensions are never gated. The open-source core must load with no
//     licensing machinery compiled in at all.
//   - A LicenseProvider is never gated by itself: it has to start before it can
//     answer anything, and gating it would be a deadlock dressed as a policy.
//   - With no license provider compiled into the build, nothing is gated. That
//     is a deliberate choice: the absence of the gate is a fact about how the
//     binary was built, not something a customer did, and refusing to start
//     every enterprise module because the build pipeline left out one module
//     would turn a packaging mistake into an outage. It is logged instead.
func (m *Manager) entitled(info Info) error {
	if info.License == "" || info.License == LicenseMIT {
		return nil
	}
	m.mu.RLock()
	provider := m.license
	m.mu.RUnlock()
	if provider == nil {
		if !m.ungatedLogged.Swap(true) {
			m.log.Info("no license provider in this build; enterprise extensions load ungated")
		}
		return nil
	}
	if provider.ExtensionInfo().ID == info.ID {
		return nil
	}
	var err error
	if callErr := safeCall(func() error { err = provider.Entitled(info); return nil }); callErr != nil {
		// A panicking license provider must not become a way past the gate.
		return fmt.Errorf("license check failed: %w", callErr)
	}
	return err
}

// adoptLicenseProvider records the first loaded extension that provides
// licensing. Later ones are ignored and reported: two gates disagreeing would
// make "is this licensed" depend on load order.
func (m *Manager) adoptLicenseProvider(id ID) {
	m.mu.Lock()
	inst, ok := m.loaded[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	lp, isProvider := inst.(LicenseProvider)
	if !isProvider {
		m.mu.Unlock()
		return
	}
	if m.license != nil {
		existing := m.license.ExtensionInfo().ID
		m.mu.Unlock()
		m.log.Warn("more than one license provider compiled in; ignoring the later one",
			"using", existing, "ignored", id)
		return
	}
	m.license = lp
	m.mu.Unlock()
	m.log.Debug("license provider adopted", "id", id)
}

// LicenseStatus returns the license state reported by the provider in this
// build, and whether a provider exists at all. Callers render both cases: "no
// licensing in this build" and "licensing says X" are different answers, and
// collapsing them would leave a build with a broken license looking unlicensed
// by design.
func (m *Manager) LicenseStatus() (LicenseStatus, bool) {
	if m == nil {
		return LicenseStatus{}, false
	}
	m.mu.RLock()
	provider := m.license
	m.mu.RUnlock()
	if provider == nil {
		return LicenseStatus{}, false
	}
	var st LicenseStatus
	if err := safeCall(func() error { st = provider.LicenseStatus(); return nil }); err != nil {
		m.log.Warn("license provider panicked reporting status", "error", err)
		return LicenseStatus{Error: err.Error()}, true
	}
	return st, true
}

// enabled applies the configuration rules that decide whether an extension
// loads at all. It is shared by Load and Preview so that what `pando ext`
// advertises is exactly what a real run would load.
func (m *Manager) enabled(info Info, disabled map[ID]struct{}) bool {
	if _, off := disabled[info.ID]; off {
		return false
	}
	entry, configured := m.opts.Entries[string(info.ID)]
	if configured {
		// A configured-but-not-enabled entry stays off, unless it carries
		// configuration: writing a Config table is itself an opt-in.
		return entry.Enabled || len(entry.Config) > 0
	}
	return m.defaultEnabled(info)
}

// disabledSet builds the lookup used by enabled.
func (m *Manager) disabledSet() map[ID]struct{} {
	out := make(map[ID]struct{}, len(m.opts.Disabled))
	for _, id := range m.opts.Disabled {
		out[ID(id)] = struct{}{}
	}
	return out
}

// defaultEnabled decides what happens to an extension with no configuration
// entry at all. Bundled MIT extensions are on by default; anything else must be
// switched on explicitly, so that adding a private module to a build never
// silently changes behaviour.
//
// A LicenseProvider is the exception, and has to be. It is not a feature
// somebody opts into — it is the gate the other extensions are measured
// against. Left off by default, an operator who enabled an enterprise module
// but never thought about the licensing extension would get an ungated build,
// which is precisely the accident this whole mechanism exists to prevent. It
// can still be switched off explicitly through [Extensions] Disabled.
func (m *Manager) defaultEnabled(info Info) bool {
	if m.licenseIDs[info.ID] {
		return true
	}
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

	// A license provider must be loaded before anything it gates, so it is
	// pinned ahead of every other extension regardless of declared
	// dependencies. Depth is otherwise the dependency depth.
	depth := make(map[ID]int, len(infos))
	licenseFirst := make(map[ID]bool, 1)
	for _, info := range infos {
		if inst := previewInstance(m, info); inst != nil {
			if _, ok := inst.(LicenseProvider); ok {
				licenseFirst[info.ID] = true
			}
		}
	}
	m.licenseIDs = licenseFirst
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
	sort.SliceStable(out, func(i, j int) bool {
		if licenseFirst[out[i].ID] != licenseFirst[out[j].ID] {
			return licenseFirst[out[i].ID]
		}
		return depth[out[i].ID] < depth[out[j].ID]
	})
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
		if m.license != nil && m.license.ExtensionInfo().ID == id {
			m.license = nil
		}
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
	m.license = nil
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

// Instance returns the loaded instance of an extension, or nil when it is not
// loaded.
func (m *Manager) Instance(id ID) Extension {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loaded[id]
}

// Preview instantiates every *registered* extension and returns the instances
// implementing T, without provisioning any of them.
//
// It exists for surfaces that must be described before Pando is running: the
// CLI builds its command tree in init(), where no configuration has been read
// yet and no database may be opened. Preview therefore ignores configuration
// deliberately — help output describes what the binary contains, and whether an
// extension is enabled is checked when a command actually runs.
//
// The instances returned are throwaway and unprovisioned: read declarations
// from them, never act. To act, Load the manager and take the provisioned
// instance from Instance(id).
func Preview[T any](m *Manager) []T {
	if m == nil {
		return nil
	}
	var out []T
	for _, info := range m.registry.List() {
		if info.New == nil {
			continue
		}
		inst := previewInstance(m, info)
		if inst == nil {
			continue
		}
		if v, ok := inst.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// previewInstance builds one instance, containing a panic in a factory so that
// one malformed extension cannot stop the CLI from describing the others.
func previewInstance(m *Manager, info Info) (inst Extension) {
	defer func() {
		if r := recover(); r != nil {
			m.log.Warn("extension factory panicked during preview", "id", info.ID, "panic", r)
			inst = nil
		}
	}()
	return info.New()
}
