package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/token", s.handleToken)
	mux.HandleFunc("/api/v1/project", s.handleProject)
	mux.HandleFunc("/api/v1/project/context", s.handleProjectContext)
	mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	mux.HandleFunc("/api/v1/sessions/", s.handleSessionByID)
	mux.HandleFunc("/api/v1/tools", s.handleTools)
	mux.HandleFunc("/api/v1/files", s.handleFiles)
	mux.HandleFunc("/api/v1/files/rename", s.handleRenameFile)
	mux.HandleFunc("/api/v1/files/search", s.handleSearchFiles)
	mux.HandleFunc("/api/v1/files/", s.handleFileByPath)
	mux.HandleFunc("/api/v1/chat", s.handleChat)
	mux.HandleFunc("/api/v1/chat/stream", s.handleChatStream)
	// Settings
	mux.HandleFunc("/api/v1/settings", s.handleSettings)
	mux.HandleFunc("/api/v1/settings/providers", s.handleGetProviders)
	// Config hot-reload events (SSE)
	mux.HandleFunc("GET /api/v1/config/events", s.handleConfigEvents)
	// Config sections
	mux.HandleFunc("/api/v1/config/providers", s.handleConfigProviders)
	mux.HandleFunc("/api/v1/config/agents", s.handleConfigAgents)
	mux.HandleFunc("/api/v1/config/mcp-servers", s.handleConfigMCPServers)
	mux.HandleFunc("DELETE /api/v1/config/mcp-servers/{name}", s.handleDeleteConfigMCPServer)
	mux.HandleFunc("POST /api/v1/config/mcp-servers/{name}/reload", s.handleReloadMCPServer)
	mux.HandleFunc("/api/v1/config/mcp-gateway", s.handleConfigMCPGateway)
	mux.HandleFunc("/api/v1/config/lsp", s.handleConfigLSP)
	mux.HandleFunc("DELETE /api/v1/config/lsp/{language}", s.handleDeleteConfigLSP)
	mux.HandleFunc("/api/v1/config/tools", s.handleConfigTools)
	mux.HandleFunc("/api/v1/config/bash", s.handleConfigBash)
	mux.HandleFunc("/api/v1/config/extensions", s.handleConfigExtensions)
	mux.HandleFunc("/api/v1/config/services", s.handleConfigServices)
	mux.HandleFunc("/api/v1/config/evaluator", s.handleConfigEvaluator)
	mux.HandleFunc("POST /api/v1/config/api-server/regenerate-token", s.handleRegenerateAPIToken)
	// Skills catalog (remote install not yet implemented — returns stubs)
	mux.HandleFunc("GET /api/v1/skills/catalog", s.handleSkillsCatalog)
	mux.HandleFunc("POST /api/v1/skills/install", s.handleInstallSkill)
	mux.HandleFunc("DELETE /api/v1/skills/{name}", s.handleUninstallSkill)
	// Logs
	mux.HandleFunc("/api/v1/logs", s.handleGetLogs)
	mux.HandleFunc("/api/v1/logs/stream", s.handleLogsStream)
	// Orchestrator
	mux.HandleFunc("GET /api/v1/orchestrator/tasks", s.handleGetTasks)
	mux.HandleFunc("POST /api/v1/orchestrator/tasks", s.handleCreateTask)
	mux.HandleFunc("GET /api/v1/orchestrator/tasks/{id}", s.handleGetTaskByID)
	mux.HandleFunc("DELETE /api/v1/orchestrator/tasks/{id}", s.handleDeleteTask)
	mux.HandleFunc("POST /api/v1/orchestrator/tasks/{id}/cancel", s.handleCancelTask)
	// Terminal
	mux.HandleFunc("POST /api/v1/terminal/exec", s.handleTerminalExec)
	// Snapshots
	mux.HandleFunc("GET /api/v1/snapshots/count", s.handleSnapshotsCount)
	mux.HandleFunc("GET /api/v1/snapshots", s.handleGetSnapshots)
	mux.HandleFunc("POST /api/v1/snapshots", s.handleCreateSnapshot)
	mux.HandleFunc("GET /api/v1/snapshots/{id}", s.handleGetSnapshotByID)
	mux.HandleFunc("POST /api/v1/snapshots/{id}/apply", s.handleApplySnapshot)
	mux.HandleFunc("POST /api/v1/snapshots/{id}/revert", s.handleRevertSnapshot)
	mux.HandleFunc("DELETE /api/v1/snapshots/{id}", s.handleDeleteSnapshot)
	// Evaluator
	mux.HandleFunc("GET /api/v1/evaluator/metrics", s.handleGetEvaluatorMetrics)
	mux.HandleFunc("GET /api/v1/evaluator/templates", s.handleGetEvaluatorTemplates)
	mux.HandleFunc("GET /api/v1/evaluator/skills", s.handleGetEvaluatorSkills)
	mux.HandleFunc("GET /api/v1/evaluator/sessions", s.handleGetEvaluatorSessions)
	// Models
	mux.HandleFunc("GET /api/v1/models", s.handleListModels)
	mux.HandleFunc("PUT /api/v1/models/active", s.handleSetActiveModel)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
