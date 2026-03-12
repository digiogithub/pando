// Package treesitter provides TypeScript language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// TypeScriptExtractor extracts symbols from TypeScript source code
type TypeScriptExtractor struct {
	BaseExtractor
}

// NewTypeScriptExtractor creates a new TypeScript extractor
func NewTypeScriptExtractor(config WalkerConfig) *TypeScriptExtractor {
	return &TypeScriptExtractor{
		BaseExtractor: NewBaseExtractor(LanguageTypeScript, config),
	}
}

// GetSymbolTypes returns the types of symbols the TypeScript extractor can find
func (t *TypeScriptExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeInterface,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeProperty,
		SymbolTypeVariable,
		SymbolTypeConstant,
		SymbolTypeEnum,
		SymbolTypeTypeAlias,
		SymbolTypeNamespace,
	}
}

// ExtractSymbols extracts all symbols from TypeScript source code
func (t *TypeScriptExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	// Walk top-level declarations
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
func (t *TypeScriptExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "class_declaration":
		symbols = append(symbols, t.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "interface_declaration":
		if symbol := t.extractInterface(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "function_declaration":
		if symbol := t.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "arrow_function":
		if symbol := t.extractArrowFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "lexical_declaration", "variable_declaration":
		symbols = append(symbols, t.extractVariableDeclaration(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "enum_declaration":
		symbols = append(symbols, t.extractEnum(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "type_alias_declaration":
		if symbol := t.extractTypeAlias(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "module", "internal_module":
		symbols = append(symbols, t.extractNamespace(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "export_statement":
		// Extract the declaration within export
		declaration := node.ChildByFieldName("declaration")
		if declaration != nil {
			symbols = append(symbols, t.extractNode(declaration, sourceCode, filePath, projectID, parentPath, parentID)...)
		}

	case "expression_statement":
		// Check for exported arrow functions assigned to variables
		expr := node.NamedChild(0)
		if expr != nil {
			symbols = append(symbols, t.extractNode(expr, sourceCode, filePath, projectID, parentPath, parentID)...)
		}
	}

	return symbols
}

// extractClass extracts a class declaration and its members
func (t *TypeScriptExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := t.BuildNamePath(parentPath, name)

	symbol := t.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = t.ExtractDocString(node, sourceCode)
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

		memberSymbols := t.extractClassMember(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
		symbol.Children = append(symbol.Children, memberSymbols...)
		symbols = append(symbols, memberSymbols...)
	}

	return symbols
}

// extractClassMember extracts a class member (method, property, constructor)
func (t *TypeScriptExtractor) extractClassMember(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "method_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return symbols
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := t.BuildNamePath(parentPath, name)

		symbolType := SymbolTypeMethod
		if name == "constructor" {
			symbolType = SymbolTypeConstructor
		}

		symbol := t.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
		symbol.Signature = t.extractMethodSignature(node, sourceCode)
		symbol.DocString = t.ExtractDocString(node, sourceCode)
		symbols = append(symbols, symbol)

	case "public_field_definition", "field_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return symbols
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := t.BuildNamePath(parentPath, name)

		symbol := t.CreateSymbol(node, sourceCode, SymbolTypeProperty, name, namePath, filePath, projectID, parentID)
		symbols = append(symbols, symbol)
	}

	return symbols
}

// extractInterface extracts an interface declaration
func (t *TypeScriptExtractor) extractInterface(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := t.BuildNamePath(parentPath, name)

	symbol := t.CreateSymbol(node, sourceCode, SymbolTypeInterface, name, namePath, filePath, projectID, parentID)
	symbol.DocString = t.ExtractDocString(node, sourceCode)

	return symbol
}

// extractFunction extracts a function declaration
func (t *TypeScriptExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := t.BuildNamePath(parentPath, name)

	symbol := t.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.Signature = t.extractFunctionSignature(node, sourceCode)
	symbol.DocString = t.ExtractDocString(node, sourceCode)

	return symbol
}

// extractArrowFunction extracts an arrow function (if named via assignment)
func (t *TypeScriptExtractor) extractArrowFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Arrow functions are usually extracted via variable declarations
	// This handles standalone arrow functions which are rare
	return nil
}

// extractVariableDeclaration extracts variable/const declarations
func (t *TypeScriptExtractor) extractVariableDeclaration(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	isConst := false
	// Check if const
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && GetNodeContent(child, sourceCode) == "const" {
			isConst = true
			break
		}
	}

	// Find variable declarators
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
		namePath := t.BuildNamePath(parentPath, name)

		symbolType := SymbolTypeVariable
		if isConst {
			symbolType = SymbolTypeConstant
		}

		// Check if value is an arrow function or function expression
		value := declarator.ChildByFieldName("value")
		if value != nil {
			if value.Type() == "arrow_function" || value.Type() == "function" {
				symbolType = SymbolTypeFunction
			}
		}

		symbol := t.CreateSymbol(declarator, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
		symbol.DocString = t.ExtractDocString(node, sourceCode)
		symbols = append(symbols, symbol)
	}

	return symbols
}

// extractEnum extracts an enum declaration
func (t *TypeScriptExtractor) extractEnum(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := t.BuildNamePath(parentPath, name)

	symbol := t.CreateSymbol(node, sourceCode, SymbolTypeEnum, name, namePath, filePath, projectID, parentID)
	symbol.DocString = t.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract enum members
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member == nil {
				continue
			}

			memberName := ""
			if member.Type() == "enum_assignment" {
				memberNameNode := member.ChildByFieldName("name")
				if memberNameNode != nil {
					memberName = GetNodeContent(memberNameNode, sourceCode)
				}
			} else if member.Type() == "property_identifier" {
				memberName = GetNodeContent(member, sourceCode)
			}

			if memberName != "" {
				memberPath := t.BuildNamePath(namePath, memberName)
				memberSymbol := t.CreateSymbol(member, sourceCode, SymbolTypeEnumMember, memberName, memberPath, filePath, projectID, &symbol.ID)
				symbol.Children = append(symbol.Children, memberSymbol)
				symbols = append(symbols, memberSymbol)
			}
		}
	}

	return symbols
}

// extractTypeAlias extracts a type alias declaration
func (t *TypeScriptExtractor) extractTypeAlias(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := t.BuildNamePath(parentPath, name)

	symbol := t.CreateSymbol(node, sourceCode, SymbolTypeTypeAlias, name, namePath, filePath, projectID, parentID)
	symbol.DocString = t.ExtractDocString(node, sourceCode)

	return symbol
}

// extractNamespace extracts a namespace/module declaration
func (t *TypeScriptExtractor) extractNamespace(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := t.BuildNamePath(parentPath, name)

	symbol := t.CreateSymbol(node, sourceCode, SymbolTypeNamespace, name, namePath, filePath, projectID, parentID)
	symbols = append(symbols, symbol)

	// Extract nested declarations
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			child := body.NamedChild(i)
			if child != nil {
				childSymbols := t.extractNode(child, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, childSymbols...)
				symbols = append(symbols, childSymbols...)
			}
		}
	}

	return symbols
}

// extractFunctionSignature extracts the function signature
func (t *TypeScriptExtractor) extractFunctionSignature(node *sitter.Node, sourceCode []byte) string {
	// Get from 'function' keyword to end of return type or parameters
	params := node.ChildByFieldName("parameters")
	returnType := node.ChildByFieldName("return_type")

	var endByte uint32
	if returnType != nil {
		endByte = returnType.EndByte()
	} else if params != nil {
		endByte = params.EndByte()
	} else {
		return ""
	}

	startByte := node.StartByte()
	if int(endByte) > len(sourceCode) {
		endByte = uint32(len(sourceCode))
	}

	return string(sourceCode[startByte:endByte])
}

// extractMethodSignature extracts a method signature
func (t *TypeScriptExtractor) extractMethodSignature(node *sitter.Node, sourceCode []byte) string {
	nameNode := node.ChildByFieldName("name")
	params := node.ChildByFieldName("parameters")
	returnType := node.ChildByFieldName("return_type")

	if nameNode == nil {
		return ""
	}

	var endByte uint32
	if returnType != nil {
		endByte = returnType.EndByte()
	} else if params != nil {
		endByte = params.EndByte()
	} else {
		endByte = nameNode.EndByte()
	}

	startByte := nameNode.StartByte()
	if int(endByte) > len(sourceCode) {
		endByte = uint32(len(sourceCode))
	}

	return string(sourceCode[startByte:endByte])
}
