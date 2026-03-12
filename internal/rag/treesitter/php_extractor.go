// Package treesitter provides PHP language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// PHPExtractor extracts symbols from PHP source code
type PHPExtractor struct {
	BaseExtractor
}

// NewPHPExtractor creates a new PHP extractor
func NewPHPExtractor(config WalkerConfig) *PHPExtractor {
	return &PHPExtractor{
		BaseExtractor: NewBaseExtractor(LanguagePHP, config),
	}
}

// GetSymbolTypes returns the types of symbols the PHP extractor can find
func (p *PHPExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeInterface,
		SymbolTypeTrait,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeProperty,
		SymbolTypeConstant,
		SymbolTypeNamespace,
	}
}

// ExtractSymbols extracts all symbols from PHP source code
func (p *PHPExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	// Walk through program nodes
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := p.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (p *PHPExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "namespace_definition":
		symbols = append(symbols, p.extractNamespace(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "class_declaration":
		symbols = append(symbols, p.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "interface_declaration":
		if symbol := p.extractInterface(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "trait_declaration":
		if symbol := p.extractTrait(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "function_definition":
		if symbol := p.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "const_declaration":
		symbols = append(symbols, p.extractConstants(node, sourceCode, filePath, projectID, parentPath, parentID)...)
	}

	return symbols
}

// extractNamespace extracts a namespace declaration
func (p *PHPExtractor) extractNamespace(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbol := p.CreateSymbol(node, sourceCode, SymbolTypeNamespace, name, namePath, filePath, projectID, parentID)
	symbols = append(symbols, symbol)

	// Extract declarations within namespace
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			child := body.NamedChild(i)
			if child != nil {
				childSymbols := p.extractNode(child, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, childSymbols...)
				symbols = append(symbols, childSymbols...)
			}
		}
	}

	return symbols
}

// extractClass extracts a class declaration
func (p *PHPExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbol := p.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract class body members
	body := node.ChildByFieldName("body")
	if body == nil {
		return symbols
	}

	for i := 0; i < int(body.NamedChildCount()); i++ {
		member := body.NamedChild(i)
		if member == nil {
			continue
		}

		switch member.Type() {
		case "method_declaration":
			if methodSymbol := p.extractMethod(member, sourceCode, filePath, projectID, namePath, &symbol.ID); methodSymbol != nil {
				symbol.Children = append(symbol.Children, methodSymbol)
				symbols = append(symbols, methodSymbol)
			}

		case "property_declaration":
			propSymbols := p.extractProperties(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
			symbol.Children = append(symbol.Children, propSymbols...)
			symbols = append(symbols, propSymbols...)

		case "const_declaration":
			constSymbols := p.extractConstants(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
			symbol.Children = append(symbol.Children, constSymbols...)
			symbols = append(symbols, constSymbols...)
		}
	}

	return symbols
}

// extractInterface extracts an interface declaration
func (p *PHPExtractor) extractInterface(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbol := p.CreateSymbol(node, sourceCode, SymbolTypeInterface, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.ExtractDocString(node, sourceCode)

	return symbol
}

// extractTrait extracts a trait declaration
func (p *PHPExtractor) extractTrait(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbol := p.CreateSymbol(node, sourceCode, SymbolTypeTrait, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.ExtractDocString(node, sourceCode)

	return symbol
}

// extractFunction extracts a function definition
func (p *PHPExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbol := p.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.ExtractDocString(node, sourceCode)

	return symbol
}

// extractMethod extracts a method declaration
func (p *PHPExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbolType := SymbolTypeMethod
	if name == "__construct" {
		symbolType = SymbolTypeConstructor
	}

	symbol := p.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.ExtractDocString(node, sourceCode)

	return symbol
}

// extractProperties extracts property declarations
func (p *PHPExtractor) extractProperties(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Type() != "property_element" {
			continue
		}

		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := p.BuildNamePath(parentPath, name)

		symbol := p.CreateSymbol(child, sourceCode, SymbolTypeProperty, name, namePath, filePath, projectID, parentID)
		symbols = append(symbols, symbol)
	}

	return symbols
}

// extractConstants extracts constant declarations
func (p *PHPExtractor) extractConstants(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Type() != "const_element" {
			continue
		}

		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := p.BuildNamePath(parentPath, name)

		symbol := p.CreateSymbol(child, sourceCode, SymbolTypeConstant, name, namePath, filePath, projectID, parentID)
		symbols = append(symbols, symbol)
	}

	return symbols
}
