package tools

import (
	"context"
	"encoding/json"
)

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
}

type toolResponseType string

type (
	sessionIDContextKey       string
	messageIDContextKey       string
	acpClientConnContextKey   string
	sessionCacheContextKey    string
	runtimeResolverContextKey string
	workspaceFSContextKey     string
)

const (
	ToolResponseTypeText  toolResponseType = "text"
	ToolResponseTypeImage toolResponseType = "image"

	SessionIDContextKey     sessionIDContextKey     = "session_id"
	MessageIDContextKey     messageIDContextKey     = "message_id"
	ACPClientConnContextKey acpClientConnContextKey = "acp_client_connection"
	SessionCacheContextKey  sessionCacheContextKey  = "session_cache"
	// RuntimeResolverContextKey is the hook point for injecting a non-host
	// RuntimeResolver in Phase 2 without changing existing execution paths.
	RuntimeResolverContextKey runtimeResolverContextKey = "runtime_resolver"
	WorkspaceFSContextKey     workspaceFSContextKey     = "workspace_fs"
)

type ToolResponse struct {
	Type     toolResponseType `json:"type"`
	Content  string           `json:"content"`
	Metadata string           `json:"metadata,omitempty"`
	IsError  bool             `json:"is_error"`
}

func NewTextResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
	}
}

func WithResponseMetadata(response ToolResponse, metadata any) ToolResponse {
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return response
		}
		response.Metadata = string(metadataBytes)
	}
	return response
}

func NewTextErrorResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
		IsError: true,
	}
}

type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

type BaseTool interface {
	Info() ToolInfo
	Run(ctx context.Context, params ToolCall) (ToolResponse, error)
}

// PaginationInfo is a standardized pagination descriptor included in tool metadata.
type PaginationInfo struct {
	TotalItems    int    `json:"total_items,omitempty"`
	ReturnedItems int    `json:"returned_items,omitempty"`
	Offset        int    `json:"offset,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	HasMore       bool   `json:"has_more,omitempty"`
	CacheID       string `json:"cache_id,omitempty"` // Set if response was auto-cached
}

func GetContextValues(ctx context.Context) (string, string) {
	sessionID := ctx.Value(SessionIDContextKey)
	messageID := ctx.Value(MessageIDContextKey)
	if sessionID == nil {
		return "", ""
	}
	if messageID == nil {
		return sessionID.(string), ""
	}
	return sessionID.(string), messageID.(string)
}
