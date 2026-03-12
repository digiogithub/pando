// Package treesitter provides Go language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// GoExtractor extracts symbols from Go source code
type GoExtractor struct {
	BaseExtractor
}

// NewGoExtractor creates a new Go extractor
func NewGoExtractor(config WalkerConfig) *GoExtractor {
	return &GoExtractor{
		BaseExtractor: NewBaseExtractor(LanguageGo, config),
	}
}

// GetSymbolTypes returns the types of symbols the Go extractor can find
func (g *GoExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypePackage,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeStruct,
		SymbolTypeInterface,
		SymbolTypeTypeAlias,
		SymbolTypeConstant,
		SymbolTypeVariable,
	}
}

// ExtractSymbols extracts all symbols from Go source code
func (g *GoExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	// Extract package declaration
	if pkgSymbol := g.extractPackage(root, sourceCode, filePath, projectID); pkgSymbol != nil {
		symbols = append(symbols, pkgSymbol)
	}

	// Walk top-level declarations
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := g.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractPackage extracts the package declaration
func (g *GoExtractor) extractPackage(root *sitter.Node, sourceCode []byte, filePath string, projectID string) *CodeSymbol {
	pkgClause := FindChildByType(root, "package_clause")
	if pkgClause == nil {
		return nil
	}

	nameNode := FindChildByType(pkgClause, "package_identifier")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	return g.CreateSymbol(pkgClause, sourceCode, SymbolTypePackage, name, "/"+name, filePath, projectID, nil)
}

// extractNode extracts symbols from a node
func (g *GoExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "function_declaration":
		if symbol := g.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "method_declaration":
		if symbol := g.extractMethod(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "type_declaration":
		typeSymbols := g.extractTypeDeclaration(node, sourceCode, filePath, projectID, parentPath, parentID)
		symbols = append(symbols, typeSymbols...)

	case "const_declaration", "var_declaration":
		constSymbols := g.extractVarOrConst(node, sourceCode, filePath, projectID, parentPath, parentID)
		symbols = append(symbols, constSymbols...)
	}

	return symbols
}

// extractFunction extracts a function declaration
func (g *GoExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := g.BuildNamePath(parentPath, name)

	symbol := g.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.Signature = g.extractFunctionSignature(node, sourceCode)
	symbol.DocString = g.ExtractDocString(node, sourceCode)

	return symbol
}

// extractMethod extracts a method declaration
func (g *GoExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)

	// Get receiver type for name path
	receiverType := ""
	receiverNode := node.ChildByFieldName("receiver")
	if receiverNode != nil {
		// Find the type in the receiver parameter list
		for i := 0; i < int(receiverNode.NamedChildCount()); i++ {
			param := receiverNode.NamedChild(i)
			if param != nil && param.Type() == "parameter_declaration" {
				typeNode := param.ChildByFieldName("type")
				if typeNode != nil {
					receiverType = g.getTypeName(typeNode, sourceCode)
					break
				}
			}
		}
	}

	var namePath string
	if receiverType != "" {
		namePath = g.BuildNamePath(parentPath, receiverType+"."+name)
	} else {
		namePath = g.BuildNamePath(parentPath, name)
	}

	symbol := g.CreateSymbol(node, sourceCode, SymbolTypeMethod, name, namePath, filePath, projectID, parentID)
	symbol.Signature = g.extractFunctionSignature(node, sourceCode)
	symbol.DocString = g.ExtractDocString(node, sourceCode)

	// Store receiver info in metadata
	if receiverType != "" {
		symbol.Metadata = map[string]interface{}{
			"receiver_type": receiverType,
		}
	}

	return symbol
}

// extractTypeDeclaration extracts type declarations (struct, interface, alias)
func (g *GoExtractor) extractTypeDeclaration(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Handle type specs within the declaration
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Type() != "type_spec" {
			continue
		}

		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := g.BuildNamePath(parentPath, name)

		// Determine type (struct, interface, or alias)
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}

		var symbolType SymbolType
		switch typeNode.Type() {
		case "struct_type":
			symbolType = SymbolTypeStruct
		case "interface_type":
			symbolType = SymbolTypeInterface
		default:
			symbolType = SymbolTypeTypeAlias
		}

		symbol := g.CreateSymbol(child, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
		symbol.DocString = g.ExtractDocString(child, sourceCode)
		symbols = append(symbols, symbol)

		// Extract struct fields or interface methods
		if symbolType == SymbolTypeStruct {
			fieldSymbols := g.extractStructFields(typeNode, sourceCode, filePath, projectID, namePath, &symbol.ID)
			symbol.Children = fieldSymbols
			symbols = append(symbols, fieldSymbols...)
		} else if symbolType == SymbolTypeInterface {
			methodSymbols := g.extractInterfaceMethods(typeNode, sourceCode, filePath, projectID, namePath, &symbol.ID)
			symbol.Children = methodSymbols
			symbols = append(symbols, methodSymbols...)
		}
	}

	return symbols
}

// extractStructFields extracts fields from a struct type
func (g *GoExtractor) extractStructFields(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	fieldList := FindChildByType(node, "field_declaration_list")
	if fieldList == nil {
		return symbols
	}

	for i := 0; i < int(fieldList.NamedChildCount()); i++ {
		field := fieldList.NamedChild(i)
		if field == nil || field.Type() != "field_declaration" {
			continue
		}

		// Get field names (can be multiple in Go)
		for j := 0; j < int(field.NamedChildCount()); j++ {
			nameNode := field.NamedChild(j)
			if nameNode != nil && nameNode.Type() == "field_identifier" {
				name := GetNodeContent(nameNode, sourceCode)
				namePath := g.BuildNamePath(parentPath, name)

				symbol := g.CreateSymbol(field, sourceCode, SymbolTypeField, name, namePath, filePath, projectID, parentID)
				symbols = append(symbols, symbol)
			}
		}
	}

	return symbols
}

// extractInterfaceMethods extracts method signatures from an interface type
func (g *GoExtractor) extractInterfaceMethods(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		if child.Type() == "method_spec" {
			nameNode := child.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}

			name := GetNodeContent(nameNode, sourceCode)
			namePath := g.BuildNamePath(parentPath, name)

			symbol := g.CreateSymbol(child, sourceCode, SymbolTypeMethod, name, namePath, filePath, projectID, parentID)
			symbol.Signature = GetNodeContent(child, sourceCode)
			symbols = append(symbols, symbol)
		}
	}

	return symbols
}

// extractVarOrConst extracts variable or constant declarations
func (g *GoExtractor) extractVarOrConst(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	symbolType := SymbolTypeVariable
	if node.Type() == "const_declaration" {
		symbolType = SymbolTypeConstant
	}

	// Handle each spec in the declaration
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		specType := child.Type()
		if specType != "const_spec" && specType != "var_spec" {
			continue
		}

		// Get all identifiers in the spec
		for j := 0; j < int(child.NamedChildCount()); j++ {
			nameNode := child.NamedChild(j)
			if nameNode != nil && nameNode.Type() == "identifier" {
				name := GetNodeContent(nameNode, sourceCode)
				namePath := g.BuildNamePath(parentPath, name)

				symbol := g.CreateSymbol(child, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
				symbol.DocString = g.ExtractDocString(child, sourceCode)
				symbols = append(symbols, symbol)
			}
		}
	}

	return symbols
}

// extractFunctionSignature extracts the function signature
func (g *GoExtractor) extractFunctionSignature(node *sitter.Node, sourceCode []byte) string {
	// Get from start of node to end of parameters/result
	var endByte uint32

	// Find result or parameters
	result := node.ChildByFieldName("result")
	params := node.ChildByFieldName("parameters")

	if result != nil {
		endByte = result.EndByte()
	} else if params != nil {
		endByte = params.EndByte()
	} else {
		// No parameters, use name end
		name := node.ChildByFieldName("name")
		if name != nil {
			endByte = name.EndByte()
		} else {
			return ""
		}
	}

	startByte := node.StartByte()
	if int(endByte) > len(sourceCode) {
		endByte = uint32(len(sourceCode))
	}

	return string(sourceCode[startByte:endByte])
}

// getTypeName extracts the type name, handling pointers
func (g *GoExtractor) getTypeName(node *sitter.Node, sourceCode []byte) string {
	switch node.Type() {
	case "pointer_type":
		child := node.NamedChild(0)
		if child != nil {
			return g.getTypeName(child, sourceCode)
		}
	case "type_identifier":
		return GetNodeContent(node, sourceCode)
	case "qualified_type":
		return GetNodeContent(node, sourceCode)
	}
	return GetNodeContent(node, sourceCode)
}
