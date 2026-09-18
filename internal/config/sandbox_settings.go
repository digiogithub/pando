package config

import (
	"reflect"
	"strings"

	"github.com/spf13/viper"

	"github.com/digiogithub/pando/internal/logging"
)

// sandboxFieldPtrs maps every leaf of a SandboxConfig to its dotted config
// path ("sandbox.<jsonName>") and a pointer to the field, so lock handling can
// walk the section generically. Keep it in step with SandboxConfig.
func sandboxFieldPtrs(sc *SandboxConfig) []sandboxFieldPtr {
	return []sandboxFieldPtr{
		{"sandbox.disabled", &sc.Disabled},
		{"sandbox.mode", &sc.Mode},
		{"sandbox.network", &sc.Network},
		{"sandbox.autoAllowBashDisabled", &sc.AutoAllowBashDisabled},
		{"sandbox.writableRoots", &sc.WritableRoots},
		{"sandbox.readOnlyRoots", &sc.ReadOnlyRoots},
		{"sandbox.denyPaths", &sc.DenyPaths},
		{"sandbox.cacheDirsDisabled", &sc.CacheDirsDisabled},
		{"sandbox.useBwrap", &sc.UseBwrap},
		{"sandbox.extendTo", &sc.ExtendTo},
		{"sandbox.allowAutoEscalation", &sc.AllowAutoEscalation},
		{"sandbox.env.inherit", &sc.Env.Inherit},
		{"sandbox.env.keepSecrets", &sc.Env.KeepSecrets},
		{"sandbox.env.exclude", &sc.Env.Exclude},
		{"sandbox.env.keep", &sc.Env.Keep},
	}
}

type sandboxFieldPtr struct {
	path string
	ptr  any
}

// SandboxFieldPaths lists the dotted config path of every sandbox setting.
func SandboxFieldPaths() []string {
	var sc SandboxConfig
	fields := sandboxFieldPtrs(&sc)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, f.path)
	}
	return out
}

// IsSandboxFieldLocked reports whether a sandbox field path is locked. The
// on/off switch and the mode are one decision (Disabled forces mode off, and
// mode "off" disables), so a lock on either sandbox.disabled or sandbox.mode
// locks both; every other path is IsKeyLocked.
func IsSandboxFieldLocked(path string) bool {
	if strings.EqualFold(path, "sandbox.disabled") || strings.EqualFold(path, "sandbox.mode") {
		return IsKeyLocked("sandbox.disabled") || IsKeyLocked("sandbox.mode")
	}
	return IsKeyLocked(path)
}

// LockedSandboxFields returns the sandbox field paths an enterprise overlay
// (or restricted-path source) has locked, in SandboxFieldPaths order. A lock
// on the whole "sandbox" section locks every field; see IsSandboxFieldLocked
// for the disabled/mode coupling. Never nil.
func LockedSandboxFields() []string {
	out := []string{}
	for _, path := range SandboxFieldPaths() {
		if IsSandboxFieldLocked(path) {
			out = append(out, path)
		}
	}
	return out
}

// keepLockedSandboxFields returns dst with every locked field copied from src.
func keepLockedSandboxFields(dst, src SandboxConfig) SandboxConfig {
	dstFields := sandboxFieldPtrs(&dst)
	srcFields := sandboxFieldPtrs(&src)
	for i, f := range dstFields {
		if IsSandboxFieldLocked(f.path) {
			reflect.ValueOf(f.ptr).Elem().Set(reflect.ValueOf(srcFields[i].ptr).Elem())
		}
	}
	return dst
}

// ErrIfSandboxLockedChange returns a *LockedKeyError naming the first locked
// sandbox field whose value differs between before and after (both compared
// after NormalizeSandboxConfig), and nil when no locked field changes. A
// settings surface uses it to refuse an explicit change of a managed field
// while still accepting a save that merely echoes the managed value back.
func ErrIfSandboxLockedChange(before, after SandboxConfig) error {
	if len(LockedKeys()) == 0 {
		return nil
	}
	if n, err := NormalizeSandboxConfig(before); err == nil {
		before = n
	}
	if n, err := NormalizeSandboxConfig(after); err == nil {
		after = n
	}
	beforeFields := sandboxFieldPtrs(&before)
	afterFields := sandboxFieldPtrs(&after)
	for i, f := range beforeFields {
		if !IsSandboxFieldLocked(f.path) {
			continue
		}
		a := reflect.ValueOf(f.ptr).Elem()
		b := reflect.ValueOf(afterFields[i].ptr).Elem()
		if a.Kind() == reflect.Slice && a.Len() == 0 && b.Len() == 0 {
			continue
		}
		if !reflect.DeepEqual(a.Interface(), b.Interface()) {
			return &LockedKeyError{Key: f.path}
		}
	}
	return nil
}

// GlobalSandboxConfig reads the sandbox section of the GLOBAL config file on
// its own: the value UpdateSandbox persists, without the project-local
// tightening, the overlays or the environment. It returns the zero value when
// there is no global file or the section is absent or malformed.
func GlobalSandboxConfig() SandboxConfig {
	path, err := resolveGlobalConfigFilePath()
	if err != nil || path == "" {
		return SandboxConfig{}
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return SandboxConfig{}
	}
	var sc SandboxConfig
	if err := v.UnmarshalKey("sandbox", &sc); err != nil {
		logging.Warn("Ignoring malformed global sandbox config", "path", path, "error", err)
		return SandboxConfig{}
	}
	return sc
}

// SandboxSettingsView is the sandbox section a settings surface edits: the
// global file's values (what the user controls and UpdateSandbox writes),
// with every locked field replaced by the enforced in-memory value. Starting
// an edit from this view, rather than from Get().Sandbox, keeps the
// project-local tightening out of the global file.
func SandboxSettingsView() SandboxConfig {
	view := GlobalSandboxConfig()
	if cfg != nil {
		view = keepLockedSandboxFields(view, cfg.Sandbox)
	}
	if n, err := NormalizeSandboxConfig(view); err == nil {
		view = n
	}
	return view
}
