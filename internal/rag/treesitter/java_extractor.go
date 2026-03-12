// Package treesitter provides Java language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// JavaExtractor extracts symbols from Java source code
type JavaExtractor struct {
	BaseExtractor
}

// NewJavaExtractor creates a new Java extractor
func NewJavaExtractor(config WalkerConfig) *JavaExtractor {
	return &JavaExtractor{
		BaseExtractor: NewBaseExtractor(LanguageJava, config),
	}
}

// GetSymbolTypes returns the types of symbols the Java extractor can find
func (j *JavaExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypePackage,
		SymbolTypeClass,
		SymbolTypeInterface,
		SymbolTypeEnum,
		SymbolTypeMethod,
		SymbolTypeConstructor,
		SymbolTypeField,
		SymbolTypeConstant,
	}
}

// ExtractSymbols extracts all symbols from Java source code
func (j *JavaExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
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
func (j *JavaExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "package_declaration":
		if symbol := j.extractPackage(node, sourceCode, filePath, projectID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "class_declaration":
		symbols = append(symbols, j.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "interface_declaration":
		symbols = append(symbols, j.extractInterface(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "enum_declaration":
		symbols = append(symbols, j.extractEnum(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "method_declaration":
		if symbol := j.extractMethod(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "constructor_declaration":
		if symbol := j.extractConstructor(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "field_declaration":
		symbols = append(symbols, j.extractField(node, sourceCode, filePath, projectID, parentPath, parentID)...)
	}

	return symbols
}

// extractPackage extracts a package declaration
func (j *JavaExtractor) extractPackage(node *sitter.Node, sourceCode []byte, filePath string, projectID string) *CodeSymbol {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Type() == "scoped_identifier" {
			name := GetNodeContent(child, sourceCode)
			return j.CreateSymbol(node, sourceCode, SymbolTypePackage, name, "/"+name, filePath, projectID, nil)
		}
	}
	return nil
}

// extractClass extracts a class declaration
func (j *JavaExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
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
			if member != nil {
				memberSymbols := j.extractNode(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, memberSymbols...)
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}

// extractInterface extracts an interface declaration
func (j *JavaExtractor) extractInterface(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := j.BuildNamePath(parentPath, name)

	symbol := j.CreateSymbol(node, sourceCode, SymbolTypeInterface, name, namePath, filePath, projectID, parentID)
	symbol.DocString = j.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract interface members
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil && member.Type() == "method_declaration" {
				if methodSymbol := j.extractMethod(member, sourceCode, filePath, projectID, namePath, &symbol.ID); methodSymbol != nil {
					symbol.Children = append(symbol.Children, methodSymbol)
					symbols = append(symbols, methodSymbol)
				}
			}
		}
	}

	return symbols
}

// extractEnum extracts an enum declaration
func (j *JavaExtractor) extractEnum(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := j.BuildNamePath(parentPath, name)

	symbol := j.CreateSymbol(node, sourceCode, SymbolTypeEnum, name, namePath, filePath, projectID, parentID)
	symbol.DocString = j.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract enum constants
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil && member.Type() == "enum_constant" {
				constNameNode := member.ChildByFieldName("name")
				if constNameNode != nil {
					constName := GetNodeContent(constNameNode, sourceCode)
					constPath := j.BuildNamePath(namePath, constName)
					constSymbol := j.CreateSymbol(member, sourceCode, SymbolTypeEnumMember, constName, constPath, filePath, projectID, &symbol.ID)
					symbol.Children = append(symbol.Children, constSymbol)
					symbols = append(symbols, constSymbol)
				}
			}
		}
	}

	return symbols
}

// extractMethod extracts a method declaration
func (j *JavaExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := j.BuildNamePath(parentPath, name)

	symbol := j.CreateSymbol(node, sourceCode, SymbolTypeMethod, name, namePath, filePath, projectID, parentID)
	symbol.DocString = j.ExtractDocString(node, sourceCode)
	symbol.Signature = j.extractMethodSignature(node, sourceCode)

	return symbol
}

// extractConstructor extracts a constructor declaration
func (j *JavaExtractor) extractConstructor(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := j.BuildNamePath(parentPath, name)

	symbol := j.CreateSymbol(node, sourceCode, SymbolTypeConstructor, name, namePath, filePath, projectID, parentID)
	symbol.DocString = j.ExtractDocString(node, sourceCode)

	return symbol
}

// extractField extracts field declarations
func (j *JavaExtractor) extractField(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Check for static final (constant)
	isConstant := false
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Type() == "modifiers" {
			modText := GetNodeContent(child, sourceCode)
			if contains(modText, "static") && contains(modText, "final") {
				isConstant = true
				break
			}
		}
	}

	// Find variable declarators
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Type() == "variable_declarator" {
			nameNode := child.ChildByFieldName("name")
			if nameNode != nil {
				name := GetNodeContent(nameNode, sourceCode)
				namePath := j.BuildNamePath(parentPath, name)

				symbolType := SymbolTypeField
				if isConstant {
					symbolType = SymbolTypeConstant
				}

				symbol := j.CreateSymbol(child, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
				symbols = append(symbols, symbol)
			}
		}
	}

	return symbols
}

// extractMethodSignature extracts the method signature
func (j *JavaExtractor) extractMethodSignature(node *sitter.Node, sourceCode []byte) string {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return ""
	}

	// Get from return type to end of parameters
	var startByte, endByte uint32

	// Find return type or name
	typeNode := node.ChildByFieldName("type")
	nameNode := node.ChildByFieldName("name")

	if typeNode != nil {
		startByte = typeNode.StartByte()
	} else if nameNode != nil {
		startByte = nameNode.StartByte()
	} else {
		startByte = node.StartByte()
	}

	endByte = params.EndByte()

	if int(endByte) > len(sourceCode) {
		endByte = uint32(len(sourceCode))
	}

	return string(sourceCode[startByte:endByte])
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
