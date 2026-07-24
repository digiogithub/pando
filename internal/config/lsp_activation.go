package config

import (
	"fmt"
	"strings"
	"time"
)

// LSPTrigger identifies what caused an on-demand language server activation.
// Every activation site declares its trigger so LSPActivateOn can decide
// whether a server may be started at all.
type LSPTrigger string

const (
	// LSPTriggerEdit is a file modified by Pando itself through the edit,
	// write or patch tools.
	LSPTriggerEdit LSPTrigger = "edit"
	// LSPTriggerRead is a file merely read or opened: the view tool, the TUI
	// file tree, the editor.
	LSPTriggerRead LSPTrigger = "read"
	// LSPTriggerWorkspace is a file changed in the workspace by something other
	// than Pando (an external editor, a build step), seen by the bootstrap
	// watcher.
	LSPTriggerWorkspace LSPTrigger = "workspace"
	// LSPTriggerExplicit is a direct request for a language server, i.e. a
	// diagnostics tool call naming a file. It is honored in every mode except
	// "off", because refusing it would leave the caller with no way to obtain
	// diagnostics at all.
	LSPTriggerExplicit LSPTrigger = "explicit"
)

// Activation modes accepted by Config.LSPActivateOn, from strictest to loosest.
const (
	// LSPActivateOff never starts a server on demand.
	LSPActivateOff = "off"
	// LSPActivateEdits (default) starts a server only when Pando edits a file
	// of its language, or when diagnostics are requested for one. A session
	// that never edits anything starts no language server at all.
	LSPActivateEdits = "edits"
	// LSPActivateReads also starts a server when a file is read or opened.
	LSPActivateReads = "reads"
	// LSPActivateWorkspace additionally runs a workspace watcher that activates
	// servers for files changed outside Pando.
	LSPActivateWorkspace = "workspace"
)

// LSPActivationMode returns the normalized activation mode. An empty or
// unrecognized value falls back to the default, "edits".
func (c *Config) LSPActivationMode() string {
	if c == nil {
		return LSPActivateEdits
	}
	switch strings.ToLower(strings.TrimSpace(c.LSPActivateOn)) {
	case LSPActivateOff:
		return LSPActivateOff
	case LSPActivateReads:
		return LSPActivateReads
	case LSPActivateWorkspace:
		return LSPActivateWorkspace
	default:
		return LSPActivateEdits
	}
}

// LSPActivationAllows reports whether the given trigger may start a language
// server under the current configuration. LSPAutoActivate=false disables every
// trigger, keeping the historical meaning of that flag.
func (c *Config) LSPActivationAllows(trigger LSPTrigger) bool {
	if c == nil || !c.LSPAutoActivate {
		return false
	}
	mode := c.LSPActivationMode()
	if mode == LSPActivateOff {
		return false
	}
	switch trigger {
	case LSPTriggerExplicit:
		return true
	case LSPTriggerEdit:
		return true // allowed by "edits" and every looser mode
	case LSPTriggerRead:
		return mode == LSPActivateReads || mode == LSPActivateWorkspace
	case LSPTriggerWorkspace:
		return mode == LSPActivateWorkspace
	default:
		return false
	}
}

// LSPWorkspaceWatchEnabled reports whether the workspace-wide bootstrap watcher
// should run. It is off by default: watching the whole tree costs file
// descriptors and can start servers for files Pando never touches.
func (c *Config) LSPWorkspaceWatchEnabled() bool {
	return c.LSPActivationAllows(LSPTriggerWorkspace)
}

// Default waits for a lazily started language server.
const (
	// defaultLSPStartupTimeout covers a cold start of a heavy server (gopls
	// indexing a large module, tsserver loading a monorepo).
	defaultLSPStartupTimeout = "20s"
	// defaultLSPInstallTimeout covers a cold start preceded by an install.
	defaultLSPInstallTimeout = "120s"
)

// Runner preferences accepted by Config.LSPRunner. They select which
// JavaScript package manager may install and run npm-distributed servers.
const (
	// LSPRunnerAuto (default) uses bun when it is installed, npm otherwise.
	LSPRunnerAuto = "auto"
	// LSPRunnerBun pins bun: npm is never used, even when it is the only
	// toolchain present.
	LSPRunnerBun = "bun"
	// LSPRunnerNpm pins npm/npx, for hosts where bun is installed but should
	// not be used (corporate registries, unsupported platforms).
	LSPRunnerNpm = "npm"
	// LSPRunnerOff forbids both, so npm-distributed servers are only used when
	// their binary already exists on PATH or was staged by an earlier run.
	LSPRunnerOff = "off"
)

// LSPRunnerMode returns the normalized runner preference. An empty or
// unrecognized value falls back to the default, "auto".
func (c *Config) LSPRunnerMode() string {
	if c == nil {
		return LSPRunnerAuto
	}
	switch strings.ToLower(strings.TrimSpace(c.LSPRunner)) {
	case LSPRunnerBun:
		return LSPRunnerBun
	case LSPRunnerNpm:
		return LSPRunnerNpm
	case LSPRunnerOff:
		return LSPRunnerOff
	default:
		return LSPRunnerAuto
	}
}

// LSPAutoInstallEnabled reports whether Pando may install a missing
// npm-distributed language server on its own. A runner preference of "off"
// disables installation regardless of LSPAutoInstall: without bun or npm there
// is nothing to install with.
func (c *Config) LSPAutoInstallEnabled() bool {
	return c != nil && c.LSPAutoInstall && c.LSPRunnerMode() != LSPRunnerOff
}

// LSPStartupWait is how long a tool waits for a lazily started server to become
// ready. An unparsable or non-positive value falls back to the default.
func (c *Config) LSPStartupWait() time.Duration {
	return lspDuration(c.lspStartupTimeout(), defaultLSPStartupTimeout)
}

// LSPInstallWait is how long a tool waits when the server has to be installed
// first. It is never shorter than LSPStartupWait.
func (c *Config) LSPInstallWait() time.Duration {
	d := lspDuration(c.lspInstallTimeout(), defaultLSPInstallTimeout)
	if startup := c.LSPStartupWait(); d < startup {
		return startup
	}
	return d
}

func (c *Config) lspStartupTimeout() string {
	if c == nil {
		return ""
	}
	return c.LSPStartupTimeout
}

func (c *Config) lspInstallTimeout() string {
	if c == nil {
		return ""
	}
	return c.LSPInstallTimeout
}

// LSPActivationSettings groups the global on-demand knobs so a surface can read
// and write them as a unit.
type LSPActivationSettings struct {
	AutoActivate   bool   `json:"autoActivate"`
	ActivateOn     string `json:"activateOn"`
	AutoInstall    bool   `json:"autoInstall"`
	Runner         string `json:"runner"`
	StartupTimeout string `json:"startupTimeout"`
	InstallTimeout string `json:"installTimeout"`
}

// LSPActivationSettings returns the current global activation knobs, with the
// mode normalized and the timeouts filled in with their effective values.
func (c *Config) LSPActivationSettings() LSPActivationSettings {
	if c == nil {
		return LSPActivationSettings{
			ActivateOn:     LSPActivateEdits,
			Runner:         LSPRunnerAuto,
			StartupTimeout: defaultLSPStartupTimeout,
			InstallTimeout: defaultLSPInstallTimeout,
		}
	}
	return LSPActivationSettings{
		AutoActivate:   c.LSPAutoActivate,
		ActivateOn:     c.LSPActivationMode(),
		AutoInstall:    c.LSPAutoInstall,
		Runner:         c.LSPRunnerMode(),
		StartupTimeout: c.LSPStartupWait().String(),
		InstallTimeout: c.LSPInstallWait().String(),
	}
}

// UpdateLSPActivation validates and persists the global activation knobs.
func UpdateLSPActivation(s LSPActivationSettings) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	mode := strings.ToLower(strings.TrimSpace(s.ActivateOn))
	switch mode {
	case "":
		mode = LSPActivateEdits
	case LSPActivateOff, LSPActivateEdits, LSPActivateReads, LSPActivateWorkspace:
	default:
		return fmt.Errorf("invalid LSPActivateOn %q (want off, edits, reads or workspace)", s.ActivateOn)
	}

	runner := strings.ToLower(strings.TrimSpace(s.Runner))
	switch runner {
	case "":
		runner = LSPRunnerAuto
	case LSPRunnerAuto, LSPRunnerBun, LSPRunnerNpm, LSPRunnerOff:
	default:
		return fmt.Errorf("invalid LSPRunner %q (want auto, bun, npm or off)", s.Runner)
	}

	startup, err := lspTimeoutValue(s.StartupTimeout, defaultLSPStartupTimeout)
	if err != nil {
		return fmt.Errorf("invalid LSPStartupTimeout: %w", err)
	}
	install, err := lspTimeoutValue(s.InstallTimeout, defaultLSPInstallTimeout)
	if err != nil {
		return fmt.Errorf("invalid LSPInstallTimeout: %w", err)
	}

	old := LSPActivationSettings{
		AutoActivate:   cfg.LSPAutoActivate,
		ActivateOn:     cfg.LSPActivateOn,
		AutoInstall:    cfg.LSPAutoInstall,
		Runner:         cfg.LSPRunner,
		StartupTimeout: cfg.LSPStartupTimeout,
		InstallTimeout: cfg.LSPInstallTimeout,
	}

	apply := func(c *Config) {
		c.LSPAutoActivate = s.AutoActivate
		c.LSPActivateOn = mode
		c.LSPAutoInstall = s.AutoInstall
		c.LSPRunner = runner
		c.LSPStartupTimeout = startup
		c.LSPInstallTimeout = install
	}

	apply(cfg)
	if err := updateCfgFile(apply); err != nil {
		apply2 := func(c *Config) {
			c.LSPAutoActivate = old.AutoActivate
			c.LSPActivateOn = old.ActivateOn
			c.LSPAutoInstall = old.AutoInstall
			c.LSPRunner = old.Runner
			c.LSPStartupTimeout = old.StartupTimeout
			c.LSPInstallTimeout = old.InstallTimeout
		}
		apply2(cfg)
		return err
	}
	return nil
}

// lspTimeoutValue normalizes a user-supplied duration, rejecting values that
// would silently fall back to the default at read time.
func lspTimeoutValue(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return "", err
	}
	if d <= 0 {
		return "", fmt.Errorf("%q must be positive", value)
	}
	return value, nil
}

func lspDuration(value, fallback string) time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(value)); err == nil && d > 0 {
		return d
	}
	d, _ := time.ParseDuration(fallback)
	return d
}
