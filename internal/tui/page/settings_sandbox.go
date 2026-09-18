package page

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
	"github.com/digiogithub/pando/internal/tui/components/settings"
)

// sandboxFieldConfigPath maps a Sandbox settings field key to the config path
// the lock list and the write path know. The UI shows friendly inverted
// toggles ("enabled", "autoAllowBash") over the Disabled flags the config
// stores, and one toggle per optional extendTo target. It returns "" for a
// key that is not a sandbox setting.
func sandboxFieldConfigPath(key string) string {
	switch {
	case key == "sandbox.enabled":
		return "sandbox.disabled"
	case key == "sandbox.autoAllowBash":
		return "sandbox.autoAllowBashDisabled"
	case strings.HasPrefix(key, "sandbox.extendTo."):
		return "sandbox.extendTo"
	case strings.HasPrefix(key, "sandbox."):
		return key
	}
	return ""
}

// sandboxFieldLocked reports whether a Sandbox settings field is locked,
// honouring the disabled/mode coupling (config.IsSandboxFieldLocked).
func sandboxFieldLocked(key string) bool {
	path := sandboxFieldConfigPath(key)
	return path != "" && config.IsSandboxFieldLocked(path)
}

// sandboxBadge is the short status used by the chat sidebar and footer, e.g.
// "workspace-write (landlock+seccomp v5)" or "off".
func sandboxBadge() string {
	return sandbox.CurrentStatus().Label()
}

// buildSandboxSection renders the host command sandbox settings. Values come
// from config.SandboxSettingsView (the global file plus enforced locks), not
// from cfg.Sandbox, so that a save never copies the project-local tightening
// into the global file. cfg is only used to know a config is loaded.
func buildSandboxSection(cfg *config.Config) settings.Section {
	_ = cfg
	view := config.SandboxSettingsView()
	status := sandbox.CurrentStatus()

	mode := view.Mode
	if mode == "" {
		mode = config.SandboxModeWorkspaceWrite
	}
	network := view.Network
	if network == "" {
		network = config.SandboxNetworkAllowed
	}
	useBwrap := view.UseBwrap
	if useBwrap == "" {
		useBwrap = config.SandboxBwrapAuto
	}

	statusHint := ""
	switch {
	case !status.Policy.Enabled():
		statusHint = "Sandbox is off: agent commands run with your full user permissions."
	case !status.Capability.Enforced:
		statusHint = "Not enforced on this OS: commands run unconfined and bash still asks for approval."
	case !status.Full:
		statusHint = "Partial protection (" + strings.Join(status.Gaps, "; ") + "): bash still asks for approval."
	}
	if v, ok := os.LookupEnv(config.SandboxEnvVar); ok && strings.TrimSpace(v) != "" {
		if statusHint != "" {
			statusHint += " "
		}
		statusHint += fmt.Sprintf("%s=%s overrides the configured mode for this process.", config.SandboxEnvVar, strings.TrimSpace(v))
	}

	bwrapHint := "Linux only: bubblewrap protects Pando's config inside the workspace."
	if runtime.GOOS != "linux" {
		bwrapHint = "Linux only: ignored on " + runtime.GOOS + "."
	}

	protected := strings.Join(status.Policy.ProtectedPaths, ",")
	if protected == "" {
		protected = "—"
	}

	return settings.Section{
		Title: "Sandbox",
		Fields: []settings.Field{
			{
				Label:    "Status",
				Key:      "sandbox.backend",
				Type:     settings.FieldText,
				Value:    status.Label(),
				ReadOnly: true,
				Hint:     statusHint,
			},
			{
				Label: "Sandbox Enabled",
				Key:   "sandbox.enabled",
				Type:  settings.FieldToggle,
				Value: boolString(!view.Disabled),
				Hint:  "Confines agent commands; changes apply to the next command (the shell restarts).",
			},
			{
				Label:   "Mode",
				Key:     "sandbox.mode",
				Type:    settings.FieldSelect,
				Options: []string{config.SandboxModeWorkspaceWrite, config.SandboxModeReadOnly, config.SandboxModeStrict, config.SandboxModeOff},
				Value:   mode,
			},
			{
				Label:   "Network",
				Key:     "sandbox.network",
				Type:    settings.FieldSelect,
				Options: []string{config.SandboxNetworkAllowed, config.SandboxNetworkRestricted},
				Value:   network,
				Hint:    "read-only and strict always restrict the network.",
			},
			{
				Label: "Auto-allow Bash",
				Key:   "sandbox.autoAllowBash",
				Type:  settings.FieldToggle,
				Value: boolString(!view.AutoAllowBashDisabled),
				Hint:  "Skip the bash approval prompt while the sandbox is enforced.",
			},
			{
				Label:   "Use Bubblewrap",
				Key:     "sandbox.useBwrap",
				Type:    settings.FieldSelect,
				Options: []string{config.SandboxBwrapAuto, config.SandboxBwrapAlways, config.SandboxBwrapNever},
				Value:   useBwrap,
				Hint:    bwrapHint,
			},
			{
				Label: "Writable Roots",
				Key:   "sandbox.writableRoots",
				Type:  settings.FieldText,
				Value: strings.Join(view.WritableRoots, ","),
				Hint:  "Extra writable directories, comma separated.",
			},
			{
				Label: "Deny Paths",
				Key:   "sandbox.denyPaths",
				Type:  settings.FieldText,
				Value: strings.Join(view.DenyPaths, ","),
				Hint:  "Denied for read and write, comma separated (globs allowed).",
			},
			{
				Label: "Sandbox MCP Servers",
				Key:   "sandbox.extendTo." + config.SandboxExtendMCP,
				Type:  settings.FieldToggle,
				Value: boolString(slices.Contains(view.ExtendTo, config.SandboxExtendMCP)),
			},
			{
				Label: "Sandbox Subagents",
				Key:   "sandbox.extendTo." + config.SandboxExtendSubagents,
				Type:  settings.FieldToggle,
				Value: boolString(slices.Contains(view.ExtendTo, config.SandboxExtendSubagents)),
				Hint:  "ACP terminals and skills are always sandboxed.",
			},
			{
				Label:    "Protected Paths",
				Key:      "sandbox.protectedPaths",
				Type:     settings.FieldText,
				Value:    protected,
				ReadOnly: true,
			},
		},
	}
}

// saveSandbox persists one Sandbox settings field through config.UpdateSandbox,
// starting from the settings view so the other fields keep their global value.
func saveSandbox(field settings.Field) error {
	if config.Get() == nil {
		return fmt.Errorf("config not loaded")
	}
	sc := config.SandboxSettingsView()

	switch key := field.Key; {
	case key == "sandbox.backend" || key == "sandbox.protectedPaths":
		// read-only informational fields, nothing to save
		return nil
	case key == "sandbox.enabled":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Sandbox Enabled value: %w", err)
		}
		sc.Disabled = !enabled
		if enabled && sc.Mode == config.SandboxModeOff {
			// Turning the sandbox back on from mode "off" means the default mode.
			sc.Mode = ""
		}
	case key == "sandbox.mode":
		sc.Mode = strings.TrimSpace(field.Value)
		if sc.Mode == config.SandboxModeWorkspaceWrite {
			sc.Mode = ""
		}
		if sc.Mode != config.SandboxModeOff {
			// Picking a mode implies the sandbox is on.
			sc.Disabled = false
		}
	case key == "sandbox.network":
		sc.Network = strings.TrimSpace(field.Value)
		if sc.Network == config.SandboxNetworkAllowed {
			sc.Network = ""
		}
	case key == "sandbox.autoAllowBash":
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid Auto-allow Bash value: %w", err)
		}
		sc.AutoAllowBashDisabled = !enabled
	case key == "sandbox.useBwrap":
		sc.UseBwrap = strings.TrimSpace(field.Value)
		if sc.UseBwrap == config.SandboxBwrapAuto {
			sc.UseBwrap = ""
		}
	case key == "sandbox.writableRoots":
		sc.WritableRoots = splitCommaList(field.Value)
	case key == "sandbox.denyPaths":
		sc.DenyPaths = splitCommaList(field.Value)
	case strings.HasPrefix(key, "sandbox.extendTo."):
		target := strings.TrimPrefix(key, "sandbox.extendTo.")
		enabled, err := parseBoolValue(field.Value)
		if err != nil {
			return fmt.Errorf("invalid %s value: %w", field.Label, err)
		}
		sc.ExtendTo = slices.DeleteFunc(slices.Clone(sc.ExtendTo), func(v string) bool { return v == target })
		if enabled {
			sc.ExtendTo = append(sc.ExtendTo, target)
		}
	default:
		return fmt.Errorf("unsupported sandbox setting %q", key)
	}

	return config.UpdateSandbox(sc)
}
