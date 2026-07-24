package app

import (
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/lsp"
	"github.com/digiogithub/pando/internal/lsp/runtime"
)

// LSP runtime states reported to the surfaces.
const (
	lspRunStateStopped  = "stopped"
	lspRunStateStarting = "starting"
	lspRunStateReady    = "ready"
	lspRunStateError    = "error"
)

// LSPServerStatus is what a settings page, the API or the setup tool needs to
// describe one language server: how it is configured, whether its binary can be
// obtained, and what it is doing right now.
type LSPServerStatus struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Command is the configured command, as it should be written back to the
	// configuration; ResolvedCommand is the executable Pando would actually
	// spawn (an absolute path, possibly inside Pando's own staging directory).
	Command         string   `json:"command"`
	ResolvedCommand string   `json:"resolvedCommand,omitempty"`
	Args            []string `json:"args,omitempty"`
	Languages       []string `json:"languages,omitempty"`
	Filenames       []string `json:"filenames,omitempty"`

	// Configured reports that the user declared this server under [LSP.<name>]
	// instead of only inheriting the built-in preset.
	Configured bool `json:"configured"`
	// OptIn reports a preset that stays inactive until it is declared.
	OptIn     bool `json:"optIn"`
	Disabled  bool `json:"disabled"`
	Autostart bool `json:"autostart"`

	// Availability is "installed", "installable" or "manual".
	Availability string `json:"availability"`
	// AvailabilityLabel is the short human-readable form, e.g.
	// "installable (bun)".
	AvailabilityLabel string `json:"availabilityLabel"`
	// Reason explains a non-installed server; Hint is the command the user can
	// run themselves and URL its documentation.
	Reason string `json:"reason,omitempty"`
	Hint   string `json:"hint,omitempty"`
	URL    string `json:"url,omitempty"`

	// RunState is "stopped", "starting", "ready" or "error".
	RunState   string `json:"runState"`
	Installing bool   `json:"installing"`
}

// LSPActivationSettings returns the global on-demand knobs, so a surface can
// render them next to the per-server statuses.
func (app *App) LSPActivationSettings() config.LSPActivationSettings {
	return config.Get().LSPActivationSettings()
}

// LSPServerStatuses describes every server in the registry: the presets plus
// anything the user configured. It is the single source of truth behind the TUI
// settings page, the REST config API and the pando_setup tool, so all three
// agree on what "installed" means.
//
// Resolution is read-only: it never installs anything and never starts a
// server; a server that is only installable is reported as such.
func (app *App) LSPServerStatuses() []LSPServerStatus {
	cfg := config.Get()
	registry := cfg.LSPRegistry()

	// Snapshot the runtime state once instead of locking per server.
	app.clientsMutex.RLock()
	running := make(map[string]*lsp.Client, len(app.LSPClients))
	for name, c := range app.LSPClients {
		running[name] = c
	}
	spawning := make(map[string]struct{}, len(app.lspSpawning))
	for name := range app.lspSpawning {
		spawning[name] = struct{}{}
	}
	installing := make(map[string]struct{}, len(app.lspInstalling))
	for name := range app.lspInstalling {
		installing[name] = struct{}{}
	}
	unavailable := make(map[string]lspUnavailableEntry, len(app.lspUnavailable))
	for name, entry := range app.lspUnavailable {
		unavailable[name] = entry
	}
	app.clientsMutex.RUnlock()

	out := make([]LSPServerStatus, 0, len(registry))
	for _, s := range registry {
		_, configured := cfg.LSP[s.Name]
		st := LSPServerStatus{
			Name:        s.Name,
			Description: s.Description,
			Command:     s.Command,
			Args:        s.Args,
			Languages:   s.Languages,
			Filenames:   s.Filenames,
			Configured:  configured,
			OptIn:       s.OptIn && !configured,
			// An opt-in preset is not "disabled by the user": report it as
			// opt-in only, so the surfaces can offer to enable it.
			Disabled:  s.Disabled && !(s.OptIn && !configured),
			Autostart: s.Autostart,
			Hint:      s.Install.Hint,
			URL:       s.Install.URL,
			RunState:  lspRunStateStopped,
		}

		res := app.resolveLSPCommand(s)
		switch res.Availability {
		case LSPAvailable:
			st.Availability = "installed"
			st.AvailabilityLabel = "installed"
			st.ResolvedCommand = res.Command
		case LSPInstallable:
			st.Availability = "installable"
			st.AvailabilityLabel = "installable (" + string(runtime.Detect().Manager) + ")"
			st.Reason = res.Reason
		default:
			st.Availability = "manual"
			st.AvailabilityLabel = "not installed"
			st.Reason = res.Reason
			if st.Hint == "" {
				st.Hint = installHint(s)
			}
		}
		// A recorded failure is more specific than a fresh resolution.
		if entry, ok := unavailable[s.Name]; ok && entry.Reason != "" {
			st.Reason = entry.Reason
		}

		if _, ok := installing[s.Name]; ok {
			st.Installing = true
		}
		switch client, ok := running[s.Name]; {
		case ok && client.GetServerState() == lsp.StateReady:
			st.RunState = lspRunStateReady
		case ok && client.GetServerState() == lsp.StateError:
			st.RunState = lspRunStateError
		case ok:
			st.RunState = lspRunStateStarting
		default:
			if _, starting := spawning[s.Name]; starting {
				st.RunState = lspRunStateStarting
			}
		}

		out = append(out, st)
	}

	sort.SliceStable(out, func(i, j int) bool {
		// Configured servers first, then the rest of the catalogue by name.
		if out[i].Configured != out[j].Configured {
			return out[i].Configured
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// LSPCatalog implements tools.SetupLSPCatalog: it is the same report as
// LSPServerStatuses, in the shape the pando_setup tool consumes.
func (app *App) LSPCatalog() []tools.LSPCatalogEntry {
	statuses := app.LSPServerStatuses()
	out := make([]tools.LSPCatalogEntry, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, tools.LSPCatalogEntry{
			Name:              st.Name,
			Description:       st.Description,
			Command:           st.Command,
			Languages:         st.Languages,
			Filenames:         st.Filenames,
			Configured:        st.Configured,
			OptIn:             st.OptIn,
			Disabled:          st.Disabled,
			Installing:        st.Installing,
			Availability:      st.Availability,
			AvailabilityLabel: st.AvailabilityLabel,
			Reason:            st.Reason,
			Hint:              st.Hint,
			RunState:          st.RunState,
		})
	}
	return out
}

// LSPServerStatusByName returns the status of a single server.
func (app *App) LSPServerStatusByName(name string) (LSPServerStatus, bool) {
	for _, st := range app.LSPServerStatuses() {
		if st.Name == name {
			return st, true
		}
	}
	return LSPServerStatus{}, false
}
