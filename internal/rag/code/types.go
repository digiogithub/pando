// Package code provides code indexing with tree-sitter and semantic search.
package code

import "github.com/digiogithub/pando/internal/rag/treesitter"

// Re-export treesitter types for convenience
type (
	Language             = treesitter.Language
	SymbolType           = treesitter.SymbolType
	CodeSymbol           = treesitter.CodeSymbol
	CodeFile             = treesitter.CodeFile
	CodeProject          = treesitter.CodeProject
	IndexingJob          = treesitter.IndexingJob
	IndexingStatus       = treesitter.IndexingStatus
	ParseResult          = treesitter.ParseResult
	SymbolQuery          = treesitter.SymbolQuery
	SemanticSearchQuery  = treesitter.SemanticSearchQuery
	SemanticSearchResult = treesitter.SemanticSearchResult
)

// Re-export constants
const (
	IndexingStatusPending    = treesitter.IndexingStatusPending
	IndexingStatusInProgress = treesitter.IndexingStatusInProgress
	IndexingStatusCompleted  = treesitter.IndexingStatusCompleted
	IndexingStatusFailed     = treesitter.IndexingStatusFailed
	IndexingStatusCancelled  = treesitter.IndexingStatusCancelled
)

// HybridSearchResult represents a result from hybrid search
type HybridSearchResult struct {
	Symbol      *CodeSymbol `json:"symbol"`
	Score       float64     `json:"score"`
	VectorScore float64     `json:"vector_score,omitempty"`
	FTSScore    float64     `json:"fts_score,omitempty"`
	Rank        int         `json:"rank"`
}
