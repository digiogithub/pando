package uiauto

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/imageopt"
	"github.com/digiogithub/pando/internal/uiauto/core"
	"github.com/digiogithub/pando/internal/uiauto/events"
	"github.com/digiogithub/pando/internal/uiauto/input"
	browser "github.com/digiogithub/pando/internal/uiauto/platform/browser"
	"github.com/digiogithub/pando/internal/uiauto/screen"
	"github.com/digiogithub/pando/internal/uiauto/vision"
	"github.com/disintegration/imaging"
	"github.com/google/uuid"
)

// The following package-level function variables are indirections over
// internal/uiauto/input and internal/uiauto/screen, so tests can substitute
// fakes without a live display/compositor (this development box is a tty
// session with neither DISPLAY nor WAYLAND_DISPLAY set, so the real
// implementations would just report PLATFORM_NOT_SUPPORTED here anyway).
var (
	newPhysicalInput     = input.New
	physicalCapabilities = input.Capabilities
	captureScreen        = screen.Capture
	screenCapabilities   = screen.Capabilities
	listDisplays         = screen.Displays
)

// defaultSnapshotCap bounds how many snapshots a Manager's SnapshotStore
// keeps around at once (LRU-evicted beyond this).
const defaultSnapshotCap = 200

// Options configures a Manager.
type Options struct {
	// Backend selects the backend to resolve from the Registry: "auto" (the
	// default), or an explicit name ("atspi", "uia", "ax", "cdp", "null").
	Backend string

	// MaxNodes caps how many elements a single Observe call pulls from the
	// backend and how many a rendered response shows.
	MaxNodes int
	// DefaultDepth is used by Observe when the caller passes depth <= 0.
	DefaultDepth int

	// ActionTimeout is the default timeout for a single action/wait.
	ActionTimeout time.Duration
	// SnapshotTTL is how long an observed snapshot stays resolvable.
	SnapshotTTL time.Duration

	// AllowPhysicalInput enables the ActionResolver's synthetic
	// mouse/keyboard fallback when a native action is unsupported or fails.
	AllowPhysicalInput bool

	// AllowedApps, when non-empty, restricts every operation to apps whose
	// id or name matches (case-insensitively) one of these entries.
	AllowedApps []string
	// DeniedApps blocks apps whose id or name matches, regardless of
	// AllowedApps.
	DeniedApps []string

	// ScreenshotScale downsizes desktop_screenshot output on top of the
	// shared imageopt pipeline. 1.0 means "no extra scaling".
	ScreenshotScale float64

	// Inert makes the Manager a fully no-op automation surface: no
	// physical-input layer and no screen capture, so every OS-touching
	// entry point reports PLATFORM_NOT_SUPPORTED. It is set by
	// OptionsFromConfig when the user pins the "null" backend, which is
	// how the desktop policy says "no real desktop automation". Backend
	// == "null" alone does NOT imply it: uiauto's own tests pin "null" as
	// a neutral accessibility backend while injecting a fake physical
	// input layer, and that must keep working.
	Inert bool
}

// Manager is the Desktop Controller runtime. Since Block R (2026-08-30) it
// no longer binds to a single resolved core.Backend: it holds the set of
// backends actually available in this session -- an "OS" backend (atspi/
// uia/ax, or null when none is available) and, independently, the "cdp"
// backend when registered -- and routes each operation to whichever one
// should actually serve it (see backendForScope/backendForElement). It
// never performs an OS call itself; all of that lives behind core.Backend.
type Manager struct {
	// osBackend is the OS accessibility backend resolved for opts.Backend:
	// the explicit backend when opts.Backend pins one, or the winner of the
	// OS-only auto order (atspi/uia/ax/null) otherwise. Every operation
	// that is not routed to cdpBackend uses this one.
	osBackend     core.Backend
	osBackendName string

	// cdpBackend is the independently resolved CDP browser backend, or nil
	// when routing is disabled (pinned mode) or "cdp" is not registered.
	// Resolving/constructing it is side-effect free (browser.NewBackend
	// never launches a browser -- see platform/browser.CdpBackend.
	// Available), so holding this reference costs nothing even when no
	// browser session is ever opened.
	cdpBackend core.Backend

	// pinned is true when opts.Backend explicitly names a single backend
	// ("atspi", "cdp", "null", ...) rather than "auto"/"". A hard pin
	// disables routing entirely: every operation uses osBackend, even one
	// naming the browser's virtual app/window.
	pinned bool

	snapshots *core.SnapshotStore
	// physical is the platform synthetic-input layer shared by every
	// backend's ActionResolver (built fresh per call via resolverFor, since
	// which backend an action targets is now decided per ref/scope).
	physical core.PhysicalInput

	// capabilities is the default, no-scope-specific signal: osBackend's
	// Available() merged with the physical-input/screen-capture layers,
	// computed once at construction (mirrors pre-routing behavior exactly).
	// It deliberately never folds in cdpBackend's capabilities -- a caller
	// that needs the capabilities of whichever backend actually serves a
	// given scope (which may be "cdp") should call CapabilitiesFor instead.
	capabilities core.Capabilities
	physicalCaps core.Capabilities
	screenCaps   core.Capabilities

	// inert mirrors Options.Inert: the Manager must not reach the OS
	// through the side channels that bypass osBackend either -- physical
	// input (Manager.physical) and screen capture (captureScreen) are
	// wired up independently of the backend and would otherwise still
	// drive the real mouse and grab the real screen.
	inert bool

	opts Options
}

// resolveBackends resolves osBackend/cdpBackend/pinned from opts.Backend.
// A pinned (explicit, non-"auto") backend name disables routing altogether
// -- osBackend becomes that backend (or NullBackend on resolution failure)
// and cdpBackend stays nil, so backendForScope/backendForElement always
// fall through to osBackend. In "auto"/"" mode, osBackend is resolved from
// the OS-only auto order (backends.go's SetAutoOrder no longer includes
// "cdp", precisely so an OS backend winning that race can never shadow
// CDP), and cdpBackend is resolved independently and in addition -- never
// instead.
func resolveBackends(opts Options) (osBackend core.Backend, osName string, cdpBackend core.Backend, pinned bool) {
	name := strings.TrimSpace(opts.Backend)
	pinned = name != "" && name != "auto"

	osBackend, err := Registry().Resolve(opts.Backend)
	if err != nil || osBackend == nil {
		osBackend = core.NewNullBackend()
	}
	osName = osBackend.Name()

	if pinned {
		return osBackend, osName, nil, true
	}
	if cb, err := Registry().Resolve("cdp"); err == nil && cb != nil {
		cdpBackend = cb
	}
	return osBackend, osName, cdpBackend, false
}

// NewManager resolves the backends named by opts.Backend (falling back to
// the "null" OS backend when resolution fails, so a Manager can always be
// constructed) and builds the SnapshotStore/physical-input layer around
// them.
func NewManager(opts Options) (*Manager, error) {
	if opts.MaxNodes <= 0 {
		opts.MaxNodes = 500
	}
	if opts.DefaultDepth <= 0 {
		opts.DefaultDepth = 3
	}
	if opts.ActionTimeout <= 0 {
		opts.ActionTimeout = 10 * time.Second
	}
	if opts.SnapshotTTL <= 0 {
		opts.SnapshotTTL = 60 * time.Second
	}
	if opts.ScreenshotScale <= 0 {
		opts.ScreenshotScale = 1.0
	}

	osBackend, osName, cdpBackend, pinned := resolveBackends(opts)

	caps, availErr := osBackend.Available(context.Background())
	if availErr != nil {
		// Available() is documented to never itself fail on NullBackend and
		// platform backends are expected to follow the same contract
		// (report capabilities, don't error); degrade to no capabilities
		// rather than failing Manager construction.
		caps = core.Capabilities{}
	}

	// Construct the platform PhysicalInput (internal/uiauto/input),
	// gated on AllowPhysicalInput, so the native-action-first/physical-
	// fallback path in core/action.go becomes real. A construction failure
	// degrades to no physical fallback rather than failing Manager
	// construction (mirrors the backend-resolution fallback above);
	// Capabilities still reports the true, degraded picture.
	// An inert Manager never reaches the OS: no physical input layer and
	// no screen-capture capability, so every OS-touching entry point
	// reports PLATFORM_NOT_SUPPORTED instead of quietly acting on the real
	// desktop.
	inert := opts.Inert

	var physical core.PhysicalInput
	var inputCaps core.Capabilities
	if opts.AllowPhysicalInput && !inert {
		if p, err := newPhysicalInput(); err == nil {
			physical = p
		}
		inputCaps = physicalCapabilities()
	}

	// Merge in what the screen-capture layer (internal/uiauto/screen) can
	// actually deliver in this session. A capability is only ever reported
	// true when at least one underlying provider (backend, physical input,
	// or screen capture) can genuinely deliver it.
	var scrCaps core.Capabilities
	if !inert {
		scrCaps = screenCapabilities()
	}
	caps.Mouse = caps.Mouse || inputCaps.Mouse
	caps.Keyboard = caps.Keyboard || inputCaps.Keyboard
	caps.Screenshot = caps.Screenshot || scrCaps.Screenshot

	return &Manager{
		osBackend:     osBackend,
		osBackendName: osName,
		cdpBackend:    cdpBackend,
		pinned:        pinned,
		snapshots:     core.NewSnapshotStore(opts.SnapshotTTL, defaultSnapshotCap),
		physical:      physical,
		capabilities:  caps,
		physicalCaps:  inputCaps,
		screenCaps:    scrCaps,
		inert:         inert,
		opts:          opts,
	}, nil
}

// resolverFor builds an ActionResolver bound to backend and the Manager's
// shared physical-input layer. Built fresh per call (cheap: two fields and
// a bool) rather than cached on Manager, since which backend a given ref's
// action must target is now decided per call (see backendForElement) and a
// single long-lived ActionResolver can no longer assume one fixed backend.
func (m *Manager) resolverFor(backend core.Backend) *core.ActionResolver {
	return core.NewActionResolver(backend, m.physical, m.opts.AllowPhysicalInput)
}

// Capabilities reports what the default (OS) backend can actually do, in
// this session, merged with the physical-input/screen-capture layers. This
// is the same signal Manager reported before per-scope routing existed; it
// intentionally never folds in the CDP backend's capabilities, since those
// only apply to browser-scoped operations. Use CapabilitiesFor(ctx, scope)
// when the caller has a specific scope in mind (e.g. a browser window),
// so a reported capability always matches the backend that would actually
// serve that scope.
func (m *Manager) Capabilities() core.Capabilities { return m.capabilities }

// CapabilitiesFor reports the capabilities of whichever backend
// backendForScope would route scope to, merged with the same physical-
// input/screen-capture layers Capabilities() uses. A browser-window scope
// with a live CDP session reports that session's real capabilities
// (Accessibility/UIInspection/UIActions/Events); any other scope reports
// exactly Capabilities(). This never overclaims: a capability is only true
// here when the backend that would actually handle scope can offer it.
func (m *Manager) CapabilitiesFor(ctx context.Context, scope core.Scope) core.Capabilities {
	backend, name := m.backendForScope(ctx, scope)
	if backend == m.osBackend || name == m.osBackendName {
		return m.capabilities
	}
	caps, err := backend.Available(ctx)
	if err != nil {
		return core.Capabilities{}
	}
	caps.Mouse = caps.Mouse || m.physicalCaps.Mouse
	caps.Keyboard = caps.Keyboard || m.physicalCaps.Keyboard
	caps.Screenshot = caps.Screenshot || m.screenCaps.Screenshot
	return caps
}

// MaxNodes returns the configured element budget for Observe/render calls.
func (m *Manager) MaxNodes() int { return m.opts.MaxNodes }

// DefaultDepth returns the configured default Observe depth.
func (m *Manager) DefaultDepth() int { return m.opts.DefaultDepth }

// ActionTimeout returns the configured default action/wait timeout.
func (m *Manager) ActionTimeout() time.Duration { return m.opts.ActionTimeout }

// ScreenshotScale returns the configured screenshot scaling factor.
func (m *Manager) ScreenshotScale() float64 { return m.opts.ScreenshotScale }

// BackendName returns the name of the resolved default (OS) backend
// ("null" when none is available). It does not reflect whether a "cdp"
// backend is also available for routing -- see IsPinned/CdpAvailable.
func (m *Manager) BackendName() string { return m.osBackendName }

// IsPinned reports whether opts.Backend explicitly pinned a single backend,
// disabling per-scope routing.
func (m *Manager) IsPinned() bool { return m.pinned }

// CdpAvailable reports whether this Manager holds a resolved "cdp" backend
// eligible for routing (false when pinned to a different backend, or "cdp"
// is not registered). It says nothing about whether a browser session is
// currently live -- callers that need that should call CapabilitiesFor
// with a browser-app scope, or just try the operation: the CDP backend
// itself always answers honestly (APP_NOT_FOUND) when no session exists.
func (m *Manager) CdpAvailable() bool { return !m.pinned && m.cdpBackend != nil }

// Close releases both resolved backends' resources.
func (m *Manager) Close() error {
	var err error
	if m.osBackend != nil {
		if e := m.osBackend.Close(); e != nil {
			err = e
		}
	}
	if m.cdpBackend != nil {
		if e := m.cdpBackend.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// ---- Policy enforcement ----

// appMatches reports whether pattern matches an app by id or name,
// case-insensitively.
func appMatches(pattern, id, name string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	return strings.EqualFold(pattern, id) || (name != "" && strings.EqualFold(pattern, name))
}

// checkAppPolicy enforces the allow/deny application policy for an
// operation touching app (id and/or name; either may be empty). An empty id
// and name (no app context, e.g. a global key press) is always allowed.
func (m *Manager) checkAppPolicy(id, name string) error {
	if id == "" && name == "" {
		return nil
	}
	for _, denied := range m.opts.DeniedApps {
		if appMatches(denied, id, name) {
			return core.NewPolicyDeniedError(fmt.Sprintf("application %q is blocked by the desktop policy", firstNonEmpty(name, id)))
		}
	}
	if len(m.opts.AllowedApps) > 0 {
		for _, allowed := range m.opts.AllowedApps {
			if appMatches(allowed, id, name) {
				return nil
			}
		}
		return core.NewPolicyDeniedError(fmt.Sprintf("application %q is not in the desktop allow-list", firstNonEmpty(name, id)))
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---- Discovery ----

// Apps lists running applications, filtered by the allow/deny policy.
func (m *Manager) Apps(ctx context.Context) ([]core.AppInfo, error) {
	osApps, osErr := m.osBackend.Apps(ctx)
	var merged []core.AppInfo
	if osErr == nil {
		merged = append(merged, osApps...)
	}
	// Surface the connected browser as one virtual app alongside the OS
	// backend's apps, WITHOUT ever launching one: cdpBackend.Apps returns
	// errNoActiveSession cheaply (see platform/browser.CdpBackend.Available)
	// when nothing is registered, which is treated as "nothing to add",
	// never as a hard failure of the whole call.
	if m.CdpAvailable() {
		if cdpApps, err := m.cdpBackend.Apps(ctx); err == nil {
			merged = append(merged, cdpApps...)
		}
	}
	if osErr != nil && len(merged) == 0 {
		return nil, osErr
	}
	out := make([]core.AppInfo, 0, len(merged))
	for _, a := range merged {
		if m.checkAppPolicy(a.ID, a.Name) != nil {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// Windows lists the windows of appID (or all apps when appID is empty),
// enforcing the allow/deny policy for appID. When appID names the CDP
// virtual browser app (or is empty, meaning "every app"), the connected
// browser's pages are included alongside the OS backend's windows;
// otherwise only the OS backend is asked, so a native appID never triggers
// a pointless CDP round trip.
func (m *Manager) Windows(ctx context.Context, appID string) ([]core.WindowInfo, error) {
	if err := m.checkAppPolicy(appID, ""); err != nil {
		return nil, err
	}
	wantCdp := m.CdpAvailable()
	wantOS := true
	if appID != "" {
		if wantCdp && strings.EqualFold(appID, browser.AppID) {
			wantOS = false
		} else {
			wantCdp = false
		}
	}

	var merged []core.WindowInfo
	var firstErr error
	if wantOS {
		w, err := m.osBackend.Windows(ctx, appID)
		if err != nil {
			firstErr = err
		} else {
			merged = append(merged, w...)
		}
	}
	if wantCdp {
		if w, err := m.cdpBackend.Windows(ctx, appID); err == nil {
			merged = append(merged, w...)
		} else if firstErr == nil && !wantOS {
			firstErr = err
		}
	}
	if len(merged) == 0 && firstErr != nil {
		return nil, firstErr
	}
	out := make([]core.WindowInfo, 0, len(merged))
	for _, w := range merged {
		if m.checkAppPolicy(w.AppID, "") != nil {
			continue
		}
		out = append(out, w)
	}
	return out, nil
}

// ---- Routing ----

// backendForScope decides which backend should serve an operation on
// scope: the CDP backend when scope clearly names the connected browser
// (its virtual AppID, an element whose recorded Backend is "cdp", or a
// WindowID that is one of the browser's live CDP targets), the OS backend
// for everything else. Routing is entirely disabled in pinned mode
// (cdpBackend is nil), so a pinned Manager always returns osBackend
// regardless of scope -- the "hard pin" contract R1 requires. The WindowID
// check calls cdpBackend.Windows, which -- per platform/browser.CdpBackend
// -- never launches a browser and returns cheaply when no session is
// registered, so this stays free of side effects even when called on
// every Observe/Find/Wait.
func (m *Manager) backendForScope(ctx context.Context, scope core.Scope) (core.Backend, string) {
	if !m.CdpAvailable() {
		return m.osBackend, m.osBackendName
	}
	if scope.Root != nil {
		if scope.Root.Backend == "cdp" {
			return m.cdpBackend, "cdp"
		}
		return m.osBackend, m.osBackendName
	}
	if scope.AppID != "" {
		if strings.EqualFold(scope.AppID, browser.AppID) {
			return m.cdpBackend, "cdp"
		}
		return m.osBackend, m.osBackendName
	}
	if scope.WindowID != "" && m.isCdpWindow(ctx, scope.WindowID) {
		return m.cdpBackend, "cdp"
	}
	return m.osBackend, m.osBackendName
}

// isCdpWindow reports whether windowID is one of the connected browser's
// live CDP page targets.
func (m *Manager) isCdpWindow(ctx context.Context, windowID string) bool {
	wins, err := m.cdpBackend.Windows(ctx, "")
	if err != nil {
		return false
	}
	for _, w := range wins {
		if w.ID == windowID {
			return true
		}
	}
	return false
}

// backendForElement decides which backend must serve an action/read
// against an already-resolved el: whichever backend produced it
// (el.Backend, recorded by Observe/Find at snapshot time), so a later
// desktop_click/desktop_read on a ref always routes back to the exact
// backend that returned it -- across snapshots, and regardless of what the
// "current" routing decision for a fresh scope would be. Falls back to
// osBackend for a "cdp"-tagged element when routing has since been pinned
// away or cdpBackend is otherwise unavailable, and for any element tagged
// with a backend name this Manager does not recognize (e.g. a ref carried
// over from a differently-configured Manager).
func (m *Manager) backendForElement(el *core.Element) core.Backend {
	if el != nil && el.Backend == "cdp" && m.CdpAvailable() {
		return m.cdpBackend
	}
	return m.osBackend
}

// ---- Observation ----

// newSnapshotID generates a unique snapshot id (independent of
// core.SnapshotStore's internal generator) so element refs can be built
// before the snapshot is handed to the store.
func newSnapshotID() string {
	return "s" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

// rootElement resolves the root Element for scope against backend
// (already chosen by backendForScope for this scope): scope.Root if
// already set, otherwise a synthetic Element built from the matching
// WindowInfo (the first window of scope.AppID when scope.WindowID is
// empty), tagged with backendName so it, and everything Observe walks
// beneath it, carries correct provenance for later ref-addressed calls.
func (m *Manager) rootElement(ctx context.Context, backend core.Backend, backendName string, scope core.Scope) (*core.Element, error) {
	if scope.Root != nil {
		return scope.Root, nil
	}
	if err := m.checkAppPolicy(scope.AppID, ""); err != nil {
		return nil, err
	}
	windows, err := backend.Windows(ctx, scope.AppID)
	if err != nil {
		return nil, err
	}
	var win *core.WindowInfo
	if scope.WindowID != "" {
		for i := range windows {
			if windows[i].ID == scope.WindowID {
				win = &windows[i]
				break
			}
		}
		if win == nil {
			return nil, core.NewElementNotFoundError(fmt.Sprintf("window %q not found", scope.WindowID))
		}
	} else {
		if len(windows) == 0 {
			return nil, core.NewElementNotFoundError("no windows found for the requested scope")
		}
		win = &windows[0]
	}
	if err := m.checkAppPolicy(win.AppID, ""); err != nil {
		return nil, err
	}
	return &core.Element{
		Role:     core.RoleWindow,
		Name:     win.Title,
		Bounds:   win.Bounds,
		Enabled:  true,
		Visible:  true,
		Focused:  win.Focused,
		Backend:  backendName,
		AppID:    win.AppID,
		WindowID: win.ID,
	}, nil
}

// Observe builds a Snapshot of scope, walking the backend's Children tree
// up to depth levels (falling back to opts.DefaultDepth when depth <= 0)
// and opts.MaxNodes elements, assigning qualified e1..eN refs as it goes.
// Which backend serves scope is decided once, by backendForScope, and used
// for every Windows/Children call in this Observe -- and stamped onto
// every element's Backend field, so a later action against any ref this
// call returns routes back to the same backend (see backendForElement).
func (m *Manager) Observe(ctx context.Context, scope core.Scope, depth int) (*core.Snapshot, error) {
	if depth <= 0 {
		depth = m.opts.DefaultDepth
	}
	backend, backendName := m.backendForScope(ctx, scope)
	root, err := m.rootElement(ctx, backend, backendName, scope)
	if err != nil {
		return nil, err
	}

	snap := &core.Snapshot{
		ID:       newSnapshotID(),
		Backend:  backendName,
		AppID:    scope.AppID,
		WindowID: scope.WindowID,
		Elements: make(map[string]*core.Element),
	}

	counter := 0
	var assign func(el *core.Element, level int) error
	assign = func(el *core.Element, level int) error {
		counter++
		id := core.NewElementID(counter)
		el.ID = core.FormatElementRef(snap.ID, id)
		el.Backend = backendName
		snap.Elements[id] = el

		if m.opts.MaxNodes > 0 && counter >= m.opts.MaxNodes {
			return nil
		}
		if depth > 0 && level >= depth {
			return nil
		}
		children, err := backend.Children(ctx, el)
		if err != nil {
			return err
		}
		childRefs := make([]core.ElementRef, 0, len(children))
		for _, child := range children {
			if child == nil {
				continue
			}
			child.ParentID = el.ID
			if err := assign(child, level+1); err != nil {
				return err
			}
			childRefs = append(childRefs, child.ID)
			if m.opts.MaxNodes > 0 && counter >= m.opts.MaxNodes {
				break
			}
		}
		el.ChildIDs = childRefs
		return nil
	}

	if err := assign(root, 0); err != nil {
		return nil, err
	}
	snap.Root = root
	m.snapshots.Put(snap)
	return snap, nil
}

// Find resolves selectorStr within scope, returning up to limit matches
// (limit <= 0 means backend default) and storing them as a fresh Snapshot
// so the caller gets qualified refs back. Routed exactly like Observe: one
// backendForScope decision for the whole call, stamped onto every result
// element's Backend field.
func (m *Manager) Find(ctx context.Context, scope core.Scope, selectorStr string, limit int) ([]*core.Element, *core.Snapshot, error) {
	if err := m.checkAppPolicy(scope.AppID, ""); err != nil {
		return nil, nil, err
	}
	sel, err := core.ParseSelector(selectorStr)
	if err != nil {
		return nil, nil, err
	}
	backend, backendName := m.backendForScope(ctx, scope)
	elements, err := backend.Find(ctx, scope, sel, limit)
	if err != nil {
		return nil, nil, err
	}

	snap := &core.Snapshot{
		ID:       newSnapshotID(),
		Backend:  backendName,
		AppID:    scope.AppID,
		WindowID: scope.WindowID,
		Origin:   sel,
		Elements: make(map[string]*core.Element),
	}
	for i, el := range elements {
		if el == nil {
			continue
		}
		id := core.NewElementID(i + 1)
		el.ID = core.FormatElementRef(snap.ID, id)
		el.Backend = backendName
		snap.Elements[id] = el
	}
	m.snapshots.Put(snap)
	return elements, snap, nil
}

// Read resolves ref against the SnapshotStore and returns the element,
// enforcing the app policy for its owning application.
func (m *Manager) Read(ctx context.Context, ref core.ElementRef) (*core.Element, error) {
	_, el, err := m.snapshots.Resolve(ref)
	if err != nil {
		return nil, err
	}
	if err := m.checkAppPolicy(el.AppID, ""); err != nil {
		return nil, err
	}
	return el, nil
}

// resolveForAction resolves ref via the SnapshotStore, enforces policy, and
// determines which backend must perform the action -- always the backend
// that produced this specific ref (backendForElement), regardless of what
// the "current" routing decision for a fresh scope would be. Shared by
// every mutating action method.
func (m *Manager) resolveForAction(ref core.ElementRef) (*core.Element, core.Backend, error) {
	_, el, err := m.snapshots.Resolve(ref)
	if err != nil {
		return nil, nil, err
	}
	if err := m.checkAppPolicy(el.AppID, ""); err != nil {
		return nil, nil, err
	}
	return el, m.backendForElement(el), nil
}

// ---- Actions ----

// Click resolves ref and performs a click (native invoke, physical
// fallback), on the backend that produced ref.
func (m *Manager) Click(ctx context.Context, ref core.ElementRef) (*core.ActionResult, error) {
	el, backend, err := m.resolveForAction(ref)
	if err != nil {
		return nil, err
	}
	return m.resolverFor(backend).Click(ctx, el)
}

// Type resolves ref and enters text (native setvalue/type, physical
// fallback), on the backend that produced ref.
func (m *Manager) Type(ctx context.Context, ref core.ElementRef, text string) (*core.ActionResult, error) {
	el, backend, err := m.resolveForAction(ref)
	if err != nil {
		return nil, err
	}
	return m.resolverFor(backend).Type(ctx, el, text)
}

// Key sends a key/chord. When ref is empty, the key is sent globally
// (physical input only, backend-independent); otherwise it targets the
// resolved element on the backend that produced it.
func (m *Manager) Key(ctx context.Context, ref core.ElementRef, key string) (*core.ActionResult, error) {
	if ref == "" {
		return m.resolverFor(m.osBackend).Press(ctx, nil, key)
	}
	el, backend, err := m.resolveForAction(ref)
	if err != nil {
		return nil, err
	}
	return m.resolverFor(backend).Press(ctx, el, key)
}

// Scroll resolves ref and scrolls it by amount (native scroll, physical
// fallback), on the backend that produced ref.
func (m *Manager) Scroll(ctx context.Context, ref core.ElementRef, amount int) (*core.ActionResult, error) {
	el, backend, err := m.resolveForAction(ref)
	if err != nil {
		return nil, err
	}
	return m.resolverFor(backend).Scroll(ctx, el, amount)
}

// Focus resolves ref and focuses it (native focus, physical click
// fallback), on the backend that produced ref.
func (m *Manager) Focus(ctx context.Context, ref core.ElementRef) (*core.ActionResult, error) {
	el, backend, err := m.resolveForAction(ref)
	if err != nil {
		return nil, err
	}
	return m.resolverFor(backend).Focus(ctx, el)
}

// ---- Vision fallback ----
//
// The methods below are the coordinate-based action path
// (internal/uiauto/vision): acting at an explicit screen position rather
// than on an element ref, for when a region of the screen exposes no
// usable accessibility semantics (canvas apps, remote desktop windows,
// games, a broken accessibility implementation). They deliberately bypass
// core.ActionResolver/the backend entirely -- there is no "native" method
// here, only a direct physical input call -- so callers (the desktop_*
// tools) must mark every result "source":"vision" themselves; see
// internal/llm/tools/desktop_click_at.go.

// SemanticAvailable reports whether the resolved backend can plausibly
// answer this session's desktop_observe/desktop_find calls at all
// (Accessibility+UIActions capabilities). It is a coarse, cheap signal
// tools use to nudge the model toward the semantic path first -- vision
// fallback is genuinely a fallback, not a shortcut -- without forcing an
// actual (expensive) Find call on every desktop_click_at invocation. A
// false result means the semantic path is guaranteed unusable right now;
// a true result does not guarantee a specific selector will resolve, only
// that trying it is worthwhile before reaching for coordinates.
func (m *Manager) SemanticAvailable() bool {
	return m.capabilities.Accessibility && m.capabilities.UIActions
}

// validateCoordinates checks (x,y) against the real, currently reported
// display bounds (internal/uiauto/screen.Displays), returning
// INVALID_ARGS when out of range. When displays cannot be enumerated at
// all, validation is skipped rather than blocking every coordinate action
// on an unrelated capability gap.
func (m *Manager) validateCoordinates(x, y int) error {
	displays, err := listDisplays()
	if err != nil || len(displays) == 0 {
		return nil
	}
	bounds := make([]core.Bounds, 0, len(displays))
	for _, d := range displays {
		bounds = append(bounds, d.Bounds)
	}
	return vision.ValidateCoordinates(x, y, bounds)
}

// ClickAt performs a raw, ref-less coordinate click via physical input,
// bypassing the accessibility tree entirely -- the action path a model
// uses after looking at a (optionally grid-annotated) desktop_screenshot
// and picking a point. It is gated on Options.AllowPhysicalInput (the
// same knob that gates every other physical-input fallback) and validates
// (x,y) against the real display bounds before anything reaches the OS.
// The caller (internal/llm/tools/desktop_click_at.go) is responsible for
// the permission.Service prompt and for marking the response
// "source":"vision".
func (m *Manager) ClickAt(ctx context.Context, x, y int) (*core.ActionResult, error) {
	if !m.opts.AllowPhysicalInput {
		return nil, core.NewPolicyDeniedError("coordinate actions require DesktopAllowPhysicalInput to be enabled in the desktop policy")
	}
	if m.physical == nil {
		return nil, core.NewPlatformNotSupportedError("no physical input backend is available on this platform/session")
	}
	if err := m.validateCoordinates(x, y); err != nil {
		return nil, err
	}
	if err := m.physical.Click(x, y); err != nil {
		return nil, core.NewActionFailedError(fmt.Sprintf("physical click at (%d,%d) failed: %s", x, y, err.Error()))
	}
	return &core.ActionResult{Method: "physical", Action: core.Action{Kind: core.ActionInvoke}}, nil
}

// Wait polls selectorStr within scope until cond is satisfied or timeout
// elapses (timeout <= 0 uses opts.ActionTimeout), against whichever
// backend backendForScope routes scope to.
func (m *Manager) Wait(ctx context.Context, scope core.Scope, selectorStr string, cond core.Condition, timeout time.Duration) (*core.Element, error) {
	if err := m.checkAppPolicy(scope.AppID, ""); err != nil {
		return nil, err
	}
	sel, err := core.ParseSelector(selectorStr)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = m.opts.ActionTimeout
	}
	locator := core.NewLocator(scope, sel)
	backend, _ := m.backendForScope(ctx, scope)

	// A backend may optionally implement events.Subscriber (in addition
	// to core.Backend) to push live UI-change events instead of being
	// polled -- events.WaitFor prefers that live path and transparently
	// falls back to core.WaitFor's polling loop when the backend does
	// not implement it or the subscription itself fails. Detected purely
	// by type assertion: no core.Backend/Capabilities change was needed
	// for this.
	var sub events.Subscriber
	if s, ok := backend.(events.Subscriber); ok {
		sub = s
	}
	return events.WaitFor(ctx, backend, sub, locator, cond, timeout)
}

// parseScreenshotTarget resolves the desktop_screenshot "target" string
// into a screen.Target: "" / "screen" captures the whole display,
// "window:<id>" captures that window (screen.Capture falls back to
// whole-screen on platforms with no native window-scoped capture; there is
// no crop step here since capturing by WindowID that way already returns
// the right content on the platforms that do support it), and a qualified
// element ref ("@sXXXX:eYYYY") resolves the element and captures its
// Bounds as a screen Region.
func (m *Manager) parseScreenshotTarget(target string) (screen.Target, error) {
	target = strings.TrimSpace(target)
	if target == "" || target == "screen" {
		return screen.Target{}, nil
	}
	if strings.HasPrefix(target, "window:") {
		return screen.Target{WindowID: strings.TrimPrefix(target, "window:")}, nil
	}
	if strings.HasPrefix(target, "@") {
		_, el, err := m.snapshots.Resolve(core.ElementRef(target))
		if err != nil {
			return screen.Target{}, err
		}
		if err := m.checkAppPolicy(el.AppID, ""); err != nil {
			return screen.Target{}, err
		}
		if el.Bounds.Empty() {
			return screen.Target{}, core.NewInvalidArgsError("element " + target + " has no bounds to crop a screenshot to")
		}
		b := el.Bounds
		return screen.Target{Region: &b}, nil
	}
	return screen.Target{}, core.NewInvalidArgsError("unrecognized screenshot target " + strconv.Quote(target))
}

// Screenshot captures target ("screen" (default), "window:<id>", or a
// qualified element ref meaning "crop to its bounds") via
// internal/uiauto/screen, honors Options.ScreenshotScale, encodes the
// result as PNG, and passes it through the shared internal/imageopt
// pipeline exactly as the other image-producing tools do (see
// internal/llm/tools/browser_screenshot.go, image_crop.go). When grid is
// true, a light coordinate grid + axis labels (internal/uiauto/vision.
// DrawGrid) is overlaid before any scaling, so labels always read real,
// unscaled screen coordinates -- the coordinates desktop_click_at expects.
func (m *Manager) Screenshot(ctx context.Context, target string, grid bool) ([]byte, string, error) {
	if m.inert {
		return nil, "", core.NewPlatformNotSupportedError("no screen capture backend is available on this platform/session")
	}
	scrTarget, err := m.parseScreenshotTarget(target)
	if err != nil {
		return nil, "", err
	}

	img, err := captureScreen(ctx, scrTarget)
	if err != nil {
		return nil, "", err
	}

	if grid {
		img = vision.DrawGrid(img, vision.GridOptions{})
	}

	if scale := m.opts.ScreenshotScale; scale > 0 && scale != 1.0 {
		b := img.Bounds()
		w := int(float64(b.Dx()) * scale)
		h := int(float64(b.Dy()) * scale)
		if w > 0 && h > 0 {
			img = imaging.Resize(img, w, h, imaging.Lanczos)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", core.NewActionFailedError("failed to encode screenshot as PNG: " + err.Error())
	}

	normalized, _, _, nerr := imageopt.Normalize(buf.Bytes(), imageopt.MIMEPNG, imageopt.Options{AutoResize: true})
	if nerr != nil {
		// Normalize failing (e.g. an already-tiny image) is not fatal:
		// fall back to the un-normalized PNG bytes rather than losing the
		// screenshot.
		normalized = buf.Bytes()
	}
	return normalized, imageopt.MIMEPNG, nil
}

// ---- Shared singleton ----

var (
	sharedMu     sync.Mutex
	sharedMgr    *Manager
	sharedKey    desktopOptionsKey
	sharedLoaded bool
)

// desktopOptionsKey is the subset of config.InternalToolsConfig that affects
// Manager construction, used to detect when Shared() must rebuild it.
type desktopOptionsKey struct {
	backend            string
	maxNodes           int
	defaultDepth       int
	actionTimeout      int
	snapshotTTL        int
	allowPhysicalInput bool
	screenshotScale    float64
	allowedApps        string
	deniedApps         string
}

func optionsKeyFrom(it config.InternalToolsConfig) desktopOptionsKey {
	return desktopOptionsKey{
		backend:            it.DesktopBackend,
		maxNodes:           it.DesktopMaxNodes,
		defaultDepth:       it.DesktopDefaultDepth,
		actionTimeout:      it.DesktopActionTimeout,
		snapshotTTL:        it.DesktopSnapshotTTL,
		allowPhysicalInput: it.DesktopAllowPhysicalInput,
		screenshotScale:    it.DesktopScreenshotScale,
		allowedApps:        strings.Join(it.DesktopAllowedApps, ","),
		deniedApps:         strings.Join(it.DesktopDeniedApps, ","),
	}
}

// OptionsFromConfig converts the relevant InternalToolsConfig fields into
// Options.
func OptionsFromConfig(it config.InternalToolsConfig) Options {
	backend := strings.TrimSpace(it.DesktopBackend)
	if backend == "" {
		backend = "auto"
	}
	return Options{
		Backend:            backend,
		Inert:              backend == nullBackendName,
		MaxNodes:           it.DesktopMaxNodes,
		DefaultDepth:       it.DesktopDefaultDepth,
		ActionTimeout:      time.Duration(it.DesktopActionTimeout) * time.Second,
		SnapshotTTL:        time.Duration(it.DesktopSnapshotTTL) * time.Second,
		AllowPhysicalInput: it.DesktopAllowPhysicalInput,
		AllowedApps:        it.DesktopAllowedApps,
		DeniedApps:         it.DesktopDeniedApps,
		ScreenshotScale:    it.DesktopScreenshotScale,
	}
}

// Shared returns the process-wide Manager singleton, lazily building it (or
// rebuilding it, on relevant config change) from config.Get().InternalTools,
// mirroring browser_session.go's shared-session pattern.
func Shared() (*Manager, error) {
	sharedMu.Lock()
	defer sharedMu.Unlock()

	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("uiauto: configuration not loaded")
	}
	key := optionsKeyFrom(cfg.InternalTools)
	if sharedLoaded && sharedMgr != nil && key == sharedKey {
		return sharedMgr, nil
	}

	mgr, err := NewManager(OptionsFromConfig(cfg.InternalTools))
	if err != nil {
		return nil, err
	}
	if sharedMgr != nil {
		_ = sharedMgr.Close()
	}
	sharedMgr = mgr
	sharedKey = key
	sharedLoaded = true
	return sharedMgr, nil
}

// ResetShared discards the shared Manager singleton so the next Shared()
// call rebuilds it from the current configuration. Exposed for tests.
func ResetShared() {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if sharedMgr != nil {
		_ = sharedMgr.Close()
	}
	sharedMgr = nil
	sharedLoaded = false
}
