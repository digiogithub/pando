// Package treesitter provides JavaScript language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// JavaScriptExtractor extracts symbols from JavaScript source code
type JavaScriptExtractor struct {
	BaseExtractor
}

// NewJavaScriptExtractor creates a new JavaScript extractor
func NewJavaScriptExtractor(config WalkerConfig) *JavaScriptExtractor {
	return &JavaScriptExtractor{
		BaseExtractor: NewBaseExtractor(LanguageJavaScript, config),
	}
}

// GetSymbolTypes returns the types of symbols the JavaScript extractor can find
func (j *JavaScriptExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeVariable,
		SymbolTypeConstant,
	}
}

// ExtractSymbols extracts all symbols from JavaScript source code
func (j *JavaScriptExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := j.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (j *JavaScriptExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "class_declaration":
		symbols = append(symbols, j.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "function_declaration":
		if symbol := j.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "lexical_declaration", "variable_declaration":
		symbols = append(symbols, j.extractVariableDeclaration(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "export_statement":
		declaration := node.ChildByFieldName("declaration")
		if declaration != nil {
			symbols = append(symbols, j.extractNode(declaration, sourceCode, filePath, projectID, parentPath, parentID)...)
		}
	}

	return symbols
}

// extractClass extracts a class declaration
func (j *JavaScriptExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := j.BuildNamePath(parentPath, name)

	symbol := j.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = j.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract class body members
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member == nil || member.Type() != "method_definition" {
				continue
			}

			memberSymbols := j.extractMethod(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
			symbol.Children = append(symbol.Children, memberSymbols...)
			symbols = append(symbols, memberSymbols...)
		}
	}

	return symbols
}

// extractMethod extracts a method from a class
func (j *JavaScriptExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := j.BuildNamePath(parentPath, name)

	symbolType := SymbolTypeMethod
	if name == "constructor" {
		symbolType = SymbolTypeConstructor
	}

	symbol := j.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = j.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	return symbols
}

// extractFunction extracts a function declaration
func (j *JavaScriptExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := j.BuildNamePath(parentPath, name)

	symbol := j.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.DocString = j.ExtractDocString(node, sourceCode)

	return symbol
}

// extractVariableDeclaration extracts variable/const declarations
func (j *JavaScriptExtractor) extractVariableDeclaration(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	isConst := false
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && GetNodeContent(child, sourceCode) == "const" {
			isConst = true
			break
		}
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		declarator := node.NamedChild(i)
		if declarator == nil || declarator.Type() != "variable_declarator" {
			continue
		}

		nameNode := declarator.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := j.BuildNamePath(parentPath, name)

		symbolType := SymbolTypeVariable
		if isConst {
			symbolType = SymbolTypeConstant
		}

		value := declarator.ChildByFieldName("value")
		if value != nil {
			if value.Type() == "arrow_function" || value.Type() == "function" {
				symbolType = SymbolTypeFunction
			}
		}

		symbol := j.CreateSymbol(declarator, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
		symbol.DocString = j.ExtractDocString(node, sourceCode)
		symbols = append(symbols, symbol)
	}

	return symbols
}
