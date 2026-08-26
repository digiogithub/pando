package api

import (
	"net/http"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extensions"
	"github.com/digiogithub/pando/pkg/extension"
)

// UIManifestResponse is what the WebUI shell fetches at boot to discover the
// panels this build contributes.
//
// Not to be confused with /api/v1/config/extensions, which is the settings
// screen for skills and Lua hooks. This one describes compiled-in extension
// modules (pkg/extension).
type UIManifestResponse struct {
	// Panels is never nil in the response, so the shell can iterate without a
	// null check on the standard build, where it is simply empty.
	Panels []extensions.Panel `json:"panels"`
}

func (s *Server) handleExtensionsUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	panels := []extensions.Panel{}
	if s.app != nil {
		panels = append(panels, extensions.Panels(s.app.Extensions)...)
	}
	writeJSON(w, http.StatusOK, UIManifestResponse{Panels: panels})
}

// handleExtensionsMemory reports the state of the memory capability: whether
// remembrance writes leave this machine, where to, and how much has gone.
//
// It answers on every build, standard included, with Enabled false. The
// alternative — 404 on a build without the capability — would make the UI
// treat "no such feature" and "feature off" as the same unknown, and the one
// thing this indicator must never do is fail open into silence.
func (s *Server) handleExtensionsMemory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var status extensions.MemoryStatus
	if s.app != nil {
		cfg := config.Get()
		var memCfg config.ExtensionsMemoryConfig
		if cfg != nil {
			memCfg = cfg.Extensions.Memory
		}
		status = extensions.MemoryStatusOf(s.app.Extensions, memCfg, s.app.MemorySink)
	}
	if status.Sinks == nil {
		status.Sinks = []extensions.MemorySinkStatus{}
	}
	writeJSON(w, http.StatusOK, status)
}

// ExtensionsLicenseResponse reports the licensing state of this build.
//
// Licensed is false in two very different situations — no licensing machinery
// in this build, and licensing present but unhappy — so the two are separate
// fields. A UI that collapsed them would tell an open-source user their
// perfectly valid build is unlicensed.
type ExtensionsLicenseResponse struct {
	// Gated is true when a license provider is compiled into this build. When
	// false, nothing here is gated and the rest of the fields are empty.
	Gated bool `json:"gated"`
	// Status is the provider's report. Absent when Gated is false.
	Status *extension.LicenseStatus `json:"status,omitempty"`
	// Unlicensed lists the extensions the gate refused, with the reason.
	Unlicensed []UnlicensedExtension `json:"unlicensed"`
}

// UnlicensedExtension is one extension the gate refused to load.
type UnlicensedExtension struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

// handleExtensionsLicense answers on every build, like the memory endpoint and
// for the same reason: "this build has no licensing" is an answer the UI needs
// to render, not an error it should have to infer from a 404.
func (s *Server) handleExtensionsLicense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	resp := ExtensionsLicenseResponse{Unlicensed: []UnlicensedExtension{}}
	if s.app != nil && s.app.Extensions != nil {
		if st, ok := s.app.Extensions.LicenseStatus(); ok {
			resp.Gated = true
			resp.Status = &st
		}
		for _, st := range s.app.Extensions.Statuses() {
			if !st.Unlicensed {
				continue
			}
			reason := ""
			if st.Err != nil {
				reason = st.Err.Error()
			}
			resp.Unlicensed = append(resp.Unlicensed, UnlicensedExtension{
				ID:     string(st.Info.ID),
				Name:   st.Info.Name,
				Reason: reason,
			})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
