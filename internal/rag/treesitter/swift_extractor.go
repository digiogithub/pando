// Package treesitter provides Swift language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// SwiftExtractor extracts symbols from Swift source code
type SwiftExtractor struct {
	BaseExtractor
}

// NewSwiftExtractor creates a new Swift extractor
func NewSwiftExtractor(config WalkerConfig) *SwiftExtractor {
	return &SwiftExtractor{
		BaseExtractor: NewBaseExtractor(LanguageSwift, config),
	}
}

// GetSymbolTypes returns the types of symbols the Swift extractor can find
func (s *SwiftExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeStruct,
		SymbolTypeEnum,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeProperty,
		SymbolTypeConstant,
		SymbolTypeTypeAlias,
	}
}

// ExtractSymbols extracts all symbols from Swift source code
func (s *SwiftExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := s.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (s *SwiftExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "class_declaration":
		symbols = append(symbols, s.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "struct_declaration":
		symbols = append(symbols, s.extractStruct(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "enum_declaration":
		symbols = append(symbols, s.extractEnum(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "protocol_declaration":
		if symbol := s.extractProtocol(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "function_declaration":
		if symbol := s.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "property_declaration":
		symbols = append(symbols, s.extractProperty(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "typealias_declaration":
		if symbol := s.extractTypealias(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "extension_declaration":
		symbols = append(symbols, s.extractExtension(node, sourceCode, filePath, projectID, parentPath, parentID)...)
	}

	return symbols
}

// extractClass extracts a class declaration
func (s *SwiftExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
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

	// Extract class body
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil {
				memberSymbols := s.extractNode(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, memberSymbols...)
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}

// extractStruct extracts a struct declaration
func (s *SwiftExtractor) extractStruct(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeStruct, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract struct body
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil {
				memberSymbols := s.extractNode(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, memberSymbols...)
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}

// extractEnum extracts an enum declaration
func (s *SwiftExtractor) extractEnum(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeEnum, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract enum cases
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil && member.Type() == "enum_entry" {
				caseNameNode := member.ChildByFieldName("name")
				if caseNameNode != nil {
					caseName := GetNodeContent(caseNameNode, sourceCode)
					casePath := s.BuildNamePath(namePath, caseName)
					caseSymbol := s.CreateSymbol(member, sourceCode, SymbolTypeEnumMember, caseName, casePath, filePath, projectID, &symbol.ID)
					symbol.Children = append(symbol.Children, caseSymbol)
					symbols = append(symbols, caseSymbol)
				}
			}
		}
	}

	return symbols
}

// extractProtocol extracts a protocol declaration
func (s *SwiftExtractor) extractProtocol(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeInterface, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)

	return symbol
}

// extractFunction extracts a function declaration
func (s *SwiftExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbolType := SymbolTypeFunction
	if parentID != nil {
		symbolType = SymbolTypeMethod
	}

	symbol := s.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)

	return symbol
}

// extractProperty extracts a property declaration
func (s *SwiftExtractor) extractProperty(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Find pattern for name
	pattern := FindChildByType(node, "pattern")
	if pattern == nil {
		return symbols
	}

	nameNode := FindChildByType(pattern, "simple_identifier")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	// Check if let (constant) or var
	symbolType := SymbolTypeProperty
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && GetNodeContent(child, sourceCode) == "let" {
			symbolType = SymbolTypeConstant
			break
		}
	}

	symbol := s.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	return symbols
}

// extractTypealias extracts a typealias declaration
func (s *SwiftExtractor) extractTypealias(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeTypeAlias, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)

	return symbol
}

// extractExtension extracts an extension declaration
func (s *SwiftExtractor) extractExtension(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Get the type being extended
	typeNode := node.ChildByFieldName("name")
	if typeNode == nil {
		return symbols
	}

	typeName := GetNodeContent(typeNode, sourceCode)
	extPath := s.BuildNamePath(parentPath, typeName)

	// Extract extension body members
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member != nil {
				memberSymbols := s.extractNode(member, sourceCode, filePath, projectID, extPath, parentID)
				for _, sym := range memberSymbols {
					sym.Metadata = map[string]interface{}{
						"extension_of": typeName,
					}
				}
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}
