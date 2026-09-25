package config

import (
	"path/filepath"
	"strings"
)

// xcodeMCPBridgeBinary is the executable Xcode ships to expose its tools over
// MCP. It is run directly (/Applications/Xcode.app/.../usr/bin/mcpbridge, as
// Xcode's own ACP client passes it) or through `xcrun mcpbridge`.
const xcodeMCPBridgeBinary = "mcpbridge"

// xcrunValueFlags are the xcrun options that consume the next argument as
// their own value (e.g. "--sdk macosx"), as opposed to a standalone flag
// (e.g. "--verbose"). Without tracking these, a value like "macosx" from
// `xcrun --sdk macosx mcpbridge` would be mistaken for the tool name xcrun
// runs, and the loop below would stop one argument too early.
var xcrunValueFlags = map[string]bool{
	"--sdk":       true,
	"-sdk":        true,
	"--toolchain": true,
	"--language":  true,
}

// IsXcodeMCPBridge reports whether the server is Xcode's mcpbridge. Xcode asks
// the user to allow every agent that connects through it and can only remember
// the answer for an agent whose path and code signature it can resolve.
func (s MCPServer) IsXcodeMCPBridge() bool {
	if s.Type != "" && s.Type != MCPStdio {
		return false
	}
	command := strings.TrimSpace(s.Command)
	if command == "" {
		return false
	}
	base := filepath.Base(command)
	if base == xcodeMCPBridgeBinary {
		return true
	}
	if base == "xcrun" {
		skipNext := false
		for _, arg := range s.Args {
			arg = strings.TrimSpace(arg)
			if skipNext {
				skipNext = false
				continue
			}
			if arg == "" {
				continue
			}
			if strings.HasPrefix(arg, "-") {
				if xcrunValueFlags[arg] {
					skipNext = true
				}
				continue
			}
			return filepath.Base(arg) == xcodeMCPBridgeBinary
		}
	}
	return false
}

// SandboxExempt reports whether a stdio MCP server must run outside the host
// sandbox regardless of the global policy: explicitly opted out (NoSandbox) or
// Xcode's mcpbridge, whose permission prompt breaks when the sandbox hides the
// parent process.
func (s MCPServer) SandboxExempt() bool {
	return s.NoSandbox || s.IsXcodeMCPBridge()
}
