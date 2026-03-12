// Package treesitter provides Lua language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// LuaExtractor extracts symbols from Lua source code
type LuaExtractor struct {
	BaseExtractor
}

// NewLuaExtractor creates a new Lua extractor
func NewLuaExtractor(config WalkerConfig) *LuaExtractor {
	return &LuaExtractor{
		BaseExtractor: NewBaseExtractor(LanguageLua, config),
	}
}

// GetSymbolTypes returns the types of symbols the Lua extractor can find
func (l *LuaExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeFunction,
		SymbolTypeVariable,
		SymbolTypeConstant,
	}
}

// ExtractSymbols extracts all symbols from Lua source code
func (l *LuaExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := l.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (l *LuaExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "function_declaration", "function_definition":
		if symbol := l.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "local_function":
		if symbol := l.extractLocalFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "assignment_statement", "variable_declaration":
		symbols = append(symbols, l.extractAssignment(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "local_declaration":
		symbols = append(symbols, l.extractLocalDeclaration(node, sourceCode, filePath, projectID, parentPath, parentID)...)
	}

	return symbols
}

// extractFunction extracts a global function declaration
func (l *LuaExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Try to get function name from different possible fields
	var nameNode *sitter.Node
	var name string

	// Check for 'name' field first
	nameNode = node.ChildByFieldName("name")
	if nameNode != nil {
		name = GetNodeContent(nameNode, sourceCode)
	} else {
		// Try to find identifier as direct child
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child != nil && (child.Type() == "identifier" || child.Type() == "dot_index_expression" || child.Type() == "method_index_expression") {
				name = GetNodeContent(child, sourceCode)
				nameNode = child
				break
			}
		}
	}

	if name == "" || nameNode == nil {
		return nil
	}

	namePath := l.BuildNamePath(parentPath, name)
	symbol := l.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.Signature = l.extractFunctionSignature(node, sourceCode)

	return symbol
}

// extractLocalFunction extracts a local function declaration
func (l *LuaExtractor) extractLocalFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		// Try to find identifier as direct child
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child != nil && child.Type() == "identifier" {
				nameNode = child
				break
			}
		}
	}

	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := l.BuildNamePath(parentPath, name)

	symbol := l.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.Signature = l.extractFunctionSignature(node, sourceCode)

	return symbol
}

// extractAssignment extracts variable assignments
func (l *LuaExtractor) extractAssignment(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Look for variable_list or assignment targets
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		// Check for variable_list or direct identifiers
		if child.Type() == "variable_list" || child.Type() == "identifier" {
			varNode := child
			if child.Type() == "variable_list" {
				// Extract each variable from the list
				for j := 0; j < int(child.NamedChildCount()); j++ {
					varChild := child.NamedChild(j)
					if varChild != nil && varChild.Type() == "identifier" {
						varNode = varChild
						name := GetNodeContent(varNode, sourceCode)
						namePath := l.BuildNamePath(parentPath, name)
						symbol := l.CreateSymbol(node, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
						symbols = append(symbols, symbol)
					}
				}
			} else if child.Type() == "identifier" {
				name := GetNodeContent(child, sourceCode)
				namePath := l.BuildNamePath(parentPath, name)
				symbol := l.CreateSymbol(node, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
				symbols = append(symbols, symbol)
			}
		}
	}

	return symbols
}

// extractLocalDeclaration extracts local variable declarations
func (l *LuaExtractor) extractLocalDeclaration(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Look for variable_list in local declarations
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		if child.Type() == "variable_list" || child.Type() == "identifier" {
			if child.Type() == "variable_list" {
				// Extract each variable from the list
				for j := 0; j < int(child.NamedChildCount()); j++ {
					varChild := child.NamedChild(j)
					if varChild != nil && varChild.Type() == "identifier" {
						name := GetNodeContent(varChild, sourceCode)
						namePath := l.BuildNamePath(parentPath, name)
						symbol := l.CreateSymbol(node, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
						symbols = append(symbols, symbol)
					}
				}
			} else if child.Type() == "identifier" {
				name := GetNodeContent(child, sourceCode)
				namePath := l.BuildNamePath(parentPath, name)
				symbol := l.CreateSymbol(node, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
				symbols = append(symbols, symbol)
			}
		}
	}

	return symbols
}

// extractFunctionSignature extracts the function signature
func (l *LuaExtractor) extractFunctionSignature(node *sitter.Node, sourceCode []byte) string {
	// Try to get the function name
	var nameNode *sitter.Node
	var name string

	nameNode = node.ChildByFieldName("name")
	if nameNode != nil {
		name = GetNodeContent(nameNode, sourceCode)
	} else {
		// Try to find identifier
		for i := 0; i < int(node.NamedChildCount()); i++ {
			child := node.NamedChild(i)
			if child != nil && (child.Type() == "identifier" || child.Type() == "dot_index_expression" || child.Type() == "method_index_expression") {
				name = GetNodeContent(child, sourceCode)
				break
			}
		}
	}

	if name == "" {
		name = "function"
	}

	// Get parameters
	params := node.ChildByFieldName("parameters")
	if params != nil {
		return "function " + name + GetNodeContent(params, sourceCode)
	}

	return "function " + name + "()"
}
