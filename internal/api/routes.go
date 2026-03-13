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
	mux.HandleFunc("/api/v1/files/", s.handleFileByPath)
	mux.HandleFunc("/api/v1/chat", s.handleChat)
	mux.HandleFunc("/api/v1/chat/stream", s.handleChatStream)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
