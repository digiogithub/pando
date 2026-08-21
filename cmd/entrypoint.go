package cmd

import (
	"github.com/digiogithub/pando/internal/logging"
)

// Main is the whole program entry point: it installs the top-level panic
// recovery and runs the root command.
//
// The repository's own main.go is one call to this function, and so is the
// main.go that `xpando build` generates for a composed binary. That is the
// reason it exists: internal/logging is unreachable from another module, so a
// generated main package could only call Execute and would silently lose the
// panic handler. Anything that must happen for *every* Pando binary belongs
// here, not in main.go.
func Main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	Execute()
}
