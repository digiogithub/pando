package api

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/sandbox"
)

// SandboxStatusResponse is the badge view of the live sandbox state.
type SandboxStatusResponse struct {
	// Label is sandbox.Status.Label(), e.g. "workspace-write (landlock+seccomp v5)".
	Label string `json:"label"`
	// Active: the policy is enabled AND the backend enforces it here.
	Active bool `json:"active"`
	// Full: Active with no gaps (sandbox.Guarantees). A partial sandbox
	// keeps the bash permission prompt; Gaps says why.
	Full bool     `json:"full"`
	Gaps []string `json:"gaps"`
	// Enabled: the policy is on, whether or not the OS can enforce it.
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`
	Network string `json:"network"`
	// Source is where the mode came from: default, config, env or lock.
	Source string `json:"source"`
	Hash   string `json:"hash"`
}

// SandboxPolicyResponse is the resolved policy, read-only, so a settings
// surface can show what is actually writable and protected.
type SandboxPolicyResponse struct {
	Workspace      string   `json:"workspace"`
	WritableRoots  []string `json:"writableRoots"`
	ReadableRoots  []string `json:"readableRoots"`
	ProtectedPaths []string `json:"protectedPaths"`
	DenyPaths      []string `json:"denyPaths"`
	ExtendTo       []string `json:"extendTo"`
	AutoAllowBash  bool     `json:"autoAllowBash"`
	UseBwrap       string   `json:"useBwrap"`
	// GuardedPorts are Pando's own TCP ports confined commands cannot
	// connect to (Policy.DenyConnectPorts).
	GuardedPorts []int `json:"guardedPorts"`
}

// SandboxConfigResponse is the body of GET/PUT /api/v1/config/sandbox.
type SandboxConfigResponse struct {
	// Config is the editable section: the global file's values with every
	// locked field showing its enforced value (config.SandboxSettingsView).
	// A PUT sends this shape back.
	Config config.SandboxConfig `json:"config"`
	// Effective is the in-memory section after project-local tightening and
	// overlays: what Resolve reads.
	Effective  config.SandboxConfig  `json:"effective"`
	Capability sandbox.Capability    `json:"capability"`
	Status     SandboxStatusResponse `json:"status"`
	Policy     SandboxPolicyResponse `json:"policy"`
	// Locked lists the locked sandbox field paths ("sandbox.mode", ...).
	Locked []string `json:"locked"`
	// Platform is runtime.GOOS, so the UI can scope Linux-only knobs.
	Platform string `json:"platform"`
	// EnvOverride is the PANDO_SANDBOX value when set: it overrides the
	// configured mode for this process (unless the mode is locked).
	EnvOverride string `json:"envOverride,omitempty"`
}

func buildSandboxConfigResponse() SandboxConfigResponse {
	status := sandbox.CurrentStatus()
	p := status.Policy
	extend := make([]string, 0, len(p.ExtendTo))
	for _, purpose := range p.ExtendTo {
		extend = append(extend, string(purpose))
	}
	resp := SandboxConfigResponse{
		Config:     config.SandboxSettingsView(),
		Capability: status.Capability,
		Status: SandboxStatusResponse{
			Label:   status.Label(),
			Active:  status.Active,
			Full:    status.Full,
			Gaps:    nonNilStrings(status.Gaps),
			Enabled: p.Enabled(),
			Mode:    string(p.Mode),
			Network: string(p.Network),
			Source:  string(p.Source),
			Hash:    status.Hash,
		},
		Policy: SandboxPolicyResponse{
			Workspace:      p.Workspace,
			WritableRoots:  nonNilStrings(p.WritableRoots),
			ReadableRoots:  nonNilStrings(p.ReadableRoots),
			ProtectedPaths: nonNilStrings(p.ProtectedPaths),
			DenyPaths:      nonNilStrings(p.DenyPaths),
			ExtendTo:       extend,
			AutoAllowBash:  p.AutoAllowBash,
			UseBwrap:       string(p.UseBwrap),
			GuardedPorts:   nonNilInts(p.DenyConnectPorts),
		},
		Locked:   config.LockedSandboxFields(),
		Platform: runtime.GOOS,
	}
	if cfg := config.Get(); cfg != nil {
		resp.Effective = cfg.Sandbox
	}
	if v, ok := os.LookupEnv(config.SandboxEnvVar); ok && strings.TrimSpace(v) != "" {
		resp.EnvOverride = strings.TrimSpace(v)
	}
	return resp
}

// handleConfigSandbox reads (GET) or replaces (PUT) the host command sandbox
// settings. A PUT takes the "config" object of the GET response, persists it
// to the GLOBAL config file through config.UpdateSandbox and takes effect on
// the next spawned command (the policy is re-resolved per spawn). An invalid
// value is 400; changing a field an overlay locked is 409 with code
// config_key_locked (the writeConfigError convention), while echoing the
// locked value back unchanged is accepted.
func (s *Server) handleConfigSandbox(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if config.Get() == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, buildSandboxConfigResponse())
	case http.MethodPut:
		if config.Get() == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		var req config.SandboxConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if _, err := config.NormalizeSandboxConfig(req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := config.UpdateSandbox(req); err != nil {
			writeConfigError(w, http.StatusInternalServerError, "failed to update sandbox config: "+err.Error(), err)
			return
		}
		writeJSON(w, http.StatusOK, buildSandboxConfigResponse())
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func nonNilInts(in []int) []int {
	if in == nil {
		return []int{}
	}
	return in
}
