package extension

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultConfigEnvPrefix is the environment-variable prefix HostServices uses
// when the host does not set one. It matches the prefix the configuration
// system itself binds, so the two halves of the configuration read from the
// same namespace.
const DefaultConfigEnvPrefix = "PANDO"

// HostServices is the single value handed to every extension on Provision. It
// carries the extension's own configuration plus the host facilities it is
// allowed to use.
//
// Every service is an interface declared in this package and satisfied by a
// core type. That indirection is deliberate: it is what lets core refactor its
// internals without breaking out-of-tree extensions.
//
// Fields are added as capabilities land (agent, sessions, permissions and the
// event bus arrive with P1/P2). Adding a field is backwards compatible;
// removing or retyping one is not.
type HostServices struct {
	// ID is the extension this value was built for. It is what names the
	// extension in log records and in the environment variables that can
	// override its configuration (see the accessors below). Empty in a host
	// that hands the same value to nobody in particular.
	ID ID

	// Raw is this extension's own configuration subtree, as written under
	// [Extensions.Entries."<id>".Config] in pando.toml. Never nil.
	//
	// Its top-level keys are folded to lower case, because that is what the
	// configuration system does to every key it reads: an author who writes
	// baseURL in a file finds baseurl here. Prefer the typed accessors below,
	// which fold the key being asked for as well and so accept any spelling;
	// index Raw directly only for shapes they do not cover, and then with a
	// lower-case key.
	Raw map[string]any

	// EnvPrefix is the environment-variable prefix used by the configuration
	// accessors. Empty means DefaultConfigEnvPrefix.
	EnvPrefix string

	// Config is a read-only view over the host configuration.
	Config ConfigView

	// Logger is scoped to the extension: records already carry its ID.
	Logger *slog.Logger

	// WorkingDir is the absolute path of the project Pando is running against.
	WorkingDir string

	// CoreVersion is the Pando core version this binary was built from.
	CoreVersion string

	// Variant identifies the build variant ("", "enterprise", ...). Extensions
	// should not branch on it; it exists for reporting.
	Variant string

	// ConfigOverlays lets an extension that implements ConfigOverlayProvider
	// tell the host its overlay document has changed. Nil in hosts that do not
	// support configuration overlays, so check before calling.
	ConfigOverlays ConfigOverlayController

	// Prompts runs a non-interactive prompt through the host's agent. Nil in a
	// host that has no agent, so check before calling.
	Prompts PromptRunner
}

// Reading the extension's own configuration.
//
// Every accessor below resolves a key the same way, through Lookup:
//
//  1. an environment variable named from the extension ID and the key, so a
//     container can override any option without a configuration file;
//  2. the value in Raw, matched case-insensitively.
//
// Case-insensitivity is not a convenience, it is what makes the accessors
// agree with the configuration system underneath them. That system lowercases
// every key it reads, so a subtree written as
//
//	[Extensions.Entries."tools.acme".Config]
//	baseURL = "https://acme.internal"
//
// arrives as Raw["baseurl"]. An exact-key lookup of "baseURL" would therefore
// never find it, whatever the author wrote, and the extension would silently
// run on its defaults. Folding both sides removes that trap: an extension
// asking for "baseURL" finds a value written as baseURL, baseurl or BASEURL.
//
// Values are also coerced across the representations a configuration file can
// produce: TOML integers decode as int64 and JSON numbers as float64, and an
// environment variable always arrives as a string, so the numeric and boolean
// accessors parse strings too.

// ConfigEnvVar returns the environment variable that overrides key for this
// extension: the prefix, "EXT", the extension ID and the key, uppercased with
// every character that is not a letter or a digit replaced by an underscore.
// For extension "tools.acme" and key "baseURL" that is
// PANDO_EXT_TOOLS_ACME_BASEURL.
//
// It returns "" when the host set no extension ID, because there is then no
// namespace to read an override from.
func (h HostServices) ConfigEnvVar(key string) string {
	if h.ID == "" || strings.TrimSpace(key) == "" {
		return ""
	}
	prefix := h.EnvPrefix
	if prefix == "" {
		prefix = DefaultConfigEnvPrefix
	}
	return envIdentifier(prefix) + "_EXT_" + envIdentifier(string(h.ID)) + "_" + envIdentifier(key)
}

// Lookup resolves one key of the extension's own configuration subtree.
//
// An environment override wins over the configuration file, matching the
// precedence the host applies to core settings. Otherwise the key is matched
// against Raw, first exactly and then case-insensitively. It reports false
// when neither carries the key.
func (h HostServices) Lookup(key string) (any, bool) {
	if name := h.ConfigEnvVar(key); name != "" {
		if v, ok := os.LookupEnv(name); ok {
			return v, true
		}
	}
	if len(h.Raw) == 0 {
		return nil, false
	}
	if v, ok := h.Raw[key]; ok {
		return v, true
	}
	folded := foldConfigKey(key)
	if v, ok := h.Raw[folded]; ok {
		return v, true
	}
	// Raw is normally already folded, so this scan is the fallback for a host
	// that built the map itself. Picking the lexicographically first match
	// keeps the answer deterministic when two spellings collide; the manager
	// warns about such a collision when it hands the subtree over.
	best, found := "", false
	for k := range h.Raw {
		if foldConfigKey(k) != folded {
			continue
		}
		if !found || k < best {
			best, found = k, true
		}
	}
	if !found {
		return nil, false
	}
	return h.Raw[best], true
}

// Bool reads a boolean from the extension's own config subtree.
func (h HostServices) Bool(key string, def bool) bool {
	v, ok := h.Lookup(key)
	if !ok {
		return def
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(x)); err == nil {
			return b
		}
	}
	return def
}

// String reads a string from the extension's own config subtree. An empty
// string is treated as absent, so an option left blank falls back to def.
func (h HostServices) String(key, def string) string {
	v, ok := h.Lookup(key)
	if !ok {
		return def
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// Int reads an integer from the extension's own config subtree. TOML decoding
// yields int64 and JSON yields float64, so both are accepted, as is a decimal
// string from the environment.
func (h HostServices) Int(key string, def int) int {
	v, ok := h.Lookup(key)
	if !ok {
		return def
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return n
		}
	}
	return def
}

// Float64 reads a floating-point number from the extension's own config
// subtree.
func (h HostServices) Float64(key string, def float64) float64 {
	v, ok := h.Lookup(key)
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return f
		}
	}
	return def
}

// Duration reads a duration from the extension's own config subtree. It
// accepts a Go duration string ("30s", "5m"), a time.Duration, and a bare
// number, which is read as seconds because that is what a configuration file
// most often means by a plain 30.
func (h HostServices) Duration(key string, def time.Duration) time.Duration {
	v, ok := h.Lookup(key)
	if !ok {
		return def
	}
	switch x := v.(type) {
	case time.Duration:
		return x
	case string:
		if d, err := time.ParseDuration(strings.TrimSpace(x)); err == nil {
			return d
		}
	case int:
		return time.Duration(x) * time.Second
	case int64:
		return time.Duration(x) * time.Second
	case float64:
		return time.Duration(x * float64(time.Second))
	}
	return def
}

// StringSlice reads a list of strings from the extension's own config subtree.
// It accepts a TOML or JSON list, and a comma-separated string, which is how a
// list arrives through the environment. Empty entries are dropped; the result
// is a copy, so the caller cannot reach into the configuration.
func (h HostServices) StringSlice(key string, def []string) []string {
	v, ok := h.Lookup(key)
	if !ok {
		return def
	}
	var out []string
	switch x := v.(type) {
	case []string:
		out = make([]string, 0, len(x))
		for _, s := range x {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case []any:
		out = make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
	case string:
		for _, part := range strings.Split(x, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	default:
		return def
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// Map reads a nested table from the extension's own config subtree, for an
// option whose value is itself a set of keys (headers, labels, per-model
// settings). It returns nil when the key is absent or is not a table.
//
// The returned map's own keys are left exactly as the configuration system
// produced them: they are data the extension chose to nest, not option names
// this package may fold.
func (h HostServices) Map(key string) map[string]any {
	v, ok := h.Lookup(key)
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case map[string]any:
		return x
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = val
		}
		return out
	}
	return nil
}

// foldConfigKey folds a configuration key to the form the configuration system
// stores it in. Viper lowercases every key it reads, so lowercasing the key
// being asked for is what makes a lookup agree with the file.
func foldConfigKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

// envIdentifier turns an arbitrary name into the shape an environment variable
// can carry: upper case, with everything that is not a letter or a digit
// replaced by an underscore.
func envIdentifier(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToUpper(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// foldConfigSubtree returns cfg with its top-level keys folded, so that direct
// indexing of HostServices.Raw sees the same spelling the accessors resolve.
// Only the top level is folded: those keys are the extension's option names,
// while anything nested underneath is the extension's own data and must be
// handed over untouched.
//
// Two option names that differ only in case would silently shadow each other,
// so every collision is reported through warn (the losing key is dropped and
// the lexicographically first spelling kept, which makes the outcome
// deterministic rather than dependent on map iteration order). warn may be
// nil.
func foldConfigSubtree(cfg map[string]any, warn func(msg string, args ...any)) map[string]any {
	if len(cfg) == 0 {
		return map[string]any{}
	}

	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]any, len(cfg))
	kept := make(map[string]string, len(cfg))
	for _, k := range keys {
		folded := foldConfigKey(k)
		if prev, clash := kept[folded]; clash {
			if warn != nil {
				warn("Extension configuration keys differ only in case; the later one is ignored",
					"key", folded, "kept", prev, "ignored", k)
			}
			continue
		}
		kept[folded] = k
		out[folded] = cfg[k]
	}
	return out
}

// ConfigView exposes the parts of the host configuration an extension may
// read. It is intentionally small: extensions configure themselves through
// their own subtree, and only consult the host for facts they cannot know.
type ConfigView interface {
	// WorkingDir is the project root.
	WorkingDir() string
	// DataDir is the per-project Pando data directory (.pando/data).
	DataDir() string
	// Debug reports whether the host runs in debug mode.
	Debug() bool
	// Lookup resolves a dotted configuration path to a value, for the rare
	// case where an extension must read a core setting. Returns false when the
	// path is unknown. Implementations return copies, never live pointers.
	Lookup(path string) (any, bool)
	// LockedKeys lists the configuration paths currently locked by an overlay
	// (see ConfigOverlay.Locked), sorted and deduplicated. It is the state a
	// panel or a settings surface reads to render a key as managed rather than
	// editable. Empty when no overlay locks anything.
	LockedKeys() []string
}

// Lifecycle is an optional interface for extensions that run background work.
// The manager calls Start after all extensions are loaded, and Stop during
// shutdown before Cleanup. Both must return promptly; long work belongs in a
// goroutine the extension owns.
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
