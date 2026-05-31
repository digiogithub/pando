package api

import (
	"encoding/json"
	"net/http"

	"github.com/digiogithub/pando/internal/commands"
	"github.com/digiogithub/pando/internal/config"
)

func (s *Server) handleGetCommands(w http.ResponseWriter, r *http.Request) {
	dataDir := ""
	if cfg := config.Get(); cfg != nil {
		dataDir = cfg.Data.Directory
	}
	cmds := commands.AllCommands(dataDir)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cmds)
}
