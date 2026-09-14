// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/rag/kb"
)

// kbDocumentUpsertRequest is the request body for POST /api/v1/remembrances/kb/documents.
type kbDocumentUpsertRequest struct {
	FilePath string                 `json:"file_path"`
	Content  string                 `json:"content"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	Tags     []string               `json:"tags,omitempty"`
}

// kbDocumentUpsertResponse reports the outcome of an upsert.
type kbDocumentUpsertResponse struct {
	FilePath string `json:"file_path"`
	Action   string `json:"action"` // "created" or "updated"
}

// handleUpsertKBDocument creates a knowledge-base document, or updates it when
// file_path already names one. This is the host-driven counterpart to the
// kb_add_document tool: it lets an external corpus owner push freshness over
// REST instead of trusting the filesystem watcher.
//
// POST /api/v1/remembrances/kb/documents
// Body: {"file_path": "...", "content": "...", "metadata": {...}, "tags": [...]}
func (s *Server) handleUpsertKBDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.app == nil || s.app.Remembrances == nil || s.app.Remembrances.KB == nil {
		writeError(w, http.StatusServiceUnavailable, "remembrances KB store not initialized")
		return
	}

	var req kbDocumentUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	filePath := strings.TrimSpace(req.FilePath)
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "file_path is required")
		return
	}

	store := s.app.Remembrances.KB
	ctx := r.Context()

	existing, err := store.GetDocument(ctx, filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to look up document: "+err.Error())
		return
	}

	metadata := req.Metadata
	action := "created"
	if existing != nil {
		action = "updated"
		if metadata == nil {
			// updateDocument is delete-then-add with wholesale metadata
			// replacement (kb.go), so an upsert that omits metadata must
			// re-send the stored map rather than silently dropping it.
			metadata = existing.Metadata
		}
	}
	// tags have no storage of their own — they live under metadata["tags"],
	// same as kb_add_document. Leaves metadata untouched when tags is empty.
	metadata = kb.InjectTagsIntoMetadata(metadata, req.Tags)

	if existing != nil {
		if err := store.UpdateDocument(ctx, filePath, req.Content, metadata); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update document: "+err.Error())
			return
		}
	} else {
		if err := store.AddDocument(ctx, filePath, req.Content, metadata); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add document: "+err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, kbDocumentUpsertResponse{FilePath: filePath, Action: action})
}

// kbDocumentDeleteRequest is the request body for DELETE /api/v1/remembrances/kb/documents.
type kbDocumentDeleteRequest struct {
	FilePath string `json:"file_path"`
}

// handleDeleteKBDocument removes a knowledge-base document, its chunks and the
// mirrored file under KB.FilesystemMirrorPath(). Deleting only the database row
// would let a later reindex resurrect the document from its still-present
// mirror file, so both must go together.
//
// DELETE /api/v1/remembrances/kb/documents
// Body: {"file_path": "..."}
func (s *Server) handleDeleteKBDocument(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.app == nil || s.app.Remembrances == nil || s.app.Remembrances.KB == nil {
		writeError(w, http.StatusServiceUnavailable, "remembrances KB store not initialized")
		return
	}

	var req kbDocumentDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	filePath := strings.TrimSpace(req.FilePath)
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "file_path is required")
		return
	}

	store := s.app.Remembrances.KB
	if err := store.DeleteDocument(r.Context(), filePath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete document: "+err.Error())
		return
	}
	if err := store.DeleteDocumentFromFilesystem(filePath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove mirrored document: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"file_path": filePath, "status": "deleted"})
}

// kbReindexMu serializes concurrent KB reindex runs. A reindex walks the whole
// filesystem mirror and re-embeds every changed document, so two concurrent
// runs racing on the same rows would waste embedding calls and could interleave
// writes; the second caller gets 409 instead of silently starting a second sync.
var kbReindexMu sync.Mutex

// handleReindexKB re-syncs the KB filesystem mirror (KB.FilesystemMirrorPath())
// into the database via KB.SyncDirectoryWithStats, so a host that writes files
// outside of Pando's watcher — or that disables KBWatch entirely — can drive a
// deterministic re-sync instead of waiting for (or trusting) the watcher.
//
// POST /api/v1/remembrances/kb/reindex
func (s *Server) handleReindexKB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.app == nil || s.app.Remembrances == nil || s.app.Remembrances.KB == nil {
		writeError(w, http.StatusServiceUnavailable, "remembrances KB store not initialized")
		return
	}

	store := s.app.Remembrances.KB
	mirrorPath := store.FilesystemMirrorPath()
	if strings.TrimSpace(mirrorPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "KB filesystem mirror not configured")
		return
	}

	if !kbReindexMu.TryLock() {
		writeError(w, http.StatusConflict, "a KB reindex is already running")
		return
	}
	defer kbReindexMu.Unlock()

	stats, err := store.SyncDirectoryWithStats(r.Context(), mirrorPath, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "kb reindex failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
