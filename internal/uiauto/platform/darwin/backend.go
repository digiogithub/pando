package darwin

import (
	"context"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// DarwinBackend implements core.Backend against the macOS Accessibility
// API via the axConn seam (conn.go). The real, purego-backed axConn is
// constructed only by NewBackend (ax_darwin.go, "//go:build darwin");
// newBackendWithConn lets tests substitute a fake for every other file in
// this package.
type DarwinBackend struct {
	conn axConn
}

func newBackendWithConn(conn axConn) *DarwinBackend {
	return &DarwinBackend{conn: conn}
}

// Name implements core.Backend.
func (b *DarwinBackend) Name() string { return "ax" }

// Available implements core.Backend. Per the plan this checks
// AXIsProcessTrusted and returns a PERM_DENIED error (not just an
// all-false Capabilities) when untrusted, so the Manager/tool layer
// surfaces the actionable suggestion directly instead of silently
// degrading to PLATFORM_NOT_SUPPORTED on the first real call.
func (b *DarwinBackend) Available(ctx context.Context) (core.Capabilities, error) {
	if !b.conn.trusted(ctx) {
		return core.Capabilities{}, permDeniedTrustError()
	}
	return core.Capabilities{
		Accessibility: true,
		UIInspection:  true,
		UIActions:     true,
		// Mouse/Keyboard/Screenshot are Phase 3's input/screen layer; the
		// Manager OR-merges them in on top of whatever this backend
		// reports.
	}, nil
}

// permDeniedTrustError is the single place the "grant Accessibility
// permission" suggestion is authored, so Available and every call site
// that discovers untrust mid-operation say the same thing.
func permDeniedTrustError() *core.DesktopError {
	err := core.NewPermDeniedError("Pando is not trusted for Accessibility automation (AXIsProcessTrusted returned false)")
	err.Suggestion = "Open System Settings > Privacy & Security > Accessibility and add/enable the Pando binary, then retry."
	return err
}

// Close implements core.Backend.
func (b *DarwinBackend) Close() error {
	return b.conn.close()
}

// Apps implements core.Backend by enumerating running processes and
// building an AXUIElementRef application object for each.
func (b *DarwinBackend) Apps(ctx context.Context) ([]core.AppInfo, error) {
	procs, err := b.conn.runningApps(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.AppInfo, 0, len(procs))
	for _, p := range procs {
		appRef, err := b.conn.appElement(ctx, p.PID)
		if err != nil {
			continue // process vanished/inaccessible mid-listing
		}
		raw, err := b.conn.attributes(ctx, appRef, []string{"AXTitle", "AXWindows"})
		if err != nil {
			out = append(out, core.AppInfo{ID: strconv.Itoa(int(p.PID)), Name: p.Name, PID: int(p.PID)})
			continue
		}
		name := attrString(raw, "AXTitle")
		if name == "" {
			name = p.Name
		}
		out = append(out, core.AppInfo{
			ID:      strconv.Itoa(int(p.PID)),
			Name:    name,
			PID:     int(p.PID),
			Windows: len(attrRefs(raw, "AXWindows")),
		})
	}
	return out, nil
}

// resolveApps returns the appRef(s)/pid(s) matching appID: a decimal pid,
// or (falling back) a case-insensitive process/title name match.
func (b *DarwinBackend) resolveApps(ctx context.Context, appID string) ([]appProc, error) {
	procs, err := b.conn.runningApps(ctx)
	if err != nil {
		return nil, err
	}
	if appID == "" {
		return procs, nil
	}
	if pid, err := strconv.Atoi(appID); err == nil {
		for _, p := range procs {
			if int(p.PID) == pid {
				return []appProc{p}, nil
			}
		}
	}
	var matched []appProc
	for _, p := range procs {
		if strings.EqualFold(p.Name, appID) {
			matched = append(matched, p)
			continue
		}
		if appRef, err := b.conn.appElement(ctx, p.PID); err == nil {
			if raw, err := b.conn.attributes(ctx, appRef, []string{"AXTitle"}); err == nil {
				if strings.EqualFold(attrString(raw, "AXTitle"), appID) {
					matched = append(matched, p)
				}
			}
		}
	}
	if len(matched) == 0 {
		return nil, core.NewAppNotFoundError("no running application matches app id/name " + appID)
	}
	return matched, nil
}

// Windows implements core.Backend by reading AXWindows off the matching
// application(s).
func (b *DarwinBackend) Windows(ctx context.Context, appID string) ([]core.WindowInfo, error) {
	procs, err := b.resolveApps(ctx, appID)
	if err != nil {
		return nil, err
	}
	var out []core.WindowInfo
	for _, p := range procs {
		appRef, err := b.conn.appElement(ctx, p.PID)
		if err != nil {
			continue
		}
		raw, err := b.conn.attributes(ctx, appRef, []string{"AXWindows"})
		if err != nil {
			continue
		}
		for _, winRef := range attrRefs(raw, "AXWindows") {
			wraw, err := b.conn.attributes(ctx, winRef, []string{"AXTitle", "AXPosition", "AXSize", "AXMain"})
			if err != nil {
				continue
			}
			var pos Point
			var size Size
			if v, ok := attrValue(wraw, "AXPosition"); ok {
				pos, _ = v.(Point)
			}
			if v, ok := attrValue(wraw, "AXSize"); ok {
				size, _ = v.(Size)
			}
			out = append(out, core.WindowInfo{
				ID:      encodeWindowID(winRef),
				AppID:   strconv.Itoa(int(p.PID)),
				Title:   attrString(wraw, "AXTitle"),
				Bounds:  core.Bounds{X: int(pos.X), Y: int(pos.Y), W: int(size.W), H: int(size.H)},
				Focused: attrBool(wraw, "AXMain"),
			})
		}
	}
	return out, nil
}

// encodeWindowID / decodeWindowID represent an axRef as the plain string
// core.WindowInfo.ID/Scope.WindowID require.
func encodeWindowID(ref axRef) string {
	return strconv.Itoa(int(ref.PID)) + ":" + strconv.FormatUint(uint64(ref.Handle), 16)
}

func decodeWindowID(id string) (axRef, error) {
	parts := strings.SplitN(id, ":", 2)
	if len(parts) != 2 {
		return axRef{}, core.NewInvalidArgsError("malformed ax window id " + id)
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil {
		return axRef{}, core.NewInvalidArgsError("malformed ax window id " + id)
	}
	handle, err := strconv.ParseUint(parts[1], 16, 64)
	if err != nil {
		return axRef{}, core.NewInvalidArgsError("malformed ax window id " + id)
	}
	return axRef{PID: int32(pid), Handle: uintptr(handle)}, nil
}

// resolveScopeRoots resolves the starting axRef(s) for a Find call:
// scope.Root when set, the named window when AppID+WindowID are set, the
// application element when only AppID is set, or every running
// application (a bounded, selector-pruned global search) when scope
// carries no app/window context at all.
func (b *DarwinBackend) resolveScopeRoots(ctx context.Context, scope core.Scope) ([]axRef, error) {
	if scope.Root != nil {
		ref, err := refFromElement(scope.Root)
		if err != nil {
			return nil, err
		}
		return []axRef{ref}, nil
	}
	if scope.WindowID != "" {
		ref, err := decodeWindowID(scope.WindowID)
		if err != nil {
			return nil, err
		}
		return []axRef{ref}, nil
	}
	procs, err := b.resolveApps(ctx, scope.AppID)
	if err != nil {
		return nil, err
	}
	roots := make([]axRef, 0, len(procs))
	for _, p := range procs {
		ref, err := b.conn.appElement(ctx, p.PID)
		if err != nil {
			continue
		}
		roots = append(roots, ref)
	}
	return roots, nil
}

// Find implements core.Backend with the selector-driven, depth-capped,
// limit-capped, ctx-aware traversal in traverse.go — it never walks the
// whole tree.
func (b *DarwinBackend) Find(ctx context.Context, scope core.Scope, sel *core.Selector, limit int) ([]*core.Element, error) {
	if limit <= 0 {
		limit = defaultFindLimit
	}
	maxDepth := scope.Depth
	if maxDepth <= 0 {
		maxDepth = defaultFindDepth
	}
	roots, err := b.resolveScopeRoots(ctx, scope)
	if err != nil {
		return nil, err
	}
	t := newTraverseState(b.conn, b.Name())
	var results []*core.Element
	for _, root := range roots {
		if len(results) >= limit {
			break
		}
		if err := findRec(ctx, t, sel, root, findState{ds: []int{0}}, nil, 0, maxDepth, &results, limit); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// Children implements core.Backend: the direct AXChildren of el.
func (b *DarwinBackend) Children(ctx context.Context, el *core.Element) ([]*core.Element, error) {
	ref, err := refFromElement(el)
	if err != nil {
		return nil, err
	}
	raw, err := b.conn.attributes(ctx, ref, fixedAttrNames)
	if err != nil {
		return nil, err
	}
	n := parseFetchedNode(ref, raw)
	out := make([]*core.Element, 0, len(n.children))
	for i, childRef := range n.children {
		craw, err := b.conn.attributes(ctx, childRef, fixedAttrNames)
		if err != nil {
			continue
		}
		cn := parseFetchedNode(childRef, craw)
		out = append(out, cn.toElement(b.Name(), childRef.PID, []int{i}))
	}
	return out, nil
}

// Properties implements core.Backend. When props is empty it returns the
// cheap extras already gathered by a normal node fetch (help/selected/
// main/subrole); "actions" is only fetched on request since it costs an
// extra round trip.
func (b *DarwinBackend) Properties(ctx context.Context, el *core.Element, props []string) (map[string]any, error) {
	ref, err := refFromElement(el)
	if err != nil {
		return nil, err
	}
	raw, err := b.conn.attributes(ctx, ref, fixedAttrNames)
	if err != nil {
		return nil, err
	}
	n := parseFetchedNode(ref, raw)
	out := map[string]any{
		"rawRole": n.role,
		"subrole": n.subrole,
	}
	if n.help != "" {
		out["help"] = n.help
	}
	if n.selected {
		out["selected"] = true
	}
	if n.main {
		out["main"] = true
	}

	want := func(name string) bool {
		for _, p := range props {
			if strings.EqualFold(p, name) {
				return true
			}
		}
		return false
	}
	if len(props) == 0 || want("actions") {
		if names, err := b.conn.actionNames(ctx, ref); err == nil {
			out["actionNames"] = names
		}
	}
	return out, nil
}

// Perform implements core.Backend.
func (b *DarwinBackend) Perform(ctx context.Context, el *core.Element, action core.Action) error {
	ref, err := refFromElement(el)
	if err != nil {
		return err
	}
	return performAction(ctx, b.conn, ref, action)
}
