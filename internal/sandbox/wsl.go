package sandbox

import (
	"os"
	"strings"
)

// wslInteropPath is the file the Windows kernel driver exposes under WSL.
// Its mere existence is the most reliable signal, and cheaper to check than
// parsing /proc/version.
const wslInteropPath = "/proc/sys/fs/binfmt_misc/WSLInterop"

// IsWSL reports whether this process is running under Windows Subsystem for
// Linux. It is informational only: WSL runs a real (WSL2) or translated
// (WSL1) Linux kernel, so the Linux backend (PANDO-US-0041) applies the same
// Landlock+seccomp policy there unchanged — IsWSL exists purely so status
// labels and logs can say "Linux (WSL)" instead of a bare "Linux".
//
// It never errors: on any OS other than Linux, and on Linux outside WSL, a
// missing /proc entry simply means "not WSL" and IsWSL returns false.
func IsWSL() bool {
	if _, err := os.Stat(wslInteropPath); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return isWSLProcVersion(string(data))
}

// isWSLProcVersion is the pure parsing half of IsWSL, split out so it is
// unit-testable without a real /proc/version. WSL's kernel build string
// names Microsoft, e.g. "Linux version 5.15.90.1-microsoft-standard-WSL2
// ..." (WSL2) or "...-Microsoft (Microsoft@Microsoft.com) ..." (WSL1); a
// stock distro kernel never does.
func isWSLProcVersion(procVersion string) bool {
	return strings.Contains(strings.ToLower(procVersion), "microsoft")
}
