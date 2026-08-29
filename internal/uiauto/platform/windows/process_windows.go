//go:build windows

package windows

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// processName resolves a process id to its executable's base name (e.g.
// "notepad.exe") via OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION) +
// QueryFullProcessImageName, both from golang.org/x/sys/windows (no cgo).
// It returns "" (never an error) when the process cannot be opened —
// commonly because it runs elevated/as another user and Pando does not —
// so a name lookup failure degrades AppInfo.Name to empty rather than
// failing the whole Apps()/Windows() call.
func processName(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	full := windows.UTF16ToString(buf[:size])
	return filepath.Base(full)
}
