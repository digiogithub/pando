package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// maskedPassword is what the API returns in place of a stored basic-auth
// password. The plaintext is only ever handed back by the reveal endpoint.
const maskedPassword = "••••••••"

// maskBasicAuthPasswords returns a copy of the server config with every
// basic-auth password replaced by maskedPassword.
func maskBasicAuthPasswords(server config.APIServerConfig) config.APIServerConfig {
	if len(server.BasicAuth.Users) == 0 {
		return server
	}
	masked := server
	masked.BasicAuth.Users = make([]config.BasicAuthUser, len(server.BasicAuth.Users))
	for i, user := range server.BasicAuth.Users {
		masked.BasicAuth.Users[i] = config.BasicAuthUser{Username: user.Username, Password: maskedPassword}
	}
	return masked
}

// preserveBasicAuth keeps the stored credentials when the generic services form
// saves the server section. That payload carries masked passwords (and older
// clients omit the block entirely), so writing it back would wipe every user.
// Credentials are only ever changed through the basic-auth endpoints below.
func preserveBasicAuth(incoming, existing config.APIServerConfig) config.APIServerConfig {
	incoming.BasicAuth = existing.BasicAuth
	return incoming
}

// BasicAuthUserResponse describes a configured credential without its password.
type BasicAuthUserResponse struct {
	Username string `json:"username"`
}

// BasicAuthStatusResponse is the payload of the WebUI Access panel.
type BasicAuthStatusResponse struct {
	Enabled bool                    `json:"enabled"`
	Users   []BasicAuthUserResponse `json:"users"`
	// Enforced reports whether credentials are actually being demanded right now.
	// Basic auth is inert while the server is bound to a loopback address.
	Enforced bool `json:"enforced"`
	// BindHost is the address the server is listening on, so the panel can explain
	// why the setting is inert.
	BindHost string `json:"bindHost"`
}

func (s *Server) basicAuthStatus() BasicAuthStatusResponse {
	cfg := config.Get()
	resp := BasicAuthStatusResponse{Users: []BasicAuthUserResponse{}, BindHost: s.config.Host}
	if cfg == nil {
		return resp
	}
	resp.Enabled = cfg.Server.BasicAuth.Enabled
	for _, user := range cfg.Server.BasicAuth.Users {
		resp.Users = append(resp.Users, BasicAuthUserResponse{Username: user.Username})
	}
	resp.Enforced = resp.Enabled && len(resp.Users) > 0 && !isLoopbackHost(s.config.Host)
	return resp
}

// updateBasicAuth mutates the stored basic-auth config through config.UpdateServer.
func updateBasicAuth(mutate func(*config.BasicAuthConfig)) error {
	cfg := config.Get()
	server := cfg.Server
	users := make([]config.BasicAuthUser, len(server.BasicAuth.Users))
	copy(users, server.BasicAuth.Users)
	server.BasicAuth.Users = users
	mutate(&server.BasicAuth)
	return config.UpdateServer(server)
}

// handleConfigBasicAuth handles GET and PUT /api/v1/config/api-server/basic-auth.
func (s *Server) handleConfigBasicAuth(w http.ResponseWriter, r *http.Request) {
	if config.Get() == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.basicAuthStatus())
	case http.MethodPut:
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Enabled && len(config.Get().Server.BasicAuth.Users) == 0 {
			writeError(w, http.StatusBadRequest, "add at least one user before enabling basic auth")
			return
		}
		if err := updateBasicAuth(func(ba *config.BasicAuthConfig) { ba.Enabled = req.Enabled }); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update basic auth: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.basicAuthStatus())
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleCreateBasicAuthUser handles POST /api/v1/config/api-server/basic-auth/users.
// It upserts by username.
func (s *Server) handleCreateBasicAuthUser(w http.ResponseWriter, r *http.Request) {
	if config.Get() == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "password is required")
		return
	}
	if strings.Contains(req.Username, ":") {
		writeError(w, http.StatusBadRequest, "username cannot contain ':'")
		return
	}

	err := updateBasicAuth(func(ba *config.BasicAuthConfig) {
		for i, user := range ba.Users {
			if user.Username == req.Username {
				ba.Users[i].Password = req.Password
				return
			}
		}
		ba.Users = append(ba.Users, config.BasicAuthUser{Username: req.Username, Password: req.Password})
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save user: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.basicAuthStatus())
}

// handleDeleteBasicAuthUser handles DELETE /api/v1/config/api-server/basic-auth/users/{username}.
// Removing the last user also disables basic auth, so the config can never demand
// credentials that do not exist.
func (s *Server) handleDeleteBasicAuthUser(w http.ResponseWriter, r *http.Request) {
	if config.Get() == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	username := r.PathValue("username")
	found := false
	for _, user := range config.Get().Server.BasicAuth.Users {
		if user.Username == username {
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	err := updateBasicAuth(func(ba *config.BasicAuthConfig) {
		remaining := make([]config.BasicAuthUser, 0, len(ba.Users))
		for _, user := range ba.Users {
			if user.Username != username {
				remaining = append(remaining, user)
			}
		}
		ba.Users = remaining
		if len(remaining) == 0 {
			ba.Enabled = false
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete user: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.basicAuthStatus())
}

// handleRevealBasicAuthUser handles POST /api/v1/config/api-server/basic-auth/users/{username}/reveal.
// Passwords are stored age-encrypted and are decrypted in memory at load time, so
// this returns the plaintext to an already-authenticated caller.
func (s *Server) handleRevealBasicAuthUser(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	username := r.PathValue("username")
	for _, user := range cfg.Server.BasicAuth.Users {
		if user.Username == username {
			writeJSON(w, http.StatusOK, map[string]string{"username": user.Username, "password": user.Password})
			return
		}
	}
	writeError(w, http.StatusNotFound, "user not found")
}
