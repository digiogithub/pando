package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/logging"
)

// SandboxEnvVar is the environment variable that overrides the sandbox mode
// for one process: off | workspace-write | read-only | strict. It is applied
// by sandbox.Resolve (below an enterprise lock, above every config file).
const SandboxEnvVar = "PANDO_SANDBOX"

// sandboxEnvVarHiddenFromViper is what viper's env key replacer turns
// SandboxEnvVar into, so AutomaticEnv never finds it (see configureViper).
const sandboxEnvVarHiddenFromViper = "PANDO_SANDBOX_NOT_A_VIPER_KEY"

// Accepted sandbox values. internal/sandbox mirrors these as typed constants;
// they are duplicated here because that package imports this one.
const (
	SandboxModeWorkspaceWrite = "workspace-write"
	SandboxModeReadOnly       = "read-only"
	SandboxModeStrict         = "strict"
	SandboxModeOff            = "off"

	SandboxNetworkAllowed    = "allowed"
	SandboxNetworkRestricted = "restricted"

	SandboxBwrapAuto   = "auto"
	SandboxBwrapAlways = "always"
	SandboxBwrapNever  = "never"

	SandboxEnvInheritAll  = "all"
	SandboxEnvInheritCore = "core"
	SandboxEnvInheritNone = "none"

	SandboxExtendACPTerminals = "acp-terminals"
	SandboxExtendSkills       = "skills"
	SandboxExtendMCP          = "mcp"
	SandboxExtendSubagents    = "subagents"
)

// sandboxModeRank orders modes from loosest to tightest for the
// project-only-tightens rule. read-only and strict share a rank because
// neither contains the other (strict writes the workspace, read-only reads
// everything), so a project cannot swap one for the other.
var sandboxModeRank = map[string]int{
	SandboxModeOff:            0,
	SandboxModeWorkspaceWrite: 1,
	SandboxModeReadOnly:       2,
	SandboxModeStrict:         2,
}

var sandboxBwrapRank = map[string]int{
	SandboxBwrapNever:  0,
	SandboxBwrapAuto:   1,
	SandboxBwrapAlways: 2,
}

var sandboxInheritRank = map[string]int{
	SandboxEnvInheritAll:  0,
	SandboxEnvInheritCore: 1,
	SandboxEnvInheritNone: 2,
}

var sandboxExtendTargets = map[string]bool{
	SandboxExtendACPTerminals: true,
	SandboxExtendSkills:       true,
	SandboxExtendMCP:          true,
	SandboxExtendSubagents:    true,
}

// NormalizeSandboxConfig trims and lower-cases the enum fields, validates
// them, and trims/deduplicates the lists. Empty strings stay empty (they mean
// "default").
func NormalizeSandboxConfig(sc SandboxConfig) (SandboxConfig, error) {
	sc.Mode = normalizeSandboxEnum(sc.Mode)
	if _, ok := sandboxModeRank[sc.Mode]; sc.Mode != "" && !ok {
		return SandboxConfig{}, fmt.Errorf("sandbox mode must be one of workspace-write, read-only, strict, off")
	}
	sc.Network = normalizeSandboxEnum(sc.Network)
	switch sc.Network {
	case "", SandboxNetworkAllowed, SandboxNetworkRestricted:
	default:
		return SandboxConfig{}, fmt.Errorf("sandbox network must be one of allowed, restricted")
	}
	sc.UseBwrap = normalizeSandboxEnum(sc.UseBwrap)
	if _, ok := sandboxBwrapRank[sc.UseBwrap]; sc.UseBwrap != "" && !ok {
		return SandboxConfig{}, fmt.Errorf("sandbox useBwrap must be one of auto, always, never")
	}
	sc.Env.Inherit = normalizeSandboxEnum(sc.Env.Inherit)
	if _, ok := sandboxInheritRank[sc.Env.Inherit]; sc.Env.Inherit != "" && !ok {
		return SandboxConfig{}, fmt.Errorf("sandbox env inherit must be one of all, core, none")
	}

	extendTo := make([]string, 0, len(sc.ExtendTo))
	for _, target := range sc.ExtendTo {
		target = normalizeSandboxEnum(target)
		if target == "" {
			continue
		}
		if !sandboxExtendTargets[target] {
			return SandboxConfig{}, fmt.Errorf("sandbox extendTo entry %q must be one of acp-terminals, skills, mcp, subagents", target)
		}
		extendTo = append(extendTo, target)
	}
	sc.ExtendTo = normalizeSandboxList(extendTo)

	sc.WritableRoots = normalizeSandboxList(sc.WritableRoots)
	sc.ReadOnlyRoots = normalizeSandboxList(sc.ReadOnlyRoots)
	sc.DenyPaths = normalizeSandboxList(sc.DenyPaths)
	sc.Env.Exclude = normalizeSandboxList(sc.Env.Exclude)
	sc.Env.Keep = normalizeSandboxList(sc.Env.Keep)
	return sc, nil
}

func normalizeSandboxEnum(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

// effectiveSandboxMode is the mode a section asks for, with Disabled folded
// in and an unknown or empty value read as the default (the safe side: on).
func effectiveSandboxMode(sc SandboxConfig) string {
	if sc.Disabled {
		return SandboxModeOff
	}
	mode := normalizeSandboxEnum(sc.Mode)
	if _, ok := sandboxModeRank[mode]; !ok {
		return SandboxModeWorkspaceWrite
	}
	return mode
}

// readProjectSandboxConfig reads the sandbox section of the project-local
// config file for workingDir, on its own, without the global file or the
// environment. It returns the zero value when there is no such file or the
// section is absent or malformed (a malformed project section loosens
// nothing, so ignoring it is safe).
func readProjectSandboxConfig(workingDir string) SandboxConfig {
	configFile := FindLocalConfigFile(workingDir)
	if configFile == "" {
		return SandboxConfig{}
	}
	local := viper.New()
	local.SetConfigFile(configFile)
	if err := local.ReadInConfig(); err != nil {
		return SandboxConfig{}
	}
	var sc SandboxConfig
	if err := local.UnmarshalKey("sandbox", &sc); err != nil {
		logging.Warn("Ignoring malformed project sandbox config", "path", configFile, "error", err)
		return SandboxConfig{}
	}
	return sc
}

// applySandboxProjectRule computes the effective sandbox section after Load
// merged global, project and overlay values into merged. Every field an
// overlay set is authoritative and kept from merged (an enterprise policy
// outranks both files); every other field is recomputed from the global and
// project sections with tightenSandboxConfig, so a project value that would
// loosen the global one is dropped.
func applySandboxProjectRule(global, project, merged SandboxConfig, workspace string) SandboxConfig {
	out := tightenSandboxConfig(global, project, workspace)

	changed := OverlayChangedKeys()
	touched := func(field string) bool {
		path := "sandbox." + field
		for _, key := range changed {
			if pathsOverlap(key, path) {
				return true
			}
		}
		return false
	}

	if touched("disabled") {
		out.Disabled = merged.Disabled
	}
	if touched("mode") {
		out.Mode = merged.Mode
	}
	if touched("network") {
		out.Network = merged.Network
	}
	if touched("autoAllowBashDisabled") {
		out.AutoAllowBashDisabled = merged.AutoAllowBashDisabled
	}
	if touched("writableRoots") {
		out.WritableRoots = merged.WritableRoots
	}
	if touched("readOnlyRoots") {
		out.ReadOnlyRoots = merged.ReadOnlyRoots
	}
	if touched("denyPaths") {
		out.DenyPaths = merged.DenyPaths
	}
	if touched("cacheDirsDisabled") {
		out.CacheDirsDisabled = merged.CacheDirsDisabled
	}
	if touched("useBwrap") {
		out.UseBwrap = merged.UseBwrap
	}
	if touched("extendTo") {
		out.ExtendTo = merged.ExtendTo
	}
	if touched("allowAutoEscalation") {
		out.AllowAutoEscalation = merged.AllowAutoEscalation
	}
	if touched("env.inherit") {
		out.Env.Inherit = merged.Env.Inherit
	}
	if touched("env.keepSecrets") {
		out.Env.KeepSecrets = merged.Env.KeepSecrets
	}
	if touched("env.exclude") {
		out.Env.Exclude = merged.Env.Exclude
	}
	if touched("env.keep") {
		out.Env.Keep = merged.Env.Keep
	}
	return out
}

// tightenSandboxConfig layers a project-local sandbox section over the global
// one under the "project config may only tighten" rule:
//
//   - Disabled / Mode: the project value wins only when strictly tighter
//     (off < workspace-write < read-only|strict). A project can turn a
//     globally disabled sandbox on, never off. read-only and strict do not
//     replace each other.
//   - Network: a project can only set "restricted".
//   - AutoAllowBashDisabled, CacheDirsDisabled: a project can only set true.
//   - AllowAutoEscalation, Env.KeepSecrets, Env.Keep: global only.
//   - UseBwrap: only towards "always"; Env.Inherit: only towards "none".
//   - DenyPaths, ExtendTo, Env.Exclude: unioned.
//   - WritableRoots, ReadOnlyRoots: project entries are kept only when they
//     resolve inside the workspace (relative entries are resolved against
//     it); anything else is dropped with a warning.
func tightenSandboxConfig(global, project SandboxConfig, workspace string) SandboxConfig {
	out := global
	out.WritableRoots = append([]string(nil), global.WritableRoots...)
	out.ReadOnlyRoots = append([]string(nil), global.ReadOnlyRoots...)
	out.DenyPaths = append([]string(nil), global.DenyPaths...)
	out.ExtendTo = append([]string(nil), global.ExtendTo...)
	out.Env.Exclude = append([]string(nil), global.Env.Exclude...)
	out.Env.Keep = append([]string(nil), global.Env.Keep...)

	globalMode := effectiveSandboxMode(global)
	if projectMode := normalizeSandboxEnum(project.Mode); projectMode != "" {
		rank, ok := sandboxModeRank[projectMode]
		switch {
		case !ok:
			logging.Warn("Ignoring unknown project sandbox mode", "mode", project.Mode)
		case rank > sandboxModeRank[globalMode]:
			out.Mode = projectMode
			out.Disabled = false
		case projectMode != globalMode:
			logging.Warn("Ignoring project sandbox mode: a project config may only tighten the sandbox",
				"project", projectMode, "global", globalMode)
		}
	}
	if project.Disabled && !global.Disabled {
		logging.Warn("Ignoring project sandbox Disabled=true: a project config may only tighten the sandbox")
	}

	if normalizeSandboxEnum(project.Network) == SandboxNetworkRestricted {
		out.Network = SandboxNetworkRestricted
	}
	out.AutoAllowBashDisabled = global.AutoAllowBashDisabled || project.AutoAllowBashDisabled
	out.CacheDirsDisabled = global.CacheDirsDisabled || project.CacheDirsDisabled

	if pb := normalizeSandboxEnum(project.UseBwrap); pb != "" {
		gb := normalizeSandboxEnum(global.UseBwrap)
		if _, ok := sandboxBwrapRank[gb]; !ok {
			gb = SandboxBwrapAuto
		}
		if rank, ok := sandboxBwrapRank[pb]; ok && rank > sandboxBwrapRank[gb] {
			out.UseBwrap = pb
		}
	}
	if pi := normalizeSandboxEnum(project.Env.Inherit); pi != "" {
		gi := normalizeSandboxEnum(global.Env.Inherit)
		if _, ok := sandboxInheritRank[gi]; !ok {
			gi = SandboxEnvInheritAll
		}
		if rank, ok := sandboxInheritRank[pi]; ok && rank > sandboxInheritRank[gi] {
			out.Env.Inherit = pi
		}
	}

	out.DenyPaths = normalizeSandboxList(append(out.DenyPaths, project.DenyPaths...))
	out.ExtendTo = normalizeSandboxList(append(out.ExtendTo, project.ExtendTo...))
	out.Env.Exclude = normalizeSandboxList(append(out.Env.Exclude, project.Env.Exclude...))

	out.WritableRoots = normalizeSandboxList(append(out.WritableRoots, projectRootsInsideWorkspace("writableRoots", project.WritableRoots, workspace)...))
	out.ReadOnlyRoots = normalizeSandboxList(append(out.ReadOnlyRoots, projectRootsInsideWorkspace("readOnlyRoots", project.ReadOnlyRoots, workspace)...))

	if project.AllowAutoEscalation && !global.AllowAutoEscalation {
		logging.Warn("Ignoring project sandbox AllowAutoEscalation: a project config may only tighten the sandbox")
	}
	if project.Env.KeepSecrets && !global.Env.KeepSecrets {
		logging.Warn("Ignoring project sandbox Env.KeepSecrets: a project config may only tighten the sandbox")
	}
	if len(project.Env.Keep) > 0 {
		logging.Warn("Ignoring project sandbox Env.Keep: a project config may only tighten the sandbox")
	}
	return out
}

// projectRootsInsideWorkspace returns the absolute form of the project roots
// that resolve inside workspace, dropping (and logging) the rest. Paths are
// compared lexically after ~ expansion and, when both exist, after resolving
// symlinks, so "sub/../../etc" and a symlink pointing out are both rejected.
func projectRootsInsideWorkspace(field string, roots []string, workspace string) []string {
	if len(roots) == 0 {
		return nil
	}
	ws := strings.TrimSpace(workspace)
	if ws == "" {
		logging.Warn("Ignoring project sandbox roots: no workspace", "field", field)
		return nil
	}
	if abs, err := filepath.Abs(ws); err == nil {
		ws = abs
	}
	ws = filepath.Clean(ws)
	wsReal := ws
	if real, err := filepath.EvalSymlinks(ws); err == nil {
		wsReal = real
	}

	home, _ := os.UserHomeDir()
	var kept []string
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		path := root
		switch {
		case path == "~":
			path = home
		case strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`):
			path = filepath.Join(home, path[2:])
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(ws, path)
		}
		path = filepath.Clean(path)

		inside := pathWithin(path, ws)
		if inside {
			if real, err := filepath.EvalSymlinks(path); err == nil {
				inside = pathWithin(real, wsReal)
			}
		}
		if !inside {
			logging.Warn("Ignoring project sandbox root outside the workspace: a project config may only tighten the sandbox",
				"field", field, "root", root)
			continue
		}
		kept = append(kept, path)
	}
	return kept
}

// pathWithin reports whether path equals dir or lies below it (lexically).
func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// normalizeSandboxList trims entries and drops empty and duplicate ones,
// keeping the first occurrence's order. Returns nil for an empty result.
func normalizeSandboxList(values []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
