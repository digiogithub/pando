package llmproxy

import (
	"net/http"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/version"
)

// handleHealth handles GET /health, returning a JSON health status.
func (s *LLMProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	accounts := config.GetProviderAccounts()
	providerCount := 0
	for _, acc := range accounts {
		if !acc.Disabled {
			providerCount++
		}
	}

	modelCount := len(models.SupportedModels)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"version":   version.Version,
		"providers": providerCount,
		"models":    modelCount,
	})
}

// handleInfo handles GET /v1/, returning basic proxy information.
func (s *LLMProxyServer) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":             "pando-llm-proxy",
		"version":          version.Version,
		"openai_compatible": true,
	})
}
