// Package treesitter provides code property-graph edge extraction (lean-ctx Phase 4).
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// EdgeType classifies a code property-graph relationship.
type EdgeType string

const (
	// EdgeImports links a file to a module/package/file it imports.
	EdgeImports EdgeType = "imports"
	// EdgeCalls links an enclosing symbol to a callee referenced by name.
	EdgeCalls EdgeType = "calls"
	// EdgeTypeRef links an enclosing symbol to a referenced type name.
	EdgeTypeRef EdgeType = "type_ref"
)

// CodeEdge represents a directed relationship in the code property graph.
//
// Destinations are intentionally kept as unresolved names (DstName) and/or raw
// import paths (DstPath) — resolution to a concrete symbol/file is done lazily at
// analysis time. This mirrors lean-ctx's pragmatic graph: cheap to build
// incrementally, robust to partial indexes, and good enough for impact and
// related-file BFS.
type CodeEdge struct {
	// Project this edge belongs to.
	ProjectID string `json:"project_id"`

	// Relative source file path within the project.
	FilePath string `json:"file_path"`

	// Relationship kind.
	EdgeType EdgeType `json:"edge_type"`

	// Enclosing symbol ID for calls/type_ref edges (empty for file-level imports).
	SrcSymbolID string `json:"src_symbol_id,omitempty"`

	// Callee / referenced type name (calls, type_ref).
	DstName string `json:"dst_name,omitempty"`

	// Import path string as written in source (imports).
	DstPath string `json:"dst_path,omitempty"`

	// 1-based source line where the relationship occurs.
	StartLine int `json:"start_line"`
}

// EdgeExtractor is an optional interface a SymbolExtractor may implement to emit
// code property-graph edges in addition to symbols. Languages that do not
// implement it simply produce no edges (the graph degrades gracefully).
type EdgeExtractor interface {
	// ExtractEdges returns the relationships found in a parsed tree. The symbols
	// slice (already extracted for the same file) lets implementations attribute
	// call/type-ref edges to their enclosing symbol.
	ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error)
}

// SupportsEdges reports whether the registered extractor for lang can emit
// property-graph edges. Used to advertise per-language capability.
func (w *ASTWalker) SupportsEdges(lang Language) bool {
	extractor, ok := w.GetExtractor(lang)
	if !ok {
		return false
	}
	_, ok = extractor.(EdgeExtractor)
	return ok
}

// ExtractEdges dispatches edge extraction to the language extractor when it
// implements EdgeExtractor. Languages without edge support return (nil, nil).
func (w *ASTWalker) ExtractEdges(tree *sitter.Tree, sourceCode []byte, lang Language, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	extractor, ok := w.GetExtractor(lang)
	if !ok {
		return nil, nil
	}
	ee, ok := extractor.(EdgeExtractor)
	if !ok {
		return nil, nil
	}
	return ee.ExtractEdges(tree, sourceCode, filePath, projectID, symbols)
}

// EdgeCapableLanguages returns the languages whose registered extractor emits
// property-graph edges, for capability reporting/UX.
func (w *ASTWalker) EdgeCapableLanguages() []Language {
	var langs []Language
	for lang, ex := range w.extractors {
		if _, ok := ex.(EdgeExtractor); ok {
			langs = append(langs, lang)
		}
	}
	return langs
}

// enclosingSymbolID returns the ID of the smallest function/method/constructor
// symbol whose byte range contains pos, or "" if none. Used to attribute a call
// or type reference to the symbol that contains it.
func enclosingSymbolID(symbols []*CodeSymbol, pos int) string {
	bestID := ""
	bestSpan := -1
	for _, s := range symbols {
		if s == nil {
			continue
		}
		switch s.SymbolType {
		case SymbolTypeFunction, SymbolTypeMethod, SymbolTypeConstructor:
			// candidate
		default:
			continue
		}
		if pos < s.StartByte || pos >= s.EndByte {
			continue
		}
		span := s.EndByte - s.StartByte
		if bestSpan < 0 || span < bestSpan {
			bestSpan = span
			bestID = s.ID
		}
	}
	return bestID
}

// unquote strips matching leading/trailing quote characters (", ', `) from a
// raw string literal as captured from source.
func unquote(s string) string {
	if len(s) < 2 {
		return s
	}
	first := s[0]
	last := s[len(s)-1]
	if (first == '"' || first == '\'' || first == '`') && first == last {
		return s[1 : len(s)-1]
	}
	return s
}
