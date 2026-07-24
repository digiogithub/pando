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
	// Description is the preset's one-line description, empty for user-only
	// servers.
	Description string
	// OptIn reports that the preset only activates once the user declares it
	// under [LSP.<name>]. Surfaces use it to tell "off until you ask for it"
	// apart from "disabled by the user".
	OptIn bool
	// Command is the executable to run.
	Command string
	// Args are the arguments passed to Command.
	Args []string
	// Languages is the list of normalized file extensions this server handles
	// (lowercase, leading dot). Empty (with Filenames also empty) means it
	// handles all files.
	Languages []string
	// Filenames are base names this server claims regardless of extension
	// (e.g. "Dockerfile"), for languages whose files carry no useful suffix.
	Filenames []string
	// Disabled excludes this server from activation.
	Disabled bool
	// Autostart eagerly starts the server at boot instead of on demand.
	Autostart bool
	// Source records provenance: "preset", "user", or "user+preset".
	Source string
	// Install describes how to provision Command when it is not on PATH. It is
	// empty for user-defined commands, which Pando never tries to install.
	Install LSPInstall
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

// HandlesFile reports whether this server claims the file at path, by base name
// first and by extension otherwise.
func (s ResolvedLSPServer) HandlesFile(path string) bool {
	return LSPHandlesFile(s.Languages, s.Filenames, path)
}

// LSPHandlesFile reports whether a server declaring the given file extensions
// and base names claims the file at path. It is shared by the registry and by
// the running LSP clients so both answer identically.
//
// A server declaring neither extensions nor names is a catch-all and claims
// every file. Otherwise a file is claimed when its base name matches one of
// filenames, or when its extension matches one of languages; a file without an
// extension is claimed by name only.
func LSPHandlesFile(languages, filenames []string, path string) bool {
	if len(languages) == 0 && len(filenames) == 0 {
		return true
	}
	if matchesLSPFilename(filenames, filepath.Base(path)) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" || len(languages) == 0 {
		return false
	}
	for _, l := range languages {
		if strings.ToLower(l) == ext {
			return true
		}
	}
	return false
}

// matchesLSPFilename matches a base name case-insensitively, either in full or
// up to its first dot, so a "Dockerfile" entry also claims "Dockerfile.dev".
func matchesLSPFilename(filenames []string, base string) bool {
	if len(filenames) == 0 || base == "" {
		return false
	}
	base = strings.ToLower(base)
	stem := base
	if i := strings.Index(stem, "."); i > 0 {
		stem = stem[:i]
	}
	for _, name := range filenames {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if name == base || name == stem {
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
		Name:        name,
		Description: preset.Description,
		OptIn:       preset.OptIn,
		Command:     preset.Config.Command,
		Args:        preset.Config.Args,
		Languages:   normalizeLSPExts(preset.Config.Languages),
		Filenames:   preset.Config.Filenames,
		Source:      "preset",
		Install:     preset.Install,
		// An opt-in preset stays out of automatic activation until the user
		// declares it under [LSP.<name>].
		Disabled: preset.OptIn && !hasUser,
	}

	if hasUser {
		if uc.Command != "" {
			rs.Command = uc.Command
			if uc.Command != preset.Config.Command {
				// The user pointed at their own binary; the preset's install
				// recipe would provision a different executable.
				rs.Install = LSPInstall{}
			}
		}
		if len(uc.Args) > 0 {
			rs.Args = uc.Args
		}
		if len(uc.Languages) > 0 {
			rs.Languages = normalizeLSPExts(uc.Languages)
		}
		if len(uc.Filenames) > 0 {
			rs.Filenames = uc.Filenames
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

// LSPServersForFile returns the enabled servers that handle the given file,
// matching by base name (e.g. "Dockerfile") as well as by extension.
func (c *Config) LSPServersForFile(path string) []ResolvedLSPServer {
	if path == "" {
		return nil
	}
	var out []ResolvedLSPServer
	for _, s := range c.LSPRegistry() {
		if s.Disabled || s.Command == "" {
			continue
		}
		if s.HandlesFile(path) {
			out = append(out, s)
		}
	}
	return out
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
