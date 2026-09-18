//go:build !linux

package helper

import (
	"fmt"
	"os"
	"runtime"
)

// run is only implemented on Linux; elsewhere nothing wraps commands with the
// helper, so reaching it means a foreign caller.
func run(Spec, []string) int {
	fmt.Fprintf(os.Stderr, "pando sandbox: the %s helper is only available on Linux (running on %s)\n", Arg, runtime.GOOS)
	return ExitSetupFailed
}
