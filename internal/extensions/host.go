// Package extensions is the host side of the extension system: it adapts
// Pando's internal configuration and services to the public contract declared
// in pkg/extension, and builds the process-wide extension manager.
//
// The split matters: pkg/extension may not import internal packages, because
// out-of-tree modules (the private enterprise module) import it. Everything
// that needs to touch internal/ lives here instead.
package extensions

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/version"
	"github.com/digiogithub/pando/pkg/extension"
)

// configView adapts *config.Config to extension.ConfigView.
type configView struct {
	cfg *config.Config
}

func (v configView) WorkingDir() string {
	if v.cfg == nil {
		return ""
	}
	return v.cfg.WorkingDir
}

func (v configView) DataDir() string {
	if v.cfg == nil {
		return ""
	}
	dir := v.cfg.Data.Directory
	if dir == "" {
		return ""
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(v.cfg.WorkingDir, dir)
}

func (v configView) Debug() bool {
	return v.cfg != nil && v.cfg.Debug
}

// Lookup resolves a dotted path against the JSON representation of the
// configuration, so extensions can read a core setting without this package
// having to enumerate every field. It returns decoded copies, never live
// pointers into the configuration.
func (v configView) Lookup(path string) (any, bool) {
	if v.cfg == nil || path == "" {
		return nil, false
	}
	raw, err := json.Marshal(v.cfg)
	if err != nil {
		return nil, false
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, false
	}

	cur := tree
	for _, seg := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// Options overrides how the manager is built. The zero value is what
// production code uses.
type Options struct {
	// Config is the configuration to read from. Defaults to config.Get().
	Config *config.Config
	// Logger receives extension lifecycle messages. Defaults to the Pando
	// structured logger.
	Logger *slog.Logger
}

// NewManager builds the extension manager from the current configuration
// without loading anything yet.
func NewManager(opts Options) *extension.Manager {
	cfg := opts.Config
	if cfg == nil {
		cfg = config.Get()
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	entries := make(map[string]extension.Entry)
	var disabled []string
	workingDir := ""
	if cfg != nil {
		workingDir = cfg.WorkingDir
		disabled = cfg.Extensions.Disabled
		for id, entry := range cfg.Extensions.ExtensionEntries() {
			entries[id] = extension.Entry{Enabled: entry.Enabled, Config: entry.Config}
		}
	}

	return extension.NewManager(extension.Options{
		Entries:  entries,
		Disabled: disabled,
		Logger:   log,
		Host: extension.HostServices{
			Config:      configView{cfg: cfg},
			WorkingDir:  workingDir,
			CoreVersion: version.Version,
			Variant:     version.Variant,
		},
	})
}

// Load builds the manager and loads every enabled extension.
//
// A failure inside one extension is logged and recorded in its status, never
// returned as a startup error: an optional feature must not stop Pando from
// running. The manager is always usable, even when empty.
func Load(ctx context.Context, opts Options) *extension.Manager {
	mgr := NewManager(opts)
	if err := mgr.Load(ctx); err != nil {
		logging.Warn("Some extensions failed to load", "error", err)
	}
	if n := len(extension.List()); n > 0 {
		logging.Debug("Extension registry", "registered", n)
	}
	return mgr
}
