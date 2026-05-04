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
	mux.HandleFunc("/api/v1/files/raw/", s.handleRawFile)
	mux.HandleFunc("/api/v1/files/", s.handleFileByPath)
	mux.HandleFunc("GET /api/v1/fs/browse", s.handleFSBrowse)
	mux.HandleFunc("/api/v1/chat", s.handleChat)
	mux.HandleFunc("/api/v1/chat/stream", s.handleChatStream)
	// Settings
	mux.HandleFunc("/api/v1/settings", s.handleSettings)
	mux.HandleFunc("/api/v1/settings/providers", s.handleGetProviders)
	// Container runtime
	mux.HandleFunc("GET /api/v1/container/capabilities", s.handleContainerCapabilities)
	mux.HandleFunc("GET /api/container/capabilities", s.handleContainerCapabilities)
	mux.HandleFunc("/api/v1/container/config", s.handleContainerConfig)
	mux.HandleFunc("/api/container/config", s.handleContainerConfig)
	mux.HandleFunc("GET /api/v1/container/sessions", s.handleContainerSessions)
	mux.HandleFunc("GET /api/container/sessions", s.handleContainerSessions)
	mux.HandleFunc("POST /api/v1/container/sessions/{sessionId}/stop", s.handleStopContainerSession)
	mux.HandleFunc("POST /api/container/sessions/{sessionId}/stop", s.handleStopContainerSession)
	mux.HandleFunc("GET /api/v1/container/events", s.handleContainerEvents)
	mux.HandleFunc("GET /api/container/events", s.handleContainerEvents)
	mux.HandleFunc("GET /api/v1/container/images", s.handleContainerImages)
	mux.HandleFunc("GET /api/container/images", s.handleContainerImages)
	mux.HandleFunc("DELETE /api/v1/container/images/{ref...}", s.handleDeleteContainerImage)
	mux.HandleFunc("DELETE /api/container/images/{ref...}", s.handleDeleteContainerImage)
	mux.HandleFunc("POST /api/v1/container/images/gc", s.handleContainerImageGC)
	mux.HandleFunc("POST /api/container/images/gc", s.handleContainerImageGC)
	// Config hot-reload events (SSE)
	mux.HandleFunc("GET /api/v1/config/events", s.handleConfigEvents)
	// User-facing notifications (SSE): LLM retries, tool errors, LSP diagnostics
	mux.HandleFunc("GET /api/v1/notifications/stream", s.handleNotificationsStream)
	// Config sections
	mux.HandleFunc("/api/v1/config/providers", s.handleConfigProviders)
	mux.HandleFunc("/api/v1/config/agents", s.handleConfigAgents)
	mux.HandleFunc("/api/v1/config/mcp-servers", s.handleConfigMCPServers)
	mux.HandleFunc("DELETE /api/v1/config/mcp-servers/{name}", s.handleDeleteConfigMCPServer)
	mux.HandleFunc("POST /api/v1/config/mcp-servers/{name}/reload", s.handleReloadMCPServer)
	mux.HandleFunc("/api/v1/config/mcp-gateway", s.handleConfigMCPGateway)
	mux.HandleFunc("/api/v1/config/mcp-server", s.handleConfigMCPServer)
	mux.HandleFunc("/api/v1/config/lsp", s.handleConfigLSP)
	mux.HandleFunc("DELETE /api/v1/config/lsp/{language}", s.handleDeleteConfigLSP)
	mux.HandleFunc("/api/v1/config/tools", s.handleConfigTools)
	mux.HandleFunc("/api/v1/config/openlit", s.handleConfigOpenLit)
	mux.HandleFunc("/api/v1/config/bash", s.handleConfigBash)
	mux.HandleFunc("/api/v1/config/extensions", s.handleConfigExtensions)
	mux.HandleFunc("/api/v1/config/services", s.handleConfigServices)
	mux.HandleFunc("/api/v1/config/evaluator", s.handleConfigEvaluator)
	// Provider Accounts
	mux.HandleFunc("GET /api/v1/config/provider-accounts", s.handleListProviderAccounts)
	mux.HandleFunc("POST /api/v1/config/provider-accounts", s.handleCreateProviderAccount)
	mux.HandleFunc("GET /api/v1/config/provider-types", s.handleListProviderTypes)
	mux.HandleFunc("GET /api/v1/config/provider-accounts/{id}", s.handleGetProviderAccount)
	mux.HandleFunc("PUT /api/v1/config/provider-accounts/{id}", s.handleUpdateProviderAccount)
	mux.HandleFunc("DELETE /api/v1/config/provider-accounts/{id}", s.handleDeleteProviderAccount)
	mux.HandleFunc("POST /api/v1/config/provider-accounts/{id}/test", s.handleTestProviderAccount)
	mux.HandleFunc("POST /api/v1/config/api-server/regenerate-token", s.handleRegenerateAPIToken)
	// First-run / config generation
	mux.HandleFunc("GET /api/v1/config/init-status", s.handleConfigInitStatus)
	mux.HandleFunc("POST /api/v1/config/generate", s.handleConfigGenerate)
	// Remembrances
	mux.HandleFunc("GET /api/v1/remembrances/projects", s.handleListCodeProjects)
	mux.HandleFunc("POST /api/v1/remembrances/projects/index", s.handleIndexCodeProject)
	// Skills
	mux.HandleFunc("GET /api/v1/skills/installed", s.handleListInstalledSkills)
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
	// Personas
	mux.HandleFunc("GET /api/v1/personas", s.handleListPersonas)
	mux.HandleFunc("GET /api/v1/personas/active", s.handleGetActivePersona)
	mux.HandleFunc("PUT /api/v1/personas/active", s.handleSetActivePersona)
	// CronJobs
	mux.HandleFunc("GET /api/v1/cronjobs", s.handleListCronJobs)
	mux.HandleFunc("POST /api/v1/cronjobs", s.handleCreateCronJob)
	mux.HandleFunc("PUT /api/v1/cronjobs/{name}", s.handleCronJobByName)
	mux.HandleFunc("DELETE /api/v1/cronjobs/{name}", s.handleCronJobByName)
	mux.HandleFunc("POST /api/v1/cronjobs/{name}/run", s.handleRunCronJobNow)
	// Projects — specific paths registered before {id} wildcard (Go 1.22 precedence)
	mux.HandleFunc("GET /api/v1/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/v1/projects", s.handleCreateProject)
	mux.HandleFunc("GET /api/v1/projects/active", s.handleGetActiveProject)
	mux.HandleFunc("GET /api/v1/projects/events", s.handleProjectEvents)
	mux.HandleFunc("GET /api/v1/projects/{id}", s.handleGetProject)
	mux.HandleFunc("DELETE /api/v1/projects/{id}", s.handleDeleteProject)
	mux.HandleFunc("PATCH /api/v1/projects/{id}", s.handleRenameProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/activate", s.handleActivateProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/deactivate", s.handleDeactivateProject)
	mux.HandleFunc("POST /api/v1/projects/{id}/init", s.handleInitProject)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
