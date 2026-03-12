// Package treesitter provides Svelte language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// SvelteExtractor extracts symbols from Svelte source code
type SvelteExtractor struct {
	BaseExtractor
}

// NewSvelteExtractor creates a new Svelte extractor
func NewSvelteExtractor(config WalkerConfig) *SvelteExtractor {
	return &SvelteExtractor{
		BaseExtractor: NewBaseExtractor(LanguageSvelte, config),
	}
}

// GetSymbolTypes returns the types of symbols the Svelte extractor can find
func (s *SvelteExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeVariable,
		SymbolTypeConstant,
	}
}

// ExtractSymbols extracts all symbols from Svelte source code
func (s *SvelteExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	// Svelte files have <script>, <template>, and <style> sections
	// We focus on extracting symbols from <script> sections
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		// Look for script elements
		if child.Type() == "script_element" {
			childSymbols := s.extractScriptElement(child, sourceCode, filePath, projectID)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols, nil
}

// extractScriptElement extracts symbols from a Svelte <script> section
func (s *SvelteExtractor) extractScriptElement(node *sitter.Node, sourceCode []byte, filePath string, projectID string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Find the script content
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		// Process child nodes (JavaScript/TypeScript code)
		childSymbols := s.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols
}

// extractNode extracts symbols from a node
func (s *SvelteExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "class_declaration":
		symbols = append(symbols, s.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "function_declaration":
		if symbol := s.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "lexical_declaration", "variable_declaration":
		symbols = append(symbols, s.extractVariableDeclaration(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "export_statement":
		declaration := node.ChildByFieldName("declaration")
		if declaration != nil {
			symbols = append(symbols, s.extractNode(declaration, sourceCode, filePath, projectID, parentPath, parentID)...)
		}
	}

	return symbols
}

// extractClass extracts a class declaration
func (s *SvelteExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract class body members
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member == nil {
				continue
			}

			if member.Type() == "method_definition" {
				memberSymbols := s.extractMethod(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
				for _, ms := range memberSymbols {
					symbol.Children = append(symbol.Children, ms)
				}
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}

// extractMethod extracts a method from a class
func (s *SvelteExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeMethod, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	return symbols
}

// extractFunction extracts a function declaration
func (s *SvelteExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)

	return symbol
}

// extractVariableDeclaration extracts variable declarations
func (s *SvelteExtractor) extractVariableDeclaration(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Type() != "variable_declarator" {
			continue
		}

		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := s.BuildNamePath(parentPath, name)

		// Determine if it's a const
		symbolType := SymbolTypeVariable
		if node.Type() == "lexical_declaration" {
			// Check if it's const or let
			for j := 0; j < int(node.ChildCount()); j++ {
				c := node.Child(j)
				if c != nil && c.Type() == "const" {
					symbolType = SymbolTypeConstant
					break
				}
			}
		}

		symbol := s.CreateSymbol(child, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
		symbols = append(symbols, symbol)
	}

	return symbols
}
