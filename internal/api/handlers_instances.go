// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	ipc "github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/instanceregistry"
)

// instanceResponse is the JSON wire representation of a running Pando instance.
type instanceResponse struct {
	InstanceID string    `json:"instance_id"`
	Path       string    `json:"path"`
	PID        int       `json:"pid"`
	PubPort    int       `json:"pub_port"`
	RPCPort    int       `json:"rpc_port"`
	StartedAt  time.Time `json:"started_at"`
	Mode       string    `json:"mode"`
	IsPrimary  bool      `json:"is_primary"`
}

func entryToResponse(e *instanceregistry.Entry) instanceResponse {
	return instanceResponse{
		InstanceID: e.InstanceID,
		Path:       e.Path,
		PID:        e.PID,
		PubPort:    e.PubPort,
		RPCPort:    e.RPCPort,
		StartedAt:  e.StartedAt,
		Mode:       string(e.Mode),
		IsPrimary:  e.IsPrimary,
	}
}

// handleListInstances handles GET /api/v1/instances.
// Returns all live Pando instances registered in instanceregistry.
func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	reg := instanceregistry.New()
	entries, err := reg.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list instances: "+err.Error())
		return
	}

	responses := make([]instanceResponse, len(entries))
	for i, e := range entries {
		responses[i] = entryToResponse(e)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"instances": responses})
}

// handleGetInstance handles GET /api/v1/instances/{id}.
// Returns a single instance by its InstanceID.
func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "instance id required")
		return
	}

	reg := instanceregistry.New()
	entry, err := reg.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get instance: "+err.Error())
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}

	writeJSON(w, http.StatusOK, entryToResponse(entry))
}

// handleInstanceStream handles GET /api/v1/instances/{id}/stream.
// Proxies the remote instance's ZMQ PUB stream as Server-Sent Events.
func (s *Server) handleInstanceStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "instance id required")
		return
	}

	reg := instanceregistry.New()
	entry, err := reg.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get instance: "+err.Error())
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}

	if entry.PubPort == 0 {
		writeError(w, http.StatusServiceUnavailable, "instance has no PUB port registered")
		return
	}

	pubEndpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.PubPort)

	client, err := ipc.NewClient(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create IPC client: "+err.Error())
		return
	}
	defer client.Close()

	// Subscribe to all topics from the remote instance.
	envCh, err := client.SubscribeTo(pubEndpoint)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to subscribe to instance stream: "+err.Error())
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	// Stream events until the client disconnects or the context is cancelled.
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case env, open := <-envCh:
			if !open {
				return
			}
			// Encode the full envelope as JSON and send as an SSE data line.
			data, err := json.Marshal(env)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// instanceSendMessageRequest is the body accepted by POST /api/v1/instances/{id}/sessions/{sid}/message.
type instanceSendMessageRequest struct {
	Content string `json:"content"`
}

// handleInstanceSendMessage handles POST /api/v1/instances/{id}/sessions/{sid}/message.
// Sends a user message to a session on a remote instance via RemoteControl.
func (s *Server) handleInstanceSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	instanceID := r.PathValue("id")
	sessionID := r.PathValue("sid")

	if instanceID == "" {
		writeError(w, http.StatusBadRequest, "instance id required")
		return
	}
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id required")
		return
	}

	var req instanceSendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	reg := instanceregistry.New()
	entry, err := reg.Get(instanceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get instance: "+err.Error())
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}

	if entry.RPCPort == 0 {
		writeError(w, http.StatusServiceUnavailable, "instance has no RPC port registered")
		return
	}

	rpcEndpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.RPCPort)

	client, err := ipc.NewClient(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create IPC client: "+err.Error())
		return
	}
	defer client.Close()

	params := protocol.MessageSendParams{
		SessionID: sessionID,
		Content:   req.Content,
	}

	callCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	_, err = client.Call(callCtx, rpcEndpoint, protocol.MethodMessageSend, params)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to send message: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
