//go:build !windows

package sandbox

import "syscall"

// access(2) mode bits (unistd.h).
const (
	accessRead  = 0x4
	accessWrite = 0x2
)

// defaultHostAccess checks the Pando process's own access with access(2): the
// path itself when it exists, otherwise its nearest existing ancestor (for a
// write, whether the entry could be created there).
func defaultHostAccess(path string, write bool) (can, known bool) {
	target, ok := nearestExisting(path)
	if !ok {
		return false, false
	}
	mode := uint32(accessRead)
	if write {
		mode = accessWrite
	}
	return syscall.Access(target, mode) == nil, true
}
