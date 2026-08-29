//go:build linux

package uiauto

import (
	"github.com/digiogithub/pando/internal/uiauto/core"
	linux "github.com/digiogithub/pando/internal/uiauto/platform/linux"
)

// init registers the Linux AT-SPI2 backend ("atspi") into the shared
// registry, per the extension point documented in backends.go. This is the
// only file in this package a platform backend needs to add; manager.go and
// the rest of backends.go stay untouched.
func init() {
	globalRegistry.Register("atspi", func() (core.Backend, error) {
		return linux.NewBackend()
	})
}
