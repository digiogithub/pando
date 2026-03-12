// Package treesitter provides C language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// CExtractor extracts symbols from C source code
type CExtractor struct {
	BaseExtractor
}

// NewCExtractor creates a new C extractor
func NewCExtractor(config WalkerConfig) *CExtractor {
	return &CExtractor{
		BaseExtractor: NewBaseExtractor(LanguageC, config),
	}
}

// GetSymbolTypes returns the types of symbols the C extractor can find
func (c *CExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeFunction,
		SymbolTypeStruct,
		SymbolTypeEnum,
		SymbolTypeTypeAlias,
		SymbolTypeVariable,
		SymbolTypeConstant,
	}
}

// ExtractSymbols extracts all symbols from C source code
func (c *CExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := c.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (c *CExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "function_definition":
		if symbol := c.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "declaration":
		symbols = append(symbols, c.extractDeclaration(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "struct_specifier":
		if symbol := c.extractStruct(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "enum_specifier":
		symbols = append(symbols, c.extractEnum(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "type_definition":
		if symbol := c.extractTypedef(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "preproc_def":
		if symbol := c.extractMacro(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}
	}

	return symbols
}

// extractFunction extracts a function definition
func (c *CExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	declarator := node.ChildByFieldName("declarator")
	if declarator == nil {
		return nil
	}

	name := c.getFunctionName(declarator, sourceCode)
	if name == "" {
		return nil
	}

	namePath := c.BuildNamePath(parentPath, name)

	symbol := c.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.DocString = c.ExtractDocString(node, sourceCode)
	symbol.Signature = c.extractFunctionSignature(node, sourceCode)

	return symbol
}

// getFunctionName extracts the function name from a declarator
func (c *CExtractor) getFunctionName(node *sitter.Node, sourceCode []byte) string {
	switch node.Type() {
	case "function_declarator":
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			return c.getFunctionName(declarator, sourceCode)
		}
	case "pointer_declarator":
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			return c.getFunctionName(declarator, sourceCode)
		}
	case "identifier":
		return GetNodeContent(node, sourceCode)
	}
	return ""
}

// extractDeclaration extracts variable/function declarations
func (c *CExtractor) extractDeclaration(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Check for function declarations vs variable declarations
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		switch child.Type() {
		case "function_declarator":
			name := c.getFunctionName(child, sourceCode)
			if name != "" {
				namePath := c.BuildNamePath(parentPath, name)
				symbol := c.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
				symbols = append(symbols, symbol)
			}

		case "init_declarator":
			declarator := child.ChildByFieldName("declarator")
			if declarator != nil {
				name := c.getDeclaratorName(declarator, sourceCode)
				if name != "" {
					namePath := c.BuildNamePath(parentPath, name)
					symbol := c.CreateSymbol(child, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
					symbols = append(symbols, symbol)
				}
			}

		case "identifier", "pointer_declarator":
			name := c.getDeclaratorName(child, sourceCode)
			if name != "" {
				namePath := c.BuildNamePath(parentPath, name)
				symbol := c.CreateSymbol(node, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
				symbols = append(symbols, symbol)
			}
		}
	}

	return symbols
}

// getDeclaratorName gets the name from various declarator types
func (c *CExtractor) getDeclaratorName(node *sitter.Node, sourceCode []byte) string {
	switch node.Type() {
	case "identifier":
		return GetNodeContent(node, sourceCode)
	case "pointer_declarator":
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			return c.getDeclaratorName(declarator, sourceCode)
		}
	case "array_declarator":
		declarator := node.ChildByFieldName("declarator")
		if declarator != nil {
			return c.getDeclaratorName(declarator, sourceCode)
		}
	}
	return ""
}

// extractStruct extracts a struct definition
func (c *CExtractor) extractStruct(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := c.BuildNamePath(parentPath, name)

	symbol := c.CreateSymbol(node, sourceCode, SymbolTypeStruct, name, namePath, filePath, projectID, parentID)
	symbol.DocString = c.ExtractDocString(node, sourceCode)

	return symbol
}

// extractEnum extracts an enum definition
func (c *CExtractor) extractEnum(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := c.BuildNamePath(parentPath, name)

	symbol := c.CreateSymbol(node, sourceCode, SymbolTypeEnum, name, namePath, filePath, projectID, parentID)
	symbol.DocString = c.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract enum values
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			enumerator := body.NamedChild(i)
			if enumerator == nil || enumerator.Type() != "enumerator" {
				continue
			}

			enumNameNode := enumerator.ChildByFieldName("name")
			if enumNameNode != nil {
				enumName := GetNodeContent(enumNameNode, sourceCode)
				enumPath := c.BuildNamePath(namePath, enumName)
				enumSymbol := c.CreateSymbol(enumerator, sourceCode, SymbolTypeEnumMember, enumName, enumPath, filePath, projectID, &symbol.ID)
				symbol.Children = append(symbol.Children, enumSymbol)
				symbols = append(symbols, enumSymbol)
			}
		}
	}

	return symbols
}

// extractTypedef extracts a typedef
func (c *CExtractor) extractTypedef(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	declarator := node.ChildByFieldName("declarator")
	if declarator == nil {
		return nil
	}

	name := c.getDeclaratorName(declarator, sourceCode)
	if name == "" {
		return nil
	}

	namePath := c.BuildNamePath(parentPath, name)

	symbol := c.CreateSymbol(node, sourceCode, SymbolTypeTypeAlias, name, namePath, filePath, projectID, parentID)
	symbol.DocString = c.ExtractDocString(node, sourceCode)

	return symbol
}

// extractMacro extracts a preprocessor macro definition
func (c *CExtractor) extractMacro(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := c.BuildNamePath(parentPath, name)

	symbol := c.CreateSymbol(node, sourceCode, SymbolTypeConstant, name, namePath, filePath, projectID, parentID)

	return symbol
}

// extractFunctionSignature extracts the function signature
func (c *CExtractor) extractFunctionSignature(node *sitter.Node, sourceCode []byte) string {
	declarator := node.ChildByFieldName("declarator")
	if declarator == nil {
		return ""
	}

	// Get from return type to end of parameters
	typeNode := node.ChildByFieldName("type")
	var startByte uint32
	if typeNode != nil {
		startByte = typeNode.StartByte()
	} else {
		startByte = node.StartByte()
	}

	endByte := declarator.EndByte()
	if int(endByte) > len(sourceCode) {
		endByte = uint32(len(sourceCode))
	}

	return string(sourceCode[startByte:endByte])
}
