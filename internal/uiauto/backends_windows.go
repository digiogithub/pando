//go:build windows

package uiauto

import (
	"github.com/digiogithub/pando/internal/uiauto/core"
	windows "github.com/digiogithub/pando/internal/uiauto/platform/windows"
)

// init registers the Windows UI Automation backend ("uia") into the shared
// registry, per the extension point documented in backends.go — mirroring
// backends_linux.go ("atspi") and backends_darwin.go ("ax") so this is the
// only file in this package a platform backend needs to add; manager.go and
// the rest of backends.go stay untouched.
func init() {
	globalRegistry.Register("uia", func() (core.Backend, error) {
		return windows.NewBackend()
	})
}
