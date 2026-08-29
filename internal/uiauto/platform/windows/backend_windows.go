//go:build windows

package windows

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// Backend is the core.Backend implementation talking to Windows UI
// Automation over COM (github.com/go-ole/go-ole plus this package's
// hand-written vtable calls), mirroring the structure of the Phase 2 Linux
// AT-SPI2 backend (internal/uiauto/platform/linux). See doc.go for the COM
// threading model and the per-tree-level caching strategy.
//
// Element identity: every core.Element this backend returns carries its
// UIA RuntimeId (encoded, see runtimeid.go) in Native.Data. The live
// IUIAutomationElement COM pointer itself is never exposed outside this
// package; it lives only in handles, a mutex-guarded table from encoded
// RuntimeId to live *uiaElement, released in full by Close. A ref whose
// RuntimeId is not (or no longer) in handles resolves as STALE_REF/
// ELEMENT_NOT_FOUND (resolveElement), never a crash.
type Backend struct {
	mu       sync.Mutex
	worker   *comWorker
	auto     *automation
	root     *uiaElement
	trueCond *comObject
	cache    *cacheRequest
	handles  map[string]*uiaElement
	// connectErr is set once ensure() has been attempted and failed, so
	// repeated calls do not keep retrying a COM setup that is not going to
	// succeed (mirrors the once-per-process-lifetime nature of a failed
	// CoCreateInstance on a machine with no UIA provider registered).
	connectErr error
	connected  bool
}

// NewBackend constructs a Backend. It never fails: COM setup happens
// lazily on first use via ensure, mirroring the Linux backend's
// AtspiBackend.NewBackend/ensureConn contract so backend construction
// itself can never fail Manager.NewManager.
func NewBackend() (core.Backend, error) {
	return &Backend{handles: make(map[string]*uiaElement)}, nil
}

// Name implements core.Backend.
func (b *Backend) Name() string { return "uia" }

// ensure lazily starts the COM apartment worker thread and creates the
// IUIAutomation object, root element, TrueCondition and CacheRequest this
// backend reuses for every call. It is idempotent and safe for concurrent
// callers.
func (b *Backend) ensure(ctx context.Context) error {
	b.mu.Lock()
	if b.connected {
		b.mu.Unlock()
		return nil
	}
	if b.connectErr != nil {
		err := b.connectErr
		b.mu.Unlock()
		return err
	}
	b.mu.Unlock()

	worker, err := newComWorker()
	if err != nil {
		de := core.NewPermDeniedError("could not initialize COM (CoInitializeEx) for the UI Automation worker thread: " + err.Error())
		b.mu.Lock()
		b.connectErr = de
		b.mu.Unlock()
		return de
	}

	var (
		auto     *automation
		root     *uiaElement
		trueCond *comObject
		cache    *cacheRequest
		setupErr error
	)
	runErr := worker.run(ctx, func() {
		a, err := newAutomation()
		if err != nil {
			setupErr = core.NewPermDeniedError("could not create the CUIAutomation COM object: " + err.Error())
			return
		}
		auto = a
		rootObj, err := auto.getRootElement()
		if err != nil {
			setupErr = err
			return
		}
		root = &uiaElement{obj: rootObj}
		tc, err := auto.createTrueCondition()
		if err != nil {
			setupErr = err
			return
		}
		trueCond = tc
		cr, err := auto.createCacheRequest()
		if err != nil {
			setupErr = err
			return
		}
		cache = cr
	})
	if runErr != nil {
		worker.stop()
		return runErr
	}
	if setupErr != nil {
		worker.stop()
		b.mu.Lock()
		b.connectErr = setupErr
		b.mu.Unlock()
		return setupErr
	}

	b.mu.Lock()
	b.worker = worker
	b.auto = auto
	b.root = root
	b.trueCond = trueCond
	b.cache = cache
	b.connected = true
	b.mu.Unlock()
	return nil
}

// Available implements core.Backend. Like the Linux backend, it never
// itself returns an error: a successful lazy connect reports
// Accessibility/UIInspection/UIActions true; anything else degrades to an
// all-false Capabilities so Manager falls back gracefully.
func (b *Backend) Available(ctx context.Context) (core.Capabilities, error) {
	ok := b.ensure(ctx) == nil
	return core.Capabilities{
		Accessibility: ok,
		UIInspection:  ok,
		UIActions:     ok,
	}, nil
}

// Close implements core.Backend: releases every live COM pointer this
// backend has ever handed out (the handle table), the shared TrueCondition/
// CacheRequest/root/IUIAutomation objects, then stops the COM apartment
// worker (CoUninitialize runs on the same thread CoInitializeEx did).
func (b *Backend) Close() error {
	b.mu.Lock()
	worker := b.worker
	auto := b.auto
	root := b.root
	trueCond := b.trueCond
	cache := b.cache
	handles := b.handles
	b.handles = make(map[string]*uiaElement)
	b.connected = false
	b.mu.Unlock()

	if worker == nil {
		return nil
	}
	_ = worker.run(context.Background(), func() {
		for _, h := range handles {
			h.release()
		}
		if cache != nil {
			cache.release()
		}
		if trueCond != nil {
			trueCond.Release()
		}
		if root != nil {
			root.release()
		}
		auto.release()
	})
	worker.stop()
	return nil
}

// registerHandle stores el under its encoded RuntimeId in the persistent
// handle table (releasing whatever pointer previously lived under that key,
// if any, other than el itself), so a later Children/Perform/Find(scope.Root)
// call can resolve the same element back to a live COM pointer without
// re-searching. Known limitation: entries are never evicted before Close —
// see the Phase 4 KB summary's deferred-items section.
func (b *Backend) registerHandle(id string, el *uiaElement) {
	if id == "" || el == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.handles[id]; ok && old != el {
		old.release()
	}
	b.handles[id] = el
}

func (b *Backend) lookupHandle(id string) (*uiaElement, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.handles[id]
	return e, ok
}

// resolveElement recovers the live *uiaElement el was built from: its
// encoded RuntimeId (stashed by buildElement in Native.Data) looked up in
// the handle table, or — for a synthetic root Element the Manager builds
// straight from a WindowInfo (Native unset, only AppID/WindowID) — its
// WindowID (which this backend always sets to the window's own encoded
// RuntimeId, see Windows()) looked up the same way. Mirrors the Linux
// backend's refFromElement.
func (b *Backend) resolveElement(el *core.Element) (*uiaElement, error) {
	if el == nil {
		return nil, core.NewInvalidArgsError("nil element")
	}
	if id := runtimeIDOf(el); id != "" {
		if e, ok := b.lookupHandle(id); ok {
			return e, nil
		}
		return nil, core.NewStaleRefError("uia element with runtime id " + id + " is no longer available; re-observe or re-find it")
	}
	if el.WindowID != "" {
		if e, ok := b.lookupHandle(el.WindowID); ok {
			return e, nil
		}
		return nil, core.NewStaleRefError("uia window with id " + el.WindowID + " is no longer available; re-observe or re-find it")
	}
	return nil, core.NewElementNotFoundError("element does not carry a UIA runtime id or window id; re-observe or re-find it")
}

// topLevelWindows returns every direct child of the desktop root (i.e.
// every top-level window UIA exposes), each already resolved to a
// cachedProps + registered in the handle table.
func (b *Backend) topLevelWindows(ctx context.Context) ([]*uiaElement, []cachedProps, error) {
	if err := b.ensure(ctx); err != nil {
		return nil, nil, err
	}
	var (
		children []*uiaElement
		fetchErr error
	)
	runErr := b.worker.run(ctx, func() {
		children, fetchErr = findChildrenBuildCache(b.root, b.trueCond, b.cache)
	})
	if runErr != nil {
		return nil, nil, runErr
	}
	if fetchErr != nil {
		return nil, nil, fetchErr
	}
	props := make([]cachedProps, 0, len(children))
	kept := make([]*uiaElement, 0, len(children))
	for _, c := range children {
		var (
			p   cachedProps
			err error
		)
		runErr := b.worker.run(ctx, func() { p, err = fetchCachedProps(c) })
		if runErr != nil {
			return nil, nil, runErr
		}
		if err != nil {
			c.release()
			continue
		}
		id := EncodeRuntimeID(p.RuntimeID)
		b.registerHandle(id, c)
		props = append(props, p)
		kept = append(kept, c)
	}
	return kept, props, nil
}

// Apps implements core.Backend by grouping the desktop's top-level windows
// by owning process id, resolving each distinct process's executable name
// via process_windows.go.
func (b *Backend) Apps(ctx context.Context) ([]core.AppInfo, error) {
	_, props, err := b.topLevelWindows(ctx)
	if err != nil {
		return nil, err
	}
	order := make([]int32, 0)
	counts := make(map[int32]int)
	for _, p := range props {
		if _, seen := counts[p.ProcessID]; !seen {
			order = append(order, p.ProcessID)
		}
		counts[p.ProcessID]++
	}
	out := make([]core.AppInfo, 0, len(order))
	for _, pid := range order {
		out = append(out, core.AppInfo{
			ID:      strconv.Itoa(int(pid)),
			Name:    processName(uint32(pid)),
			PID:     int(pid),
			Windows: counts[pid],
		})
	}
	return out, nil
}

// Windows implements core.Backend, listing the desktop's top-level windows
// (optionally filtered to one process, matched by pid string or process
// name, case-insensitive).
func (b *Backend) Windows(ctx context.Context, appID string) ([]core.WindowInfo, error) {
	elems, props, err := b.topLevelWindows(ctx)
	if err != nil {
		return nil, err
	}
	var wantPID int32 = -1
	var wantName string
	if appID != "" {
		if n, perr := strconv.Atoi(appID); perr == nil {
			wantPID = int32(n)
		} else {
			wantName = strings.ToLower(appID)
		}
	}

	var out []core.WindowInfo
	for i, p := range props {
		if appID != "" {
			if wantPID >= 0 {
				if p.ProcessID != wantPID {
					continue
				}
			} else if !strings.Contains(strings.ToLower(processName(uint32(p.ProcessID))), wantName) {
				continue
			}
		}
		id := EncodeRuntimeID(p.RuntimeID)
		out = append(out, core.WindowInfo{
			ID:      id,
			AppID:   strconv.Itoa(int(p.ProcessID)),
			Title:   p.Name,
			Bounds:  p.Bounds,
			Focused: p.KeyboardFocus,
		})
		_ = elems[i] // already registered in the handle table by topLevelWindows
	}
	if appID != "" && len(out) == 0 {
		return nil, core.NewAppNotFoundError("no running application/top-level window matches app id/name " + appID)
	}
	return out, nil
}

// uiaProvider adapts Backend to the platform-independent childProvider
// interface findRec (traverse.go) depends on: it dispatches the actual COM
// call onto the worker thread and registers every discovered child into
// the backend's persistent handle table (see registerHandle's documented
// limitation) before handing findRec the plain treeNode value.
type uiaProvider struct {
	b *Backend
}

func (p *uiaProvider) childrenOf(ctx context.Context, id string) ([]treeNode, error) {
	parent, ok := p.b.lookupHandle(id)
	if !ok {
		return nil, core.NewStaleRefError("uia element handle " + id + " is no longer available")
	}
	var (
		children []*uiaElement
		fetchErr error
	)
	if runErr := p.b.worker.run(ctx, func() {
		children, fetchErr = findChildrenBuildCache(parent, p.b.trueCond, p.b.cache)
	}); runErr != nil {
		return nil, runErr
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	out := make([]treeNode, 0, len(children))
	for _, c := range children {
		var (
			props cachedProps
			err   error
		)
		if runErr := p.b.worker.run(ctx, func() { props, err = fetchCachedProps(c) }); runErr != nil {
			return nil, runErr
		}
		if err != nil {
			c.release()
			continue
		}
		key := EncodeRuntimeID(props.RuntimeID)
		p.b.registerHandle(key, c)
		out = append(out, treeNode{id: key, props: props})
	}
	return out, nil
}

// resolveScopeRoots resolves the starting treeNode(s) + owning-process id
// for a Find call from scope, mirroring the Linux backend's function of the
// same name: scope.Root when set, the named window when
// AppID+WindowID/WindowID are set, the whole app's windows when only AppID
// is set, or every top-level window (a bounded, selector-pruned search)
// when scope carries no app/window context at all.
func (b *Backend) resolveScopeRoots(ctx context.Context, scope core.Scope) ([]treeNode, string, error) {
	if scope.Root != nil {
		e, err := b.resolveElement(scope.Root)
		if err != nil {
			return nil, "", err
		}
		var (
			props cachedProps
			ferr  error
		)
		if runErr := b.worker.run(ctx, func() { props, ferr = fetchCachedProps(e) }); runErr != nil {
			return nil, "", runErr
		}
		if ferr != nil {
			return nil, "", ferr
		}
		return []treeNode{{id: EncodeRuntimeID(props.RuntimeID), props: props}}, scope.Root.AppID, nil
	}

	_, allProps, err := b.topLevelWindows(ctx)
	if err != nil {
		return nil, "", err
	}
	if scope.WindowID != "" {
		for _, p := range allProps {
			if EncodeRuntimeID(p.RuntimeID) == scope.WindowID {
				return []treeNode{{id: scope.WindowID, props: p}}, strconv.Itoa(int(p.ProcessID)), nil
			}
		}
		return nil, "", core.NewElementNotFoundError("window " + scope.WindowID + " not found")
	}
	if scope.AppID == "" {
		var out []treeNode
		for _, p := range allProps {
			out = append(out, treeNode{id: EncodeRuntimeID(p.RuntimeID), props: p})
		}
		return out, "", nil
	}

	var wantPID int32 = -1
	var wantName string
	if n, perr := strconv.Atoi(scope.AppID); perr == nil {
		wantPID = int32(n)
	} else {
		wantName = strings.ToLower(scope.AppID)
	}
	var out []treeNode
	for _, p := range allProps {
		if wantPID >= 0 {
			if p.ProcessID != wantPID {
				continue
			}
		} else if !strings.Contains(strings.ToLower(processName(uint32(p.ProcessID))), wantName) {
			continue
		}
		out = append(out, treeNode{id: EncodeRuntimeID(p.RuntimeID), props: p})
	}
	if len(out) == 0 {
		return nil, "", core.NewAppNotFoundError("no running application/top-level window matches app id/name " + scope.AppID)
	}
	return out, scope.AppID, nil
}

// Find implements core.Backend with the selector-driven, depth-capped,
// limit-capped, ctx-aware traversal in traverse.go — it never walks the
// whole desktop tree, and each level costs exactly one FindAllBuildCache
// cross-process call (see doc.go).
func (b *Backend) Find(ctx context.Context, scope core.Scope, sel *core.Selector, limit int) ([]*core.Element, error) {
	if err := b.ensure(ctx); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultFindLimit
	}
	maxDepth := scope.Depth
	if maxDepth <= 0 {
		maxDepth = defaultFindDepth
	}
	roots, appID, err := b.resolveScopeRoots(ctx, scope)
	if err != nil {
		return nil, err
	}
	provider := &uiaProvider{b: b}
	var results []*core.Element
	for _, root := range roots {
		if len(results) >= limit {
			break
		}
		if err := findRec(ctx, provider, sel, b.Name(), appID, root, findState{ds: []int{0}}, 0, maxDepth, &results, limit); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// Children implements core.Backend: the direct children of el, fetched
// with a single FindAllBuildCache cross-process call.
func (b *Backend) Children(ctx context.Context, el *core.Element) ([]*core.Element, error) {
	if err := b.ensure(ctx); err != nil {
		return nil, err
	}
	e, err := b.resolveElement(el)
	if err != nil {
		return nil, err
	}
	provider := &uiaProvider{b: b}
	nodes, err := provider.childrenOf(ctx, runtimeIDOrWindowID(el))
	if err != nil {
		return nil, err
	}
	_ = e // resolved for its error-checking side effect (STALE_REF/ELEMENT_NOT_FOUND)
	out := make([]*core.Element, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, buildElement(b.Name(), el.AppID, "", n.props))
	}
	return out, nil
}

func runtimeIDOrWindowID(el *core.Element) string {
	if id := runtimeIDOf(el); id != "" {
		return id
	}
	return el.WindowID
}

// Properties implements core.Backend: re-reads el's cached property set
// (a same-process, non-blocking call — see uielement_windows.go) and
// returns it as a generic map, plus the raw ControlType/AutomationId/
// ClassName already stashed in Native.Data. props is accepted for
// interface-parity with the Linux backend but is not used to gate which
// properties are read, since the whole fixed set is already cached
// locally at zero extra cost — unlike AT-SPI's Text/actions reads, nothing
// here costs an extra cross-process round trip.
func (b *Backend) Properties(ctx context.Context, el *core.Element, props []string) (map[string]any, error) {
	if err := b.ensure(ctx); err != nil {
		return nil, err
	}
	e, err := b.resolveElement(el)
	if err != nil {
		return nil, err
	}
	var (
		p    cachedProps
		ferr error
	)
	if runErr := b.worker.run(ctx, func() { p, ferr = fetchCachedProps(e) }); runErr != nil {
		return nil, runErr
	}
	if ferr != nil {
		return nil, ferr
	}
	return map[string]any{
		"rawControlType": ControlTypeName(p.ControlType),
		"controlTypeId":  p.ControlType,
		"automationId":   p.AutomationID,
		"className":      p.ClassName,
		"processId":      p.ProcessID,
		"bounds":         p.Bounds,
	}, nil
}

// Perform implements core.Backend, dispatching to the pattern handlers in
// patterns_windows.go on the COM worker thread.
func (b *Backend) Perform(ctx context.Context, el *core.Element, action core.Action) error {
	if err := b.ensure(ctx); err != nil {
		return err
	}
	e, err := b.resolveElement(el)
	if err != nil {
		return err
	}
	var actErr error
	if runErr := b.worker.run(ctx, func() { actErr = performAction(e, action) }); runErr != nil {
		return runErr
	}
	return actErr
}
