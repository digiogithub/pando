package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/runtime"
	embeddedrt "github.com/digiogithub/pando/internal/runtime/embedded"
)

type containerCapabilitiesResponse struct {
	Runtimes []runtime.RuntimeCapabilities `json:"runtimes"`
	Current  string                        `json:"current"`
}

type containerImageGCRequest struct {
	KeepN *int `json:"keepN,omitempty"`
}

type containerImagesResponse struct {
	Images []embeddedrt.StoreEntry `json:"images"`
	Total  int64                   `json:"total"`
}

func (s *Server) handleContainerCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	current := strings.TrimSpace(strings.ToLower(cfg.Container.Runtime))
	if current == "" {
		current = string(runtime.RuntimeHost)
	}

	writeJSON(w, http.StatusOK, containerCapabilitiesResponse{
		Runtimes: runtime.NewResolver().Discover(),
		Current:  current,
	})
}

func (s *Server) handleContainerConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		if cfg == nil {
			writeError(w, http.StatusInternalServerError, "configuration not loaded")
			return
		}
		writeJSON(w, http.StatusOK, cfg.Container)
	case http.MethodPut:
		var req config.ContainerConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req.Runtime = strings.TrimSpace(strings.ToLower(req.Runtime))
		if req.Runtime == "" {
			writeError(w, http.StatusBadRequest, "runtime must be one of host, docker, podman, embedded, auto")
			return
		}
		if _, err := config.NormalizeContainerConfig(req); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := config.UpdateContainer(req); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update container config: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, config.Get().Container)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleContainerSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": runtime.DefaultSessionManager().List(),
	})
}

func (s *Server) handleStopContainerSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	sessionID := strings.TrimSpace(r.PathValue("sessionId"))
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	if _, ok := runtime.DefaultSessionManager().Get(sessionID); !ok {
		writeError(w, http.StatusNotFound, "container session not found")
		return
	}
	if err := runtime.DefaultSessionManager().Stop(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stop container session: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":    "stopped",
		"sessionId": sessionID,
	})
}

func (s *Server) handleContainerEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > 500 {
			parsed = 500
		}
		limit = parsed
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("sessionId"))
	writeJSON(w, http.StatusOK, map[string]any{
		"events": runtime.DefaultSessionManager().Events(limit, sessionID),
	})
}

func (s *Server) handleContainerImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	store, err := currentImageStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	images, err := store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cached images: "+err.Error())
		return
	}
	total, err := store.TotalSize()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to measure cached images: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, containerImagesResponse{
		Images: images,
		Total:  total,
	})
}

func (s *Server) handleDeleteContainerImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	ref, err := url.PathUnescape(strings.TrimSpace(r.PathValue("ref")))
	if err != nil || ref == "" {
		writeError(w, http.StatusBadRequest, "ref is required")
		return
	}

	store, err := currentImageStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := store.Delete(ref); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete cached image: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
		"ref":    ref,
	})
}

func (s *Server) handleContainerImageGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	cfg := config.Get()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, "configuration not loaded")
		return
	}

	var req containerImageGCRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	keepN := cfg.Container.EmbeddedGCKeepN
	if keepN == 0 {
		keepN = 5
	}
	if req.KeepN != nil {
		keepN = *req.KeepN
	}
	if keepN < 0 {
		writeError(w, http.StatusBadRequest, "keepN must be non-negative")
		return
	}

	client, err := currentRegistryClient()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := client.GC(r.Context(), keepN); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to garbage collect cached images: "+err.Error())
		return
	}

	store, err := currentImageStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	images, err := store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list cached images: "+err.Error())
		return
	}
	total, err := store.TotalSize()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to measure cached images: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"keepN":  keepN,
		"images": images,
		"total":  total,
	})
}

func currentImageStore() (*embeddedrt.ImageStore, error) {
	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	cacheDir, err := embeddedrt.ResolveCacheDir(cfg.Container.EmbeddedCacheDir)
	if err != nil {
		return nil, err
	}
	return &embeddedrt.ImageStore{Root: cacheDir}, nil
}

func currentRegistryClient() (*embeddedrt.RegistryClient, error) {
	cfg := config.Get()
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	cacheDir, err := embeddedrt.ResolveCacheDir(cfg.Container.EmbeddedCacheDir)
	if err != nil {
		return nil, err
	}
	return &embeddedrt.RegistryClient{CacheDir: cacheDir}, nil
}
