//go:build darwin

package uiauto

import (
	"github.com/digiogithub/pando/internal/uiauto/core"
	darwin "github.com/digiogithub/pando/internal/uiauto/platform/darwin"
)

// init registers the macOS AXUIElement backend ("ax") into the shared
// registry, per the extension point documented in backends.go — mirroring
// backends_linux.go's registration of "atspi" so this is the only file in
// this package a platform backend needs to add; manager.go and the rest of
// backends.go stay untouched.
func init() {
	globalRegistry.Register("ax", func() (core.Backend, error) {
		return darwin.NewBackend()
	})
}
