// Package darwin implements the macOS AXUIElement core.Backend ("ax") for
// the Pando Desktop Controller, described in Phase 5 of
// pando/plans/desktop_controller_uiauto_plan.md.
//
// No cgo is used anywhere in this package. The real Accessibility/
// CoreFoundation bridging (dlopen + purego.RegisterLibFunc against
// ApplicationServices.framework and CoreFoundation.framework) lives in
// ax_darwin.go, built only for GOOS=darwin. Every other file in this
// package contains platform-INDEPENDENT logic — selector-driven traversal,
// AXError -> core.DesktopError mapping, Element construction from a
// decoded attribute map, action-kind dispatch — written against the small
// axConn interface (conn.go) instead of calling AX functions directly, so
// it compiles and its unit tests run on any GOOS (in particular, on this
// Linux development machine) against a fake axConn.
//
// Element identity: an AXUIElementRef is a live CoreFoundation object.
// axRef{PID, Handle} identifies one within the current process; Handle is
// only meaningful for the lifetime of the *DarwinBackend that produced it
// (its handle table CFRetains every ref it hands out and CFReleases them
// all in Close()). Because a raw pointer cannot safely be reused across
// snapshots or after Close(), every Element this backend builds also
// stashes the durable (pid, AXIdentifier, role, index-path) tuple described
// by the plan in Element.Native.Data — see element.go's nativeIndexPathKey
// et al. — as the re-resolution key a future Find/Observe can use to
// recover an equivalent node even if the live handle went stale.
//
// CoreFoundation memory-management discipline: every CFRelease-able object
// this package creates or copies (CFStringRef, CFArrayRef, AXUIElementRef,
// CFNumberRef is read in place and never retained) is released exactly
// once, either immediately after use (attribute value decode) or is
// retained into the backend's handle table and released in Close(). The
// fixed, small vocabulary of attribute-name and action-name CFStrings is
// interned once (created, never released) since the process lives for the
// lifetime of the backend; see ax_darwin.go's cfStringIntern.
package darwin
