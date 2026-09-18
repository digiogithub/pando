//go:build !linux && !darwin && !windows

package sandbox

import "runtime"

// platformWrapper returns the not-enforced fallback on every OS without a
// dedicated backend file (wrapper_linux.go, wrapper_darwin.go,
// wrapper_windows.go).
func platformWrapper() Wrapper {
	return newNoopWrapper(runtime.GOOS + ": not supported")
}
