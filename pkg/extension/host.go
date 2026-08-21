package extension

import (
	"context"
	"log/slog"
)

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
	// Raw is this extension's own configuration subtree, as written under
	// [Extensions.Entries."<id>".Config] in pando.toml. Never nil.
	Raw map[string]any

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
}

// Bool reads a boolean from the extension's own config subtree.
func (h HostServices) Bool(key string, def bool) bool {
	if v, ok := h.Raw[key].(bool); ok {
		return v
	}
	return def
}

// String reads a string from the extension's own config subtree.
func (h HostServices) String(key, def string) string {
	if v, ok := h.Raw[key].(string); ok && v != "" {
		return v
	}
	return def
}

// Int reads an integer from the extension's own config subtree. TOML decoding
// yields int64 and JSON yields float64, so both are accepted.
func (h HostServices) Int(key string, def int) int {
	switch v := h.Raw[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return def
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
}

// Lifecycle is an optional interface for extensions that run background work.
// The manager calls Start after all extensions are loaded, and Stop during
// shutdown before Cleanup. Both must return promptly; long work belongs in a
// goroutine the extension owns.
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
