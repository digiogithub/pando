package uiauto

import (
	"github.com/digiogithub/pando/internal/uiauto/core"
	browser "github.com/digiogithub/pando/internal/uiauto/platform/browser"
)

// init registers the CDP browser backend ("cdp") into the shared registry,
// per the extension point documented in backends.go. Unlike the OS
// accessibility backends, CDP is cross-platform, so this file carries no
// //go:build tag. This is the only file in this package a platform backend
// needs to add; manager.go and the rest of backends.go stay untouched.
func init() {
	globalRegistry.Register("cdp", func() (core.Backend, error) {
		return browser.NewBackend()
	})
}
