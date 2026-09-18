package sandbox

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// EnvVar is the environment variable overriding the sandbox mode:
// off | workspace-write | read-only | strict. on/true/1 enables the
// configured mode (workspace-write when none); false/0/disabled mean off.
const EnvVar = config.SandboxEnvVar

// ResolveOptions is the outside world ResolveConfig reads. Zero fields mean
// "nothing": Resolve fills them from the real process and configuration.
type ResolveOptions struct {
	// GOOS selects the per-OS path tables (default runtime.GOOS).
	GOOS string
	// HomeDir is the user's home directory.
	HomeDir string
	// LookupEnv reads environment variables (PANDO_SANDBOX, TMPDIR, cache
	// overrides, ...).
	LookupEnv func(string) (string, bool)
	// IsLocked reports whether a dotted config path (e.g. "sandbox.mode") is
	// locked by an enterprise overlay. A locked path ignores PANDO_SANDBOX.
	IsLocked func(path string) bool
	// DataDir is config Data.Directory (protected; relative to the workspace).
	DataDir string
	// LocalConfigFile is the project config file found for the workspace,
	// protected in addition to <ws>/.pando.{toml,json}.
	LocalConfigFile string
	// GuardedPorts are the TCP ports of Pando's own listeners (GuardedPorts),
	// copied into Policy.DenyConnectPorts while the network is allowed.
	GuardedPorts []int
}

// Resolve builds the effective policy from the loaded configuration and the
// workspace (defaulting to cfg.WorkingDir, then the process cwd). cfg may be
// nil, which resolves the defaults. The environment override and enterprise
// locks are applied; the project-only-tightens rule was already applied to
// cfg.Sandbox by config.Load.
func Resolve(cfg *config.Config, workspace string) Policy {
	var sc config.SandboxConfig
	opts := ResolveOptions{
		GOOS:      runtime.GOOS,
		LookupEnv: os.LookupEnv,
		IsLocked:  config.IsKeyLocked,
	}
	if cfg != nil {
		sc = cfg.Sandbox
		opts.DataDir = cfg.Data.Directory
		if strings.TrimSpace(workspace) == "" {
			workspace = cfg.WorkingDir
		}
	}
	if strings.TrimSpace(workspace) == "" {
		workspace, _ = os.Getwd()
	}
	if home, err := os.UserHomeDir(); err == nil {
		opts.HomeDir = home
	}
	opts.LocalConfigFile = config.FindLocalConfigFile(workspace)
	opts.GuardedPorts = GuardedPorts()
	return ResolveConfig(sc, workspace, opts)
}

// ResolveConfig is Resolve with every input explicit; it touches neither the
// filesystem nor the process environment beyond what opts provides.
//
// Mode precedence: lock > env > config > default. A locked sandbox.mode or
// sandbox.disabled makes PANDO_SANDBOX ignored for that aspect (an overlay
// that locks the sandbox on cannot be undone from the environment).
func ResolveConfig(sc config.SandboxConfig, workspace string, opts ResolveOptions) Policy {
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	isLocked := opts.IsLocked
	if isLocked == nil {
		isLocked = func(string) bool { return false }
	}
	e := pathEnv{goos: opts.GOOS, home: opts.HomeDir, lookup: opts.LookupEnv}

	ws := strings.TrimSpace(workspace)
	if ws != "" {
		if !isAbs(ws, opts.GOOS) {
			if abs, err := filepath.Abs(ws); err == nil {
				ws = abs
			}
		}
		ws = filepath.Clean(ws)
	}

	// Mode from config.
	mode, ok := ParseMode(sc.Mode)
	if !ok {
		slog.Warn("sandbox: unknown mode in config, using the default", "mode", sc.Mode)
		mode = ModeWorkspaceWrite
	}
	source := SourceDefault
	if strings.TrimSpace(sc.Mode) != "" || sc.Disabled {
		source = SourceConfig
	}
	if sc.Disabled {
		mode = ModeOff
	}
	modeLocked := isLocked("sandbox.mode") || isLocked("sandbox.disabled")
	if modeLocked {
		source = SourceLock
	}

	// Environment override, unless locked.
	if raw, set := lookup(opts.LookupEnv, EnvVar); set && strings.TrimSpace(raw) != "" {
		envMode, ok := parseEnvMode(raw)
		if ok && envMode == modeOn {
			// "on" re-enables the configured mode rather than forcing the
			// default one.
			envMode, _ = ParseMode(sc.Mode)
			if envMode == ModeOff || envMode == "" {
				envMode = ModeWorkspaceWrite
			}
		}
		switch {
		case !ok:
			slog.Warn("sandbox: ignoring invalid "+EnvVar, "value", raw)
		case modeLocked:
			if envMode != mode {
				slog.Warn("sandbox: ignoring "+EnvVar+", the sandbox mode is locked by policy", "value", raw)
			}
		default:
			mode = envMode
			source = SourceEnv
		}
	}

	p := Policy{Mode: mode, Workspace: ws, Source: source}
	if mode == ModeOff {
		// Nothing to enforce; keep the hash distinct and stable.
		p.Network = NetworkAllowed
		p.UseBwrap = BwrapAuto
		p.Env = EnvPolicy{Inherit: EnvInheritAll}
		return p
	}

	// Network: read-only and strict always restrict.
	p.Network = NetworkAllowed
	if strings.EqualFold(strings.TrimSpace(sc.Network), config.SandboxNetworkRestricted) ||
		mode == ModeReadOnly || mode == ModeStrict {
		p.Network = NetworkRestricted
	}

	expand := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, r := range in {
			out = append(out, expandPath(r, opts.HomeDir, ws, opts.GOOS))
		}
		return out
	}

	temps := tempRoots(e)
	var writable []string
	switch mode {
	case ModeWorkspaceWrite:
		writable = append(writable, ws)
		writable = append(writable, temps...)
		if !sc.CacheDirsDisabled {
			writable = append(writable, cacheRoots(e)...)
		}
		writable = append(writable, expand(sc.WritableRoots)...)
	case ModeStrict:
		writable = append(writable, ws)
		writable = append(writable, temps...)
		writable = append(writable, expand(sc.WritableRoots)...)
	case ModeReadOnly:
		writable = append(writable, temps...)
	}
	p.WritableRoots = cleanList(writable)

	if mode == ModeStrict {
		readable := append([]string{ws}, p.WritableRoots...)
		readable = append(readable, systemReadRoots(e)...)
		readable = append(readable, expand(sc.ReadOnlyRoots)...)
		p.ReadableRoots = cleanList(readable)
	}

	p.ProtectedPaths = cleanList(protectedPaths(e, ws, opts.DataDir, opts.LocalConfigFile))
	p.DenyPaths = cleanList(expand(sc.DenyPaths))

	p.Env = EnvPolicy{
		Inherit:      EnvInheritAll,
		ScrubSecrets: !sc.Env.KeepSecrets,
		Exclude:      cleanList(sc.Env.Exclude),
		Keep:         cleanList(sc.Env.Keep),
	}
	switch EnvInherit(strings.ToLower(strings.TrimSpace(sc.Env.Inherit))) {
	case EnvInheritCore:
		p.Env.Inherit = EnvInheritCore
	case EnvInheritNone:
		p.Env.Inherit = EnvInheritNone
	}

	p.AutoAllowBash = !sc.AutoAllowBashDisabled
	p.AllowAutoEscalation = sc.AllowAutoEscalation

	p.UseBwrap = BwrapAuto
	switch BwrapPolicy(strings.ToLower(strings.TrimSpace(sc.UseBwrap))) {
	case BwrapAlways:
		p.UseBwrap = BwrapAlways
	case BwrapNever:
		p.UseBwrap = BwrapNever
	}

	extend := append([]Purpose(nil), DefaultExtendTo...)
	for _, t := range sc.ExtendTo {
		switch pt := Purpose(strings.ToLower(strings.TrimSpace(t))); pt {
		case PurposeACPTerminals, PurposeSkills, PurposeMCP, PurposeSubagents:
			extend = append(extend, pt)
		}
	}
	p.ExtendTo = sortedPurposes(extend)

	if !p.RestrictsNetwork() {
		p.DenyConnectPorts = cleanPorts(opts.GuardedPorts)
	}
	return p
}

// modeOn is parseEnvMode's answer for a bare "on": enabled, configured mode.
const modeOn Mode = "on"

// parseEnvMode parses a PANDO_SANDBOX value.
func parseEnvMode(raw string) (Mode, bool) {
	switch normalize(raw) {
	case "on", "true", "1", "yes", "enabled":
		return modeOn, true
	case "false", "0", "no", "disabled", "none":
		return ModeOff, true
	}
	return ParseMode(raw)
}

func lookup(fn func(string) (string, bool), key string) (string, bool) {
	if fn == nil {
		return "", false
	}
	return fn(key)
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func sortedPurposes(in []Purpose) []Purpose {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}
