package cliassist

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SysInfo holds the detected operating system and shell information.
type SysInfo struct {
	OS        string // "linux", "macos", "windows"
	ShellPath string // e.g. "/bin/bash", "C:\Windows\System32\cmd.exe"
	ShellName string // e.g. "bash", "zsh", "fish", "powershell", "cmd"
}

// DetectSysInfo detects the current OS and user shell at runtime.
func DetectSysInfo() SysInfo {
	info := SysInfo{}

	switch runtime.GOOS {
	case "darwin":
		info.OS = "macos"
	case "windows":
		info.OS = "windows"
	default:
		info.OS = "linux"
	}

	if runtime.GOOS == "windows" {
		// Prefer PowerShell if available
		if ps, ok := os.LookupEnv("PSModulePath"); ok && ps != "" {
			info.ShellPath = "powershell.exe"
			info.ShellName = "powershell"
		} else {
			comspec := os.Getenv("COMSPEC")
			if comspec == "" {
				comspec = "cmd.exe"
			}
			info.ShellPath = comspec
			info.ShellName = "cmd"
		}
	} else {
		shellPath := os.Getenv("SHELL")
		if shellPath == "" {
			shellPath = "/bin/sh"
		}
		info.ShellPath = shellPath
		// Handle login shells like "-bash" by stripping leading "-"
		info.ShellName = strings.TrimPrefix(filepath.Base(shellPath), "-")
	}

	return info
}
