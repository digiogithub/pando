// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package proxy provides the server-side IPC dispatcher for Remembrances write
// operations (KB, Events, Code indexing).  It bridges the dbproxy infrastructure
// layer and the rag business-logic layer without creating circular imports.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
	rag "github.com/digiogithub/pando/internal/rag"
)

// kbAddDocRequest mirrors kb.kbAddDocumentRequest (unexported) for JSON decoding.
type kbAddDocRequest struct {
	FilePath   string                 `json:"file_path"`
	Content    string                 `json:"content"`
	Metadata   map[string]interface{} `json:"metadata"`
	Chunks     []string               `json:"chunks"`
	Embeddings [][]float32            `json:"embeddings"`
}

// saveEventReq mirrors events.saveEventRequest (unexported) for JSON decoding.
type saveEventReq struct {
	Subject   string                 `json:"subject"`
	Content   string                 `json:"content"`
	Metadata  map[string]interface{} `json:"metadata"`
	Embedding []float32              `json:"embedding"`
}

type replaceSessionEventsReq struct {
	SessionID  string                 `json:"session_id"`
	Subject    string                 `json:"subject"`
	Metadata   map[string]interface{} `json:"metadata"`
	Chunks     []string               `json:"chunks"`
	Embeddings [][]float32            `json:"embeddings"`
}

// codeUpsertProjectReq mirrors code.codeUpsertProjectRequest for JSON decoding.
type codeUpsertProjectReq struct {
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"root_path"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// codeSetProjectStatusReq mirrors code.codeSetProjectStatusRequest for JSON decoding.
type codeSetProjectStatusReq struct {
	ProjectID     string     `json:"project_id"`
	Status        string     `json:"status"`
	LastIndexedAt *time.Time `json:"last_indexed_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// codeIndexFileReq mirrors code.codeIndexFileRequest for JSON decoding.
// Symbols are decoded as json.RawMessage and then forwarded to the store.
type codeIndexFileReq struct {
	ProjectID string          `json:"project_id"`
	RootPath  string          `json:"root_path"`
	FilePath  string          `json:"file_path"`
	Language  string          `json:"language"`
	FileHash  string          `json:"file_hash"`
	Symbols   json.RawMessage `json:"symbols"`
	Edges     json.RawMessage `json:"edges,omitempty"`
}

// codeDeleteFileReq carries the project ID and file path for a file deletion.
type codeDeleteFileReq struct {
	ProjectID string `json:"project_id"`
	FilePath  string `json:"file_path"`
}

// RemembrancesWriteDispatcher implements dbproxy.RemembrancesDispatcher.
// It routes inbound IPC write requests for KB, Events, and Code indexing to
// the appropriate methods on the local RemembrancesService.
type RemembrancesWriteDispatcher struct {
	svc *rag.RemembrancesService
}

// NewRemembrancesWriteDispatcher creates a dispatcher backed by svc.
// svc must be the primary instance's RemembrancesService (with RW database access).
func NewRemembrancesWriteDispatcher(svc *rag.RemembrancesService) *RemembrancesWriteDispatcher {
	return &RemembrancesWriteDispatcher{svc: svc}
}

// Ensure RemembrancesWriteDispatcher satisfies the dbproxy interface at compile time.
var _ dbproxy.RemembrancesDispatcher = (*RemembrancesWriteDispatcher)(nil)

// DispatchRemembrancesWrite routes a proxied write request to the appropriate store method.
func (d *RemembrancesWriteDispatcher) DispatchRemembrancesWrite(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {

	// ---- KB writes ----

	case "KBAddDocument":
		if d.svc.KB == nil {
			return nil, fmt.Errorf("rag dispatcher: KB store not available")
		}
		var req kbAddDocRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: KBAddDocument unmarshal: %w", err)
		}
		err := d.svc.KB.AddDocumentWithEmbeddings(ctx, req.FilePath, req.Content, req.Metadata, req.Chunks, req.Embeddings)
		return nil, err

	case "KBUpdateDocument":
		if d.svc.KB == nil {
			return nil, fmt.Errorf("rag dispatcher: KB store not available")
		}
		var req kbAddDocRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: KBUpdateDocument unmarshal: %w", err)
		}
		// Delete then re-insert with new content and pre-computed embeddings.
		_ = d.svc.KB.DeleteDocument(ctx, req.FilePath)
		err := d.svc.KB.AddDocumentWithEmbeddings(ctx, req.FilePath, req.Content, req.Metadata, req.Chunks, req.Embeddings)
		return nil, err

	case "KBDeleteDocument":
		if d.svc.KB == nil {
			return nil, fmt.Errorf("rag dispatcher: KB store not available")
		}
		var filePath string
		if err := json.Unmarshal(params, &filePath); err != nil {
			return nil, fmt.Errorf("rag dispatcher: KBDeleteDocument unmarshal: %w", err)
		}
		err := d.svc.KB.DeleteDocument(ctx, filePath)
		return nil, err

	// ---- Event writes ----

	case "SaveEvent":
		if d.svc.Events == nil {
			return nil, fmt.Errorf("rag dispatcher: Events store not available")
		}
		var req saveEventReq
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: SaveEvent unmarshal: %w", err)
		}
		id, err := d.svc.Events.SaveEventWithEmbedding(ctx, req.Subject, req.Content, req.Metadata, req.Embedding)
		if err != nil {
			return nil, err
		}
		return json.Marshal(id)

	case "ReplaceSessionEvents":
		if d.svc.Events == nil {
			return nil, fmt.Errorf("rag dispatcher: Events store not available")
		}
		var req replaceSessionEventsReq
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: ReplaceSessionEvents unmarshal: %w", err)
		}
		err := d.svc.Events.ReplaceSessionEvents(ctx, req.SessionID, req.Subject, req.Metadata, req.Chunks, req.Embeddings)
		return nil, err

	// ---- Code indexing writes ----

	case "CodeUpsertProject":
		if d.svc.Code == nil {
			return nil, fmt.Errorf("rag dispatcher: Code indexer not available")
		}
		var req codeUpsertProjectReq
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: CodeUpsertProject unmarshal: %w", err)
		}
		err := d.svc.Code.UpsertProjectDirect(ctx,
			req.ProjectID, req.Name, req.RootPath, req.Status,
			req.CreatedAt, req.UpdatedAt,
		)
		return nil, err

	case "CodeSetProjectStatus":
		if d.svc.Code == nil {
			return nil, fmt.Errorf("rag dispatcher: Code indexer not available")
		}
		var req codeSetProjectStatusReq
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: CodeSetProjectStatus unmarshal: %w", err)
		}
		err := d.svc.Code.SetProjectStatusDirect(ctx,
			req.ProjectID, req.Status, req.LastIndexedAt, req.UpdatedAt,
		)
		return nil, err

	case "CodeIndexFile":
		if d.svc.Code == nil {
			return nil, fmt.Errorf("rag dispatcher: Code indexer not available")
		}
		var req codeIndexFileReq
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: CodeIndexFile unmarshal: %w", err)
		}
		err := d.svc.Code.IndexFileDirect(ctx,
			req.ProjectID, req.FilePath, req.Language, req.FileHash, req.Symbols, req.Edges,
		)
		return nil, err

	case "CodeDeleteProject":
		if d.svc.Code == nil {
			return nil, fmt.Errorf("rag dispatcher: Code indexer not available")
		}
		var projectID string
		if err := json.Unmarshal(params, &projectID); err != nil {
			return nil, fmt.Errorf("rag dispatcher: CodeDeleteProject unmarshal: %w", err)
		}
		err := d.svc.Code.DeleteProjectDirect(ctx, projectID)
		return nil, err

	case "CodeDeleteFile":
		if d.svc.Code == nil {
			return nil, fmt.Errorf("rag dispatcher: Code indexer not available")
		}
		var req codeDeleteFileReq
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("rag dispatcher: CodeDeleteFile unmarshal: %w", err)
		}
		err := d.svc.Code.DeleteFileDirect(ctx, req.ProjectID, req.FilePath)
		return nil, err

	case "CodeUpdateLanguageStats":
		if d.svc.Code == nil {
			return nil, fmt.Errorf("rag dispatcher: Code indexer not available")
		}
		var projectID string
		if err := json.Unmarshal(params, &projectID); err != nil {
			return nil, fmt.Errorf("rag dispatcher: CodeUpdateLanguageStats unmarshal: %w", err)
		}
		err := d.svc.Code.UpdateLanguageStatsDirect(ctx, projectID)
		return nil, err

	default:
		return nil, fmt.Errorf("rag dispatcher: unsupported remembrances write method %q", method)
	}
}
