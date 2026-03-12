// Package treesitter provides Python language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// PythonExtractor extracts symbols from Python source code
type PythonExtractor struct {
	BaseExtractor
}

// NewPythonExtractor creates a new Python extractor
func NewPythonExtractor(config WalkerConfig) *PythonExtractor {
	return &PythonExtractor{
		BaseExtractor: NewBaseExtractor(LanguagePython, config),
	}
}

// GetSymbolTypes returns the types of symbols the Python extractor can find
func (p *PythonExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeProperty,
		SymbolTypeVariable,
	}
}

// ExtractSymbols extracts all symbols from Python source code
func (p *PythonExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

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
func (p *PythonExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "class_definition":
		symbols = append(symbols, p.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "function_definition":
		symbols = append(symbols, p.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "decorated_definition":
		symbols = append(symbols, p.extractDecorated(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "expression_statement":
		if symbol := p.extractAssignment(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}
	}

	return symbols
}

// extractClass extracts a class definition
func (p *PythonExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbol := p.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.extractPythonDocString(node, sourceCode)
	symbol.Signature = p.extractClassSignature(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract class body
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			child := body.NamedChild(i)
			if child == nil {
				continue
			}

			childSymbols := p.extractClassMember(child, sourceCode, filePath, projectID, namePath, &symbol.ID)
			for _, cs := range childSymbols {
				symbol.Children = append(symbol.Children, cs)
			}
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// extractClassMember extracts a member from a class body
func (p *PythonExtractor) extractClassMember(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	switch node.Type() {
	case "function_definition":
		return p.extractMethod(node, sourceCode, filePath, projectID, parentPath, parentID)

	case "decorated_definition":
		return p.extractDecorated(node, sourceCode, filePath, projectID, parentPath, parentID)

	case "expression_statement":
		if symbol := p.extractClassVariable(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			return []*CodeSymbol{symbol}
		}
	}

	return nil
}

// extractFunction extracts a function definition (module level)
func (p *PythonExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbol := p.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.extractPythonDocString(node, sourceCode)
	symbol.Signature = p.extractFunctionSignature(node, sourceCode)
	symbols = append(symbols, symbol)

	return symbols
}

// extractMethod extracts a method from a class
func (p *PythonExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	symbolType := SymbolTypeMethod
	// Check if it's a property by looking at decorators
	if p.hasDecorator(node, "property", sourceCode) || p.hasDecorator(node, "cached_property", sourceCode) {
		symbolType = SymbolTypeProperty
	}

	symbol := p.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.extractPythonDocString(node, sourceCode)
	symbol.Signature = p.extractFunctionSignature(node, sourceCode)
	symbols = append(symbols, symbol)

	return symbols
}

// extractDecorated extracts a decorated definition
func (p *PythonExtractor) extractDecorated(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	definition := node.ChildByFieldName("definition")
	if definition == nil {
		return nil
	}

	switch definition.Type() {
	case "class_definition":
		return p.extractClass(definition, sourceCode, filePath, projectID, parentPath, parentID)

	case "function_definition":
		// Check for property decorator
		if p.nodeHasPropertyDecorator(node, sourceCode) {
			return p.extractPropertyFromDecorated(node, definition, sourceCode, filePath, projectID, parentPath, parentID)
		}
		// Check if this is at module level or class level
		if parentID != nil {
			return p.extractMethod(definition, sourceCode, filePath, projectID, parentPath, parentID)
		}
		return p.extractFunction(definition, sourceCode, filePath, projectID, parentPath, parentID)
	}

	return nil
}

// nodeHasPropertyDecorator checks if the decorated node has a property decorator
func (p *PythonExtractor) nodeHasPropertyDecorator(node *sitter.Node, sourceCode []byte) bool {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child != nil && child.Type() == "decorator" {
			// The decorator can be an identifier or a call
			for j := 0; j < int(child.NamedChildCount()); j++ {
				decoratorChild := child.NamedChild(j)
				if decoratorChild == nil {
					continue
				}
				if decoratorChild.Type() == "identifier" {
					name := GetNodeContent(decoratorChild, sourceCode)
					if name == "property" || name == "cached_property" {
						return true
					}
				}
			}
		}
	}
	return false
}

// extractPropertyFromDecorated extracts a property from a decorated function
func (p *PythonExtractor) extractPropertyFromDecorated(decoratedNode *sitter.Node, funcNode *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := funcNode.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := p.BuildNamePath(parentPath, name)

	// Use the decorated node for location
	symbol := p.CreateSymbol(decoratedNode, sourceCode, SymbolTypeProperty, name, namePath, filePath, projectID, parentID)
	symbol.DocString = p.extractPythonDocString(funcNode, sourceCode)
	symbols = append(symbols, symbol)

	return symbols
}

// hasDecorator checks if a function has a specific decorator
func (p *PythonExtractor) hasDecorator(node *sitter.Node, decoratorName string, sourceCode []byte) bool {
	// For decorated_definition, decorators are siblings before the function
	parent := node.Parent()
	if parent != nil && parent.Type() == "decorated_definition" {
		for i := 0; i < int(parent.NamedChildCount()); i++ {
			child := parent.NamedChild(i)
			if child != nil && child.Type() == "decorator" {
				for j := 0; j < int(child.NamedChildCount()); j++ {
					decoratorChild := child.NamedChild(j)
					if decoratorChild != nil && decoratorChild.Type() == "identifier" {
						if GetNodeContent(decoratorChild, sourceCode) == decoratorName {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// extractAssignment extracts a module-level variable assignment
func (p *PythonExtractor) extractAssignment(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Look for assignment within the expression statement
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		if child.Type() == "assignment" {
			leftNode := child.ChildByFieldName("left")
			if leftNode != nil && leftNode.Type() == "identifier" {
				name := GetNodeContent(leftNode, sourceCode)
				namePath := p.BuildNamePath(parentPath, name)

				symbol := p.CreateSymbol(child, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
				return symbol
			}
		}
	}

	return nil
}

// extractClassVariable extracts a class variable assignment
func (p *PythonExtractor) extractClassVariable(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	// Similar to extractAssignment but for class context
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		switch child.Type() {
		case "assignment":
			leftNode := child.ChildByFieldName("left")
			if leftNode != nil && leftNode.Type() == "identifier" {
				name := GetNodeContent(leftNode, sourceCode)
				namePath := p.BuildNamePath(parentPath, name)

				symbol := p.CreateSymbol(child, sourceCode, SymbolTypeProperty, name, namePath, filePath, projectID, parentID)
				return symbol
			}

		case "type":
			// Type annotated variable: name: type = value
			leftNode := child.ChildByFieldName("left")
			if leftNode != nil && leftNode.Type() == "identifier" {
				name := GetNodeContent(leftNode, sourceCode)
				namePath := p.BuildNamePath(parentPath, name)

				symbol := p.CreateSymbol(child, sourceCode, SymbolTypeProperty, name, namePath, filePath, projectID, parentID)
				return symbol
			}
		}
	}

	return nil
}

// extractPythonDocString extracts a Python docstring from a function/class
func (p *PythonExtractor) extractPythonDocString(node *sitter.Node, sourceCode []byte) string {
	body := node.ChildByFieldName("body")
	if body == nil {
		return ""
	}

	// Python docstrings are the first expression in the body if it's a string
	if body.NamedChildCount() > 0 {
		firstStmt := body.NamedChild(0)
		if firstStmt != nil && firstStmt.Type() == "expression_statement" {
			if firstStmt.NamedChildCount() > 0 {
				expr := firstStmt.NamedChild(0)
				if expr != nil && (expr.Type() == "string" || expr.Type() == "concatenated_string") {
					docstring := GetNodeContent(expr, sourceCode)
					// Remove quotes
					docstring = p.cleanDocString(docstring)
					return docstring
				}
			}
		}
	}

	return ""
}

// cleanDocString removes surrounding quotes from a docstring
func (p *PythonExtractor) cleanDocString(docstring string) string {
	if len(docstring) < 6 {
		return docstring
	}

	// Handle triple-quoted strings
	if (docstring[:3] == `"""` && docstring[len(docstring)-3:] == `"""`) ||
		(docstring[:3] == "'''" && docstring[len(docstring)-3:] == "'''") {
		return docstring[3 : len(docstring)-3]
	}

	// Handle single-quoted strings
	if len(docstring) >= 2 {
		if (docstring[0] == '"' && docstring[len(docstring)-1] == '"') ||
			(docstring[0] == '\'' && docstring[len(docstring)-1] == '\'') {
			return docstring[1 : len(docstring)-1]
		}
	}

	return docstring
}

// extractFunctionSignature extracts the function signature
func (p *PythonExtractor) extractFunctionSignature(node *sitter.Node, sourceCode []byte) string {
	nameNode := node.ChildByFieldName("name")
	params := node.ChildByFieldName("parameters")
	returnType := node.ChildByFieldName("return_type")

	if nameNode == nil {
		return ""
	}

	sig := "def " + GetNodeContent(nameNode, sourceCode)

	if params != nil {
		sig += GetNodeContent(params, sourceCode)
	} else {
		sig += "()"
	}

	if returnType != nil {
		sig += " -> " + GetNodeContent(returnType, sourceCode)
	}

	return sig
}

// extractClassSignature extracts the class signature
func (p *PythonExtractor) extractClassSignature(node *sitter.Node, sourceCode []byte) string {
	nameNode := node.ChildByFieldName("name")
	superclass := node.ChildByFieldName("superclasses")

	if nameNode == nil {
		return ""
	}

	sig := "class " + GetNodeContent(nameNode, sourceCode)

	if superclass != nil {
		sig += GetNodeContent(superclass, sourceCode)
	}

	return sig
}
