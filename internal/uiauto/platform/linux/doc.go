// Package linux implements the Linux AT-SPI2 accessibility backend for the
// Pando Desktop Controller (internal/uiauto). It talks to the AT-SPI2
// registry and applications over D-Bus using github.com/godbus/dbus/v5 —
// no cgo, no external process.
//
// Design notes:
//
//   - Every accessibility object is identified by a (bus name, object path)
//     pair (accessibleRef). core.Element stores both in Native.Data so a
//     later Children/Perform call can act on it without re-searching.
//
//   - Traversal is selector-driven (see traverse.go): Find never walks the
//     whole tree. It carries a small per-DFS-branch set of "pending selector
//     steps" down the tree, testing only what could still complete a match,
//     and stops as soon as it has `limit` results, a depth cap is hit, or
//     ctx is cancelled.
//
//   - Property reads are batched with org.freedesktop.DBus.Properties.GetAll
//     instead of one round-trip per attribute, and a per-call memo cache
//     (traverseCache) makes sure a single Find/Children/Observe call never
//     re-fetches the same object twice.
//
// This package purposefully depends only on a small busConn interface
// (conn.go) rather than *dbus.Conn directly, so the traversal and matching
// logic can be exercised in unit tests against a fake in-memory tree with no
// real accessibility bus involved.
package linux
