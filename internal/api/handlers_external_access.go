package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// externalBindHost is the address the listener moves to when external access is
// turned on: every interface, so the same instance can be reached from other
// machines while the local UI keeps working.
const externalBindHost = "0.0.0.0"

// ExternalAccessStatus describes whether the running server is reachable from
// outside this machine, and whether the toggle may be flipped.
type ExternalAccessStatus struct {
	// Enabled reports that the listener is bound to a non-loopback address.
	Enabled bool `json:"enabled"`
	// BindHost is the address currently bound.
	BindHost string `json:"bindHost"`
	// Port is the port the listener uses; it never changes when toggling.
	Port int `json:"port"`
	// CanToggle is false when the startup mode owns no HTTP surface, or when the
	// process was started already exposed (e.g. `pando serve --host 0.0.0.0`):
	// the bind is then the operator's choice and the UI must not take it away.
	CanToggle bool `json:"canToggle"`
	// BasicAuthReady reports that credentials exist and are enabled, which is
	// required before the agent may be exposed to the network.
	BasicAuthReady bool `json:"basicAuthReady"`
	// URLs lists the reachable addresses of this instance, for sharing.
	URLs []string `json:"urls"`
}

// externalAccessStatus builds the current status payload.
func (s *Server) externalAccessStatus() ExternalAccessStatus {
	host := s.BindHost()
	status := ExternalAccessStatus{
		Enabled:   !isLoopbackHost(host),
		BindHost:  host,
		Port:      s.config.Port,
		CanToggle: isLoopbackHost(s.InitialHost()) && externalAccessAllowedForMode(s.config.StartupMode),
		URLs:      []string{},
	}

	if cfg := config.Get(); cfg != nil {
		status.BasicAuthReady = cfg.Server.BasicAuth.Enabled && len(cfg.Server.BasicAuth.Users) > 0
	}
	if status.Enabled {
		status.URLs = s.reachableURLs()
	}
	return status
}

// reachableURLs lists the LAN addresses the Web UI answers on, so the user can
// copy one into another device. Loopback and link-local addresses are skipped:
// they are useless to a second machine.
func (s *Server) reachableURLs() []string {
	scheme := "http"
	if s.IsTLS() {
		scheme = "https"
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logging.Debug("External access: failed to list interfaces", "error", err)
		return []string{}
	}

	urls := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			continue
		}
		host := ip.String()
		if ip.To4() == nil {
			host = "[" + host + "]"
		}
		urls = append(urls, fmt.Sprintf("%s://%s:%d", scheme, host, s.config.Port))
	}
	return urls
}

// handleExternalAccess handles GET and PUT /api/v1/config/api-server/external-access.
//
// Enabling rebinds the live listener to 0.0.0.0 so the instance can be used from
// several places at once. It is refused unless basic auth is enabled with at
// least one user: the API runs shell commands and writes files, so an unguarded
// network bind is remote code execution for the whole LAN.
//
// The change is deliberately not persisted to the config file — the next start
// is local again unless the user configures Server.Host explicitly.
func (s *Server) handleExternalAccess(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.externalAccessStatus())
	case http.MethodPut:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		status := s.externalAccessStatus()
		if status.Enabled == req.Enabled {
			writeJSON(w, http.StatusOK, status)
			return
		}
		if !status.CanToggle {
			writeError(w, http.StatusConflict, "external access is fixed by the --host flag this instance was started with")
			return
		}
		if req.Enabled && !status.BasicAuthReady {
			writeError(w, http.StatusBadRequest, "basic_auth_required_for_external_access")
			return
		}

		target := s.InitialHost()
		if req.Enabled {
			target = externalBindHost
		}
		if err := s.Rebind(target); err != nil {
			logging.Error("External access: rebind failed", "host", target, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to rebind server: "+err.Error())
			return
		}
		logging.Info("External access changed", "host", target, "enabled", req.Enabled)

		writeJSON(w, http.StatusOK, s.externalAccessStatus())
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// externalAccessAllowedForMode reports whether the toggle is meaningful for the
// startup mode. Kept separate so future surfaces can reuse it.
func externalAccessAllowedForMode(mode string) bool {
	switch strings.ToLower(mode) {
	case "app", "desktop", "serve", "":
		return true
	default:
		return false
	}
}
