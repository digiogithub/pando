package linux

import (
	"context"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// AtspiBackend is the core.Backend implementation talking to Linux AT-SPI2
// over D-Bus. The bus connection is established lazily on first use (not in
// NewBackend, so backend construction itself can never fail) and cached;
// Available reports honestly instead of erroring when the accessibility
// bus/session is unreachable, matching the contract NullBackend and
// Manager document.
type AtspiBackend struct {
	mu     sync.Mutex
	conn   *dbusConn
	events *eventSource
}

// NewBackend constructs an AtspiBackend. It never fails: connecting to the
// accessibility bus happens lazily on the first real operation.
func NewBackend() (core.Backend, error) {
	return &AtspiBackend{}, nil
}

// Name implements core.Backend.
func (b *AtspiBackend) Name() string { return "atspi" }

func (b *AtspiBackend) ensureConn(ctx context.Context) (*dbusConn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		return b.conn, nil
	}
	conn, err := connectA11yBus(ctx)
	if err != nil {
		return nil, err
	}
	b.conn = conn
	return conn, nil
}

// Available implements core.Backend. It never returns an error: a
// reachable a11y bus reports Accessibility/UIInspection/UIActions true;
// anything else degrades to an all-false Capabilities so callers fall back
// gracefully (see the Manager/NullBackend contract this mirrors).
func (b *AtspiBackend) Available(ctx context.Context) (core.Capabilities, error) {
	_, err := b.ensureConn(ctx)
	return detectCapabilities(err == nil), nil
}

// Close implements core.Backend.
func (b *AtspiBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.events != nil {
		b.events.close()
		b.events = nil
	}
	if b.conn == nil {
		return nil
	}
	err := b.conn.close()
	b.conn = nil
	return err
}

// Apps implements core.Backend by listing the AT-SPI2 registry's top-level
// children, one per running (a11y-registered) application.
func (b *AtspiBackend) Apps(ctx context.Context) ([]core.AppInfo, error) {
	conn, err := b.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	appRefs, err := listAppRefs(ctx, conn)
	if err != nil {
		return nil, err
	}
	out := make([]core.AppInfo, 0, len(appRefs))
	for _, ref := range appRefs {
		n, err := fetchNode(ctx, conn, ref)
		if err != nil {
			continue // skip an app that vanished/errored mid-listing
		}
		out = append(out, core.AppInfo{
			ID:      ref.Bus,
			Name:    n.name,
			Windows: int(n.childCount),
		})
	}
	return out, nil
}

// Windows implements core.Backend by listing the direct children (frames/
// windows/dialogs) of the matching application(s).
func (b *AtspiBackend) Windows(ctx context.Context, appID string) ([]core.WindowInfo, error) {
	conn, err := b.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	appRefs, err := listAppRefs(ctx, conn)
	if err != nil {
		return nil, err
	}

	var targets []accessibleRef
	if appID == "" {
		targets = appRefs
	} else {
		for _, ref := range appRefs {
			if ref.Bus == appID {
				targets = append(targets, ref)
				continue
			}
			if n, err := fetchNode(ctx, conn, ref); err == nil && strings.EqualFold(n.name, appID) {
				targets = append(targets, ref)
			}
		}
		if len(targets) == 0 {
			return nil, core.NewAppNotFoundError("no running application matches app id/name " + appID)
		}
	}

	var out []core.WindowInfo
	for _, appRef := range targets {
		children, err := getChildren(ctx, conn, appRef)
		if err != nil {
			continue
		}
		for _, childRef := range children {
			n, err := fetchNode(ctx, conn, childRef)
			if err != nil {
				continue
			}
			ds := decodeState(n.state)
			out = append(out, core.WindowInfo{
				ID:      string(childRef.Path),
				AppID:   appRef.Bus,
				Title:   n.name,
				Bounds:  n.bounds,
				Focused: ds.Focused,
			})
		}
	}
	return out, nil
}

// Children implements core.Backend: the direct children of el, built from a
// single GetChildren call plus one batched fetch per child.
func (b *AtspiBackend) Children(ctx context.Context, el *core.Element) ([]*core.Element, error) {
	conn, err := b.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	ref, err := refFromElement(el)
	if err != nil {
		return nil, err
	}
	children, err := getChildren(ctx, conn, ref)
	if err != nil {
		return nil, core.NewActionFailedError("could not list children: " + err.Error())
	}
	out := make([]*core.Element, 0, len(children))
	for _, childRef := range children {
		n, err := fetchNode(ctx, conn, childRef)
		if err != nil {
			continue
		}
		out = append(out, n.toElement(b.Name(), childRef.Bus))
	}
	return out, nil
}

// Find implements core.Backend with the selector-driven, depth-capped,
// limit-capped, ctx-aware traversal in traverse.go — it never walks the
// whole tree.
func (b *AtspiBackend) Find(ctx context.Context, scope core.Scope, sel *core.Selector, limit int) ([]*core.Element, error) {
	conn, err := b.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultFindLimit
	}
	maxDepth := scope.Depth
	if maxDepth <= 0 {
		maxDepth = defaultFindDepth
	}

	roots, err := resolveScopeRoots(ctx, conn, scope)
	if err != nil {
		return nil, err
	}

	t := newTraverseState(conn, b.Name())
	var results []*core.Element
	for _, root := range roots {
		if len(results) >= limit {
			break
		}
		if err := findRec(ctx, t, sel, root, findState{ds: []int{0}}, 0, maxDepth, &results, limit); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// Properties implements core.Backend. When props is empty it returns the
// cheap attributes already gathered by a normal node fetch (raw role,
// interfaces, child count, decoded state extras); "text" and "actions" are
// only fetched on request since they cost an extra round trip each.
func (b *AtspiBackend) Properties(ctx context.Context, el *core.Element, props []string) (map[string]any, error) {
	conn, err := b.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	ref, err := refFromElement(el)
	if err != nil {
		return nil, err
	}
	n, err := fetchNode(ctx, conn, ref)
	if err != nil {
		return nil, core.NewActionFailedError("could not read properties: " + err.Error())
	}
	ds := decodeState(n.state)
	out := ds.nativeExtras()
	out["rawRole"] = n.roleName
	out["interfaces"] = n.interfaces
	out["childCount"] = n.childCount
	out["bounds"] = n.bounds

	want := func(name string) bool {
		if len(props) == 0 {
			return false
		}
		for _, p := range props {
			if strings.EqualFold(p, name) {
				return true
			}
		}
		return false
	}

	if len(props) == 0 || want("text") {
		if hasIface(n.interfaces, "Text") {
			if body, err := conn.call(ctx, ref.Bus, ref.Path, textIface, "GetText", int32(0), int32(-1)); err == nil && len(body) > 0 {
				if s, ok := body[0].(string); ok {
					const maxText = 20000
					if len(s) > maxText {
						s = s[:maxText]
					}
					out["text"] = s
				}
			}
		}
	}
	if want("actions") {
		if hasIface(n.interfaces, "Action") {
			if names, err := actionNames(ctx, conn, ref); err == nil {
				out["actionNames"] = names
			}
		}
	}
	return out, nil
}

// Perform implements core.Backend.
func (b *AtspiBackend) Perform(ctx context.Context, el *core.Element, action core.Action) error {
	conn, err := b.ensureConn(ctx)
	if err != nil {
		return err
	}
	ref, err := refFromElement(el)
	if err != nil {
		return err
	}
	return performAction(ctx, conn, ref, action)
}

// listAppRefs returns the accessibleRef of every application registered
// with the AT-SPI2 registry (the root's direct children).
func listAppRefs(ctx context.Context, conn busConn) ([]accessibleRef, error) {
	root := accessibleRef{Bus: registryDest, Path: registryPath}
	refs, err := getChildren(ctx, conn, root)
	if err != nil {
		return nil, core.NewActionFailedError("could not list AT-SPI applications: " + err.Error())
	}
	return refs, nil
}

// getChildren is a standalone (non-memoized) GetChildren call, used by the
// Backend methods that only need a single level and do not otherwise share
// a traverseState.
func getChildren(ctx context.Context, conn busConn, ref accessibleRef) ([]accessibleRef, error) {
	body, err := conn.call(ctx, ref.Bus, ref.Path, accessibleIface, "GetChildren")
	if err != nil {
		return nil, err
	}
	var refs []soRef
	if len(body) > 0 {
		if err := storeSoRefSlice(body[0], &refs); err != nil {
			return nil, err
		}
	}
	out := make([]accessibleRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.ref())
	}
	return out, nil
}

// resolveScopeRoots resolves the starting accessibleRef(s) for a Find call
// from scope: scope.Root when set, the named window when
// AppID+WindowID are set, the whole app subtree when only AppID is set, or
// every running application (a bounded, selector-pruned global search) when
// scope carries no app/window context at all.
func resolveScopeRoots(ctx context.Context, conn busConn, scope core.Scope) ([]accessibleRef, error) {
	if scope.Root != nil {
		ref, err := refFromElement(scope.Root)
		if err != nil {
			return nil, err
		}
		return []accessibleRef{ref}, nil
	}
	if scope.WindowID != "" {
		if scope.AppID == "" {
			return nil, core.NewInvalidArgsError("scope.WindowID requires scope.AppID (the AT-SPI backend addresses a window by application bus name + object path)")
		}
		return []accessibleRef{{Bus: scope.AppID, Path: pathOf(scope.WindowID)}}, nil
	}
	appRefs, err := listAppRefs(ctx, conn)
	if err != nil {
		return nil, err
	}
	if scope.AppID == "" {
		return appRefs, nil
	}
	var matched []accessibleRef
	for _, ref := range appRefs {
		if ref.Bus == scope.AppID {
			matched = append(matched, ref)
			continue
		}
		if n, err := fetchNode(ctx, conn, ref); err == nil && strings.EqualFold(n.name, scope.AppID) {
			matched = append(matched, ref)
		}
	}
	if len(matched) == 0 {
		return nil, core.NewAppNotFoundError("no running application matches app id/name " + scope.AppID)
	}
	return matched, nil
}
