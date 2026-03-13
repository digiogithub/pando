package api

import (
	"net/http"

	"github.com/digiogithub/pando/internal/llm/agent"
)

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	tools := agent.GetMcpTools(r.Context(), s.app.Permissions)

	toolsList := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		toolsList = append(toolsList, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  t.InputSchema,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tools": toolsList,
	})
}
