package config

import (
	"path/filepath"
	"sort"
	"strings"
)

// ResolvedLSPServer is a fully-resolved LSP server candidate available for
// activation. It is produced by merging a built-in preset (LSPPresets) with the
// user's [LSP.*] configuration. Presets act as the default catalogue, so a
// language server can be auto-activated by file type even when the user has not
// configured it explicitly — provided its binary is found on PATH at spawn time.
type ResolvedLSPServer struct {
	// Name is the registry key (e.g. "gopls", "pyright").
	Name string
	// Command is the executable to run.
	Command string
	// Args are the arguments passed to Command.
	Args []string
	// Languages is the list of normalized file extensions this server handles
	// (lowercase, leading dot). Empty means it handles all files.
	Languages []string
	// Disabled excludes this server from activation.
	Disabled bool
	// Autostart eagerly starts the server at boot instead of on demand.
	Autostart bool
	// Source records provenance: "preset", "user", or "user+preset".
	Source string
}

// HandlesExt reports whether this server handles the given file extension.
// ext must include the leading dot and is matched case-insensitively. A server
// with no Languages handles every extension.
func (s ResolvedLSPServer) HandlesExt(ext string) bool {
	if len(s.Languages) == 0 {
		return true
	}
	ext = strings.ToLower(ext)
	for _, l := range s.Languages {
		if l == ext {
			return true
		}
	}
	return false
}

// normalizeLSPExts lowercases each extension and ensures a single leading dot,
// so "GO", "go" and ".go" all normalize to ".go".
func normalizeLSPExts(exts []string) []string {
	if len(exts) == 0 {
		return nil
	}
	out := make([]string, 0, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}

// resolveLSPServer merges a preset (which may be empty) with an optional user
// configuration. User-provided non-empty fields win; empty user fields inherit
// from the preset, so a user can enable a preset by only setting e.g. Autostart.
func resolveLSPServer(name string, preset LSPPreset, hasUser bool, uc LSPConfig) ResolvedLSPServer {
	hasPreset := preset.Name != ""

	rs := ResolvedLSPServer{
		Name:      name,
		Command:   preset.Config.Command,
		Args:      preset.Config.Args,
		Languages: normalizeLSPExts(preset.Config.Languages),
		Source:    "preset",
	}

	if hasUser {
		if uc.Command != "" {
			rs.Command = uc.Command
		}
		if len(uc.Args) > 0 {
			rs.Args = uc.Args
		}
		if len(uc.Languages) > 0 {
			rs.Languages = normalizeLSPExts(uc.Languages)
		}
		rs.Disabled = uc.Disabled
		rs.Autostart = uc.Autostart
		if hasPreset {
			rs.Source = "user+preset"
		} else {
			rs.Source = "user"
		}
	}

	return rs
}

// LSPRegistry returns the merged set of LSP servers available for activation.
// It starts from the built-in presets (the default catalogue) and overlays the
// user's [LSP.*] configuration: an entry with the same name as a preset
// overrides/extends it, while an entry with a new name is appended. The result
// is deterministically ordered: presets first (in preset order), then any
// user-only servers sorted by name.
func (c *Config) LSPRegistry() []ResolvedLSPServer {
	presets := LSPPresets()

	seen := make(map[string]bool, len(presets))
	out := make([]ResolvedLSPServer, 0, len(presets)+len(c.LSP))

	for _, p := range presets {
		uc, hasUser := c.LSP[p.Name]
		out = append(out, resolveLSPServer(p.Name, p, hasUser, uc))
		seen[p.Name] = true
	}

	// User-only servers with no matching preset.
	names := make([]string, 0, len(c.LSP))
	for name := range c.LSP {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, resolveLSPServer(name, LSPPreset{}, true, c.LSP[name]))
	}

	return out
}

// LSPServersForExt returns the enabled servers that handle the given file
// extension. ext should include the leading dot (".go"); matching is
// case-insensitive. Disabled servers and those without a command are skipped.
func (c *Config) LSPServersForExt(ext string) []ResolvedLSPServer {
	ext = strings.ToLower(ext)
	if ext == "" {
		return nil
	}
	var out []ResolvedLSPServer
	for _, s := range c.LSPRegistry() {
		if s.Disabled || s.Command == "" {
			continue
		}
		if s.HandlesExt(ext) {
			out = append(out, s)
		}
	}
	return out
}

// LSPServersForFile is a convenience wrapper around LSPServersForExt that
// derives the extension from a file path.
func (c *Config) LSPServersForFile(path string) []ResolvedLSPServer {
	return c.LSPServersForExt(filepath.Ext(path))
}

// LSPAutostartServers returns the servers that should be eagerly started at
// boot: those with Autostart=true that are enabled and have a command.
func (c *Config) LSPAutostartServers() []ResolvedLSPServer {
	var out []ResolvedLSPServer
	for _, s := range c.LSPRegistry() {
		if s.Disabled || s.Command == "" || !s.Autostart {
			continue
		}
		out = append(out, s)
	}
	return out
}
