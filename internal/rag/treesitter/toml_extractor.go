// Package treesitter provides TOML language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// TOMLExtractor extracts symbols from TOML source code
type TOMLExtractor struct {
	BaseExtractor
}

// NewTOMLExtractor creates a new TOML extractor
func NewTOMLExtractor(config WalkerConfig) *TOMLExtractor {
	return &TOMLExtractor{
		BaseExtractor: NewBaseExtractor(LanguageTOML, config),
	}
}

// GetSymbolTypes returns the types of symbols the TOML extractor can find
func (t *TOMLExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeVariable,
		SymbolTypeConstant,
	}
}

// ExtractSymbols extracts all symbols from TOML source code
func (t *TOMLExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := t.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (t *TOMLExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "table":
		// TOML [section] headers
		if symbol := t.extractTable(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
			// Extract pairs within this table
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child != nil && child.Type() == "pair" {
					if pairSymbol := t.extractPair(child, sourceCode, filePath, projectID, symbol.NamePath, &symbol.ID); pairSymbol != nil {
						symbols = append(symbols, pairSymbol)
					}
				}
			}
		}

	case "pair":
		// Key-value pairs at root level
		if symbol := t.extractPair(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}
	}

	return symbols
}

// extractTable extracts a TOML table (section)
func (t *TOMLExtractor) extractTable(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Find the table name
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}

		if child.Type() == "bare_key" || child.Type() == "quoted_key" || child.Type() == "dotted_key" {
			name := GetNodeContent(child, sourceCode)
			namePath := t.BuildNamePath(parentPath, name)
			return t.CreateSymbol(node, sourceCode, SymbolTypeConstant, name, namePath, filePath, projectID, parentID)
		}
	}

	return nil
}

// extractPair extracts a TOML key-value pair
func (t *TOMLExtractor) extractPair(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Get the key
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		if child.Type() == "bare_key" || child.Type() == "quoted_key" || child.Type() == "dotted_key" {
			name := GetNodeContent(child, sourceCode)
			namePath := t.BuildNamePath(parentPath, name)
			return t.CreateSymbol(node, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
		}
	}

	return nil
}
