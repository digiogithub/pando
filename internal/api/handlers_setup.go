package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/ollamasetup"
)

// setupOllama tracks the install/pull jobs started by the first-run setup
// assistant. One manager per process is enough: jobs are keyed by id.
var setupOllama = ollamasetup.NewManager()

// setupOllamaBaseURL is the native Ollama URL Remembrances will use: the
// base URL of the first enabled Ollama provider account, then OLLAMA_BASE_URL,
// then http://localhost:11434.
func setupOllamaBaseURL() string {
	configured := ""
	if cfg := config.Get(); cfg != nil {
		for _, account := range cfg.ProviderAccounts {
			if account.Type == models.ProviderOllama && !account.Disabled && strings.TrimSpace(account.BaseURL) != "" {
				configured = account.BaseURL
				break
			}
		}
	}
	return models.ResolveOllamaRawBaseURL(configured)
}

type setupOllamaStatusResponse struct {
	ollamasetup.Status
	DocumentModel         string `json:"documentModel"`
	CodeModel             string `json:"codeModel"`
	HasDocumentModel      bool   `json:"hasDocumentModel"`
	HasCodeModel          bool   `json:"hasCodeModel"`
	DockerCommand         string `json:"dockerCommand"`
	RemembrancesEnabled   bool   `json:"remembrancesEnabled"`
	RemembrancesDocModel  string `json:"remembrancesDocumentModel,omitempty"`
	RemembrancesCodeModel string `json:"remembrancesCodeModel,omitempty"`
}

// handleSetupStatus handles GET /api/v1/setup/status.
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	status, err := config.GetSetupStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleSetupScope handles POST /api/v1/setup/scope {scope: "global"|"project"}.
// It creates the configuration file for the chosen scope so every setting the
// assistant saves afterwards lands in it.
func (s *Server) handleSetupScope(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	path, err := config.PrepareSetupScope(strings.TrimSpace(req.Scope))
	if err != nil {
		writeConfigError(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	status, _ := config.GetSetupStatus()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"scope":      req.Scope,
		"configPath": path,
		"status":     status,
	})
}

// handleSetupSuggestModels handles GET /api/v1/setup/suggested-models?provider=.
// It refreshes the dynamic model registry first so a provider that was just
// authenticated (Copilot device flow) reports its models.
func (s *Server) handleSetupSuggestModels(w http.ResponseWriter, r *http.Request) {
	provider := models.ModelProvider(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if r.URL.Query().Get("refresh") == "1" {
		refreshDynamicModelsAfterAccountChange()
	}
	mainModel, fastModel := config.SuggestSetupModels(provider)
	writeJSON(w, http.StatusOK, map[string]string{
		"provider":  string(provider),
		"mainModel": mainModel,
		"fastModel": fastModel,
	})
}

// handleSetupModels handles POST /api/v1/setup/models {mainModel, fastModel}.
// The main model goes to the coder agent (rebuilding the live agent's
// provider), the fast model to every secondary agent.
func (s *Server) handleSetupModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MainModel string `json:"mainModel"`
		FastModel string `json:"fastModel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	mainID := strings.TrimSpace(req.MainModel)
	if mainID == "" {
		writeError(w, http.StatusBadRequest, "mainModel is required")
		return
	}
	fastID := strings.TrimSpace(req.FastModel)
	if fastID == "" {
		fastID = mainID
	}
	if resolved, ok := models.ResolveModelID(models.ModelID(mainID)); ok {
		mainID = string(resolved)
	}
	if resolved, ok := models.ResolveModelID(models.ModelID(fastID)); ok {
		fastID = string(resolved)
	}

	if err := s.setCoderModel(models.ModelID(mainID)); err != nil {
		writeConfigError(w, http.StatusBadRequest, "main model: "+err.Error(), err)
		return
	}
	for _, name := range config.SecondaryAgentNames() {
		if err := config.UpdateAgentModel(name, models.ModelID(fastID)); err != nil {
			writeConfigError(w, http.StatusBadRequest, "fast model ("+string(name)+"): "+err.Error(), err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"mainModel": mainID, "fastModel": fastID})
}

// handleSetupOllamaStatus handles GET /api/v1/setup/ollama/status.
func (s *Server) handleSetupOllamaStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	st := setupOllama.Detect(ctx, setupOllamaBaseURL())
	resp := setupOllamaStatusResponse{
		Status:        st,
		DocumentModel: config.DefaultSetupDocumentEmbeddingModel,
		CodeModel:     config.DefaultSetupCodeEmbeddingModel,
		DockerCommand: ollamasetup.DockerInstallCmd,
	}
	resp.HasDocumentModel = ollamasetup.HasModel(st.Models, resp.DocumentModel)
	resp.HasCodeModel = ollamasetup.HasModel(st.Models, resp.CodeModel)
	if cfg := config.Get(); cfg != nil {
		resp.RemembrancesEnabled = cfg.Remembrances.Enabled
		resp.RemembrancesDocModel = cfg.Remembrances.DocumentEmbeddingModel
		resp.RemembrancesCodeModel = cfg.Remembrances.CodeEmbeddingModel
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSetupOllamaInstall handles POST /api/v1/setup/ollama/install {option}.
// Only fixed, server-side command lines run; the client picks one by id.
func (s *Server) handleSetupOllamaInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Option string `json:"option"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	job, err := setupOllama.Install(strings.TrimSpace(req.Option))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

// handleSetupOllamaStart handles POST /api/v1/setup/ollama/start.
func (s *Server) handleSetupOllamaStart(w http.ResponseWriter, r *http.Request) {
	baseURL := setupOllamaBaseURL()
	if err := setupOllama.Start(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	running := setupOllama.WaitRunning(ctx, baseURL)
	writeJSON(w, http.StatusOK, map[string]interface{}{"running": running, "baseUrl": baseURL})
}

// handleSetupOllamaPull handles POST /api/v1/setup/ollama/pull {model}.
func (s *Server) handleSetupOllamaPull(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	job, err := setupOllama.Pull(setupOllamaBaseURL(), req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

// handleSetupJob handles GET /api/v1/setup/jobs/{id}.
func (s *Server) handleSetupJob(w http.ResponseWriter, r *http.Request) {
	job, ok := setupOllama.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleSetupRemembrances handles POST /api/v1/setup/remembrances
// {documentModel, codeModel}: enables Remembrances on local Ollama models.
func (s *Server) handleSetupRemembrances(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocumentModel string `json:"documentModel"`
		CodeModel     string `json:"codeModel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := config.ApplySetupRemembrances(req.DocumentModel, req.CodeModel); err != nil {
		writeConfigError(w, http.StatusBadRequest, err.Error(), err)
		return
	}
	cfg := config.Get()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":       cfg.Remembrances.Enabled,
		"documentModel": cfg.Remembrances.DocumentEmbeddingModel,
		"codeModel":     cfg.Remembrances.CodeEmbeddingModel,
	})
}

// handleSetupComplete handles POST /api/v1/setup/complete: the assistant was
// finished, so it stops opening by itself.
func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if err := config.MarkSetupCompleted(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, _ := config.GetSetupStatus()
	writeJSON(w, http.StatusOK, status)
}
