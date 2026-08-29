// Package uiauto is the Pando Desktop Controller: it wires the
// platform-independent internal/uiauto/core building blocks (Element,
// Selector, Locator, SnapshotStore, Backend, ActionResolver, ...) to a
// concrete accessibility backend and exposes a single Manager the
// desktop_* agent tools drive.
package uiauto

import "github.com/digiogithub/pando/internal/uiauto/core"

// globalRegistry is the process-wide backend registry. It always has the
// "null" backend registered so Registry().Resolve("auto") never fails, even
// on a platform/session with no accessibility backend compiled in or
// available.
//
// Extension point for later phases: a platform package (P2
// internal/uiauto/platform/linux, P4 .../windows, P5 .../darwin, P6
// .../browser) registers itself by calling Registry().Register(name, ...)
// from its own init() function. That package is then pulled in with a
// per-GOOS build-tagged blank import added to a NEW file in this directory
// (e.g. backends_linux.go with a "//go:build linux" tag), never by editing
// this file or manager.go.
var globalRegistry = core.NewRegistry()

func init() {
	globalRegistry.Register("null", func() (core.Backend, error) {
		return core.NewNullBackend(), nil
	})
	// "auto" prefers real platform backends, in the order later phases will
	// register them, and falls back to "null". Backend names not yet
	// registered are simply skipped by Registry.Resolve.
	//
	// "cdp" is deliberately NOT in this order (Block R fix, 2026-08-30):
	// Registry.Resolve("auto") returns the FIRST backend in this list that
	// constructs successfully, and every OS accessibility backend
	// constructs successfully whenever its bus/API is merely reachable
	// (regardless of whether any app is actually exposing anything through
	// it) -- so with "cdp" in this list, on any Linux box with a live
	// a11y bus "atspi" always won and "cdp" was never even tried, making
	// the CDP backend permanently unreachable under "auto". Manager now
	// resolves "cdp" as a second, independent backend (see
	// resolveBackends in manager.go) and routes per-operation between it
	// and whichever backend this auto-order picks, instead of relying on
	// a single global winner.
	globalRegistry.SetAutoOrder("atspi", "uia", "ax", "null")
}

// Registry returns the process-wide backend Registry, so platform packages
// and tests can register or resolve backends without reaching into an
// unexported package variable.
func Registry() *core.Registry { return globalRegistry }
