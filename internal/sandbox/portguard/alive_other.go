//go:build !unix

package portguard

import "os"

// processAlive reports whether pid names a running process. On Windows
// FindProcess opens a handle and fails when the process does not exist.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
