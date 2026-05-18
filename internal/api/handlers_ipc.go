// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"net/http"
	"os"
	"time"

	"github.com/digiogithub/pando/internal/instanceregistry"
)

// ipcInstanceInfo is the JSON representation of a peer instance in the IPC topology.
type ipcInstanceInfo struct {
	InstanceID string    `json:"instance_id"`
	Role       string    `json:"role"`
	PID        int       `json:"pid"`
	PubPort    int       `json:"pub_port"`
	RPCPort    int       `json:"rpc_port"`
	Mode       string    `json:"mode"`
	StartedAt  time.Time `json:"started_at"`
}

// ipcStatusResponse is the JSON body returned by GET /api/v1/ipc/status.
type ipcStatusResponse struct {
	Role       string            `json:"role"`
	Workdir    string            `json:"workdir"`
	InstanceID string            `json:"instance_id"`
	PID        int               `json:"pid"`
	PubPort    int               `json:"pub_port"`
	RPCPort    int               `json:"rpc_port"`
	Instances  []ipcInstanceInfo `json:"instances"`
}

// handleIPCStatus handles GET /api/v1/ipc/status.
// Returns IPC topology information for the current server instance.
func (s *Server) handleIPCStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	reg := instanceregistry.New()
	entries, err := reg.ListByPath(s.config.CWD)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list instances: "+err.Error())
		return
	}

	peers := make([]ipcInstanceInfo, 0, len(entries))
	for _, e := range entries {
		role := "secondary"
		if e.IsPrimary {
			role = "primary"
		}
		peers = append(peers, ipcInstanceInfo{
			InstanceID: e.InstanceID,
			Role:       role,
			PID:        e.PID,
			PubPort:    e.PubPort,
			RPCPort:    e.RPCPort,
			Mode:       string(e.Mode),
			StartedAt:  e.StartedAt,
		})
	}

	resp := ipcStatusResponse{
		Role:       s.config.Role,
		Workdir:    s.config.CWD,
		InstanceID: s.config.InstanceID,
		PID:        os.Getpid(),
		PubPort:    s.config.PubPort,
		RPCPort:    s.config.RPCPort,
		Instances:  peers,
	}

	writeJSON(w, http.StatusOK, resp)
}
