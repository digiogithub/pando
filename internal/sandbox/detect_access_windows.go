//go:build windows

package sandbox

// defaultHostAccess cannot tell ACL-based access cheaply on Windows, where
// the sandbox is not enforced anyway: the answer is always unknown.
func defaultHostAccess(string, bool) (can, known bool) {
	return false, false
}
