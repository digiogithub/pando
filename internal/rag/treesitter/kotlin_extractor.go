// Package treesitter provides Kotlin language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// KotlinExtractor extracts symbols from Kotlin source code
type KotlinExtractor struct {
	BaseExtractor
}

// NewKotlinExtractor creates a new Kotlin extractor
func NewKotlinExtractor(config WalkerConfig) *KotlinExtractor {
	return &KotlinExtractor{
		BaseExtractor: NewBaseExtractor(LanguageKotlin, config),
	}
}

// GetSymbolTypes returns the types of symbols the Kotlin extractor can find
func (k *KotlinExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypePackage,
		SymbolTypeClass,
		SymbolTypeInterface,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeProperty,
		SymbolTypeConstant,
		SymbolTypeEnum,
	}
}

// ExtractSymbols extracts all symbols from Kotlin source code
func (k *KotlinExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := k.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (k *KotlinExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "package_header":
		if symbol := k.extractPackage(node, sourceCode, filePath, projectID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "class_declaration":
		symbols = append(symbols, k.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "object_declaration":
		symbols = append(symbols, k.extractObject(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "interface_declaration":
		if symbol := k.extractInterface(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "function_declaration":
		if symbol := k.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "property_declaration":
		if symbol := k.extractProperty(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}
	}

	return symbols
}

// extractPackage extracts a package declaration
func (k *KotlinExtractor) extractPackage(node *sitter.Node, sourceCode []byte, filePath string, projectID string) *CodeSymbol {
	identifier := FindChildByType(node, "identifier")
	if identifier == nil {
		return nil
	}

	name := GetNodeContent(identifier, sourceCode)
	return k.CreateSymbol(node, sourceCode, SymbolTypePackage, name, "/"+name, filePath, projectID, nil)
}

// extractClass extracts a class declaration
func (k *KotlinExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Find type_identifier for name
	nameNode := FindChildByType(node, "type_identifier")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := k.BuildNamePath(parentPath, name)

	symbol := k.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = k.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract class body members
	body := FindChildByType(node, "class_body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil {
				memberSymbols := k.extractNode(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, memberSymbols...)
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}

// extractObject extracts an object declaration (singleton)
func (k *KotlinExtractor) extractObject(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := FindChildByType(node, "type_identifier")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := k.BuildNamePath(parentPath, name)

	symbol := k.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = k.ExtractDocString(node, sourceCode)
	symbol.Metadata = map[string]interface{}{
		"is_object": true,
	}
	symbols = append(symbols, symbol)

	// Extract object body members
	body := FindChildByType(node, "class_body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil {
				memberSymbols := k.extractNode(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, memberSymbols...)
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}

// extractInterface extracts an interface declaration
func (k *KotlinExtractor) extractInterface(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := FindChildByType(node, "type_identifier")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := k.BuildNamePath(parentPath, name)

	symbol := k.CreateSymbol(node, sourceCode, SymbolTypeInterface, name, namePath, filePath, projectID, parentID)
	symbol.DocString = k.ExtractDocString(node, sourceCode)

	return symbol
}

// extractFunction extracts a function declaration
func (k *KotlinExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := FindChildByType(node, "simple_identifier")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := k.BuildNamePath(parentPath, name)

	symbolType := SymbolTypeFunction
	if parentID != nil {
		symbolType = SymbolTypeMethod
	}

	symbol := k.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = k.ExtractDocString(node, sourceCode)

	return symbol
}

// extractProperty extracts a property declaration
func (k *KotlinExtractor) extractProperty(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Find variable declaration
	varDecl := FindChildByType(node, "variable_declaration")
	if varDecl == nil {
		return nil
	}

	nameNode := FindChildByType(varDecl, "simple_identifier")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := k.BuildNamePath(parentPath, name)

	// Check if const (val with no getter/setter usually)
	symbolType := SymbolTypeProperty
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && GetNodeContent(child, sourceCode) == "const" {
			symbolType = SymbolTypeConstant
			break
		}
	}

	symbol := k.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = k.ExtractDocString(node, sourceCode)

	return symbol
}
