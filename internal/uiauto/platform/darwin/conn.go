package darwin

import "context"

// axConn is the minimal Accessibility-API surface the platform-independent
// traversal/action/backend code in this package depends on, instead of
// calling AXUIElement*/CoreFoundation functions directly. This mirrors the
// busConn seam the Linux AT-SPI2 backend (Phase 2) uses for the same
// reason: it lets traverse_test.go/actions_test.go/backend_test.go run on
// any GOOS against a fake, in-memory element tree. The real implementation
// (realAXConn, ax_darwin.go, "//go:build darwin") talks to
// ApplicationServices/CoreFoundation through purego.
type axConn interface {
	// attributes fetches the given attribute names for ref in a single
	// batched round trip (AXUIElementCopyMultipleAttributeValues in the
	// real implementation). Attributes the element does not support, or
	// that have no value, are simply absent from the returned map — this
	// call only fails for a connection-level problem (stale element,
	// disabled API, ...).
	attributes(ctx context.Context, ref axRef, names []string) (map[string]any, error)

	// actionNames returns the AXUIElementCopyActionNames for ref.
	actionNames(ctx context.Context, ref axRef) ([]string, error)

	// performAction invokes AXUIElementPerformAction(ref, name).
	performAction(ctx context.Context, ref axRef, name string) error

	// setAttribute invokes AXUIElementSetAttributeValue(ref, attr, value).
	// value is one of string, bool, float64.
	setAttribute(ctx context.Context, ref axRef, attr string, value any) error

	// runningApps lists (pid, process name) for the processes this backend
	// can enumerate, independent of any AX call.
	runningApps(ctx context.Context) ([]appProc, error)

	// appElement returns the axRef for AXUIElementCreateApplication(pid).
	appElement(ctx context.Context, pid int32) (axRef, error)

	// trusted reports whether the current process is AX-trusted
	// (AXIsProcessTrusted).
	trusted(ctx context.Context) bool

	// close releases every resource (retained handles, loaded libraries)
	// this connection owns.
	close() error
}

// attrValue decodes a single named attribute from a batched attributes()
// result, returning (value, true) when present.
func attrValue(m map[string]any, name string) (any, bool) {
	v, ok := m[name]
	return v, ok
}

func attrString(m map[string]any, name string) string {
	if v, ok := attrValue(m, name); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func attrBool(m map[string]any, name string) bool {
	if v, ok := attrValue(m, name); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func attrRefs(m map[string]any, name string) []axRef {
	if v, ok := attrValue(m, name); ok {
		if refs, ok := v.([]axRef); ok {
			return refs
		}
	}
	return nil
}
