// Package treesitter provides Vue language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// VueExtractor extracts symbols from Vue source code
type VueExtractor struct {
	BaseExtractor
}

// NewVueExtractor creates a new Vue extractor
func NewVueExtractor(config WalkerConfig) *VueExtractor {
	return &VueExtractor{
		BaseExtractor: NewBaseExtractor(LanguageVue, config),
	}
}

// GetSymbolTypes returns the types of symbols the Vue extractor can find
func (v *VueExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeVariable,
		SymbolTypeConstant,
		SymbolTypeProperty,
	}
}

// ExtractSymbols extracts all symbols from Vue source code
func (v *VueExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	// Vue files have <template>, <script>, and <style> sections
	// We focus on extracting symbols from <script> sections
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		// Look for script elements
		if child.Type() == "script_element" || child.Type() == "element" {
			childSymbols := v.extractScriptElement(child, sourceCode, filePath, projectID)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols, nil
}

// extractScriptElement extracts symbols from a Vue <script> section
func (v *VueExtractor) extractScriptElement(node *sitter.Node, sourceCode []byte, filePath string, projectID string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Check if this is actually a script element
	startTag := node.ChildByFieldName("start_tag")
	if startTag != nil {
		tagName := v.findTagName(startTag, sourceCode)
		if tagName != "script" {
			return symbols
		}
	}

	// Find the script content
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		// Look for raw_text or script content
		if child.Type() == "raw_text" || child.Type() == "script_content" {
			// Parse the content as JavaScript/TypeScript
			childSymbols := v.extractScriptContent(child, sourceCode, filePath, projectID)
			symbols = append(symbols, childSymbols...)
		}
	}

	return symbols
}

// findTagName extracts the tag name from a start tag
func (v *VueExtractor) findTagName(node *sitter.Node, sourceCode []byte) string {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil && (child.Type() == "tag_name" || child.Type() == "element_name") {
			return GetNodeContent(child, sourceCode)
		}
	}
	return ""
}

// extractScriptContent extracts symbols from script content
func (v *VueExtractor) extractScriptContent(node *sitter.Node, sourceCode []byte, filePath string, projectID string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Process child nodes (JavaScript/TypeScript code)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := v.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols
}

// extractNode extracts symbols from a node
func (v *VueExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "class_declaration":
		symbols = append(symbols, v.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "function_declaration":
		if symbol := v.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "lexical_declaration", "variable_declaration":
		symbols = append(symbols, v.extractVariableDeclaration(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "export_statement":
		declaration := node.ChildByFieldName("declaration")
		if declaration != nil {
			symbols = append(symbols, v.extractNode(declaration, sourceCode, filePath, projectID, parentPath, parentID)...)
		}

	case "export_default":
		// Vue components often export a default object
		value := node.ChildByFieldName("value")
		if value != nil && value.Type() == "object" {
			symbols = append(symbols, v.extractVueComponentObject(value, sourceCode, filePath, projectID, parentPath, parentID)...)
		}
	}

	return symbols
}

// extractVueComponentObject extracts properties from a Vue component object
func (v *VueExtractor) extractVueComponentObject(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Look for common Vue component properties like data, methods, computed, etc.
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Type() != "pair" {
			continue
		}

		keyNode := child.ChildByFieldName("key")
		valueNode := child.ChildByFieldName("value")

		if keyNode == nil || valueNode == nil {
			continue
		}

		key := GetNodeContent(keyNode, sourceCode)

		// Extract methods
		if key == "methods" && valueNode.Type() == "object" {
			for j := 0; j < int(valueNode.NamedChildCount()); j++ {
				method := valueNode.NamedChild(j)
				if method != nil && method.Type() == "pair" {
					if methodSymbol := v.extractVueMethod(method, sourceCode, filePath, projectID, parentPath, parentID); methodSymbol != nil {
						symbols = append(symbols, methodSymbol)
					}
				}
			}
		}
	}

	return symbols
}

// extractVueMethod extracts a Vue method from a methods object
func (v *VueExtractor) extractVueMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	keyNode := node.ChildByFieldName("key")
	if keyNode == nil {
		return nil
	}

	name := GetNodeContent(keyNode, sourceCode)
	namePath := v.BuildNamePath(parentPath, name)

	symbol := v.CreateSymbol(node, sourceCode, SymbolTypeMethod, name, namePath, filePath, projectID, parentID)

	return symbol
}

// extractClass extracts a class declaration
func (v *VueExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := v.BuildNamePath(parentPath, name)

	symbol := v.CreateSymbol(node, sourceCode, SymbolTypeClass, name, namePath, filePath, projectID, parentID)
	symbol.DocString = v.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract class body members
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			member := body.NamedChild(i)
			if member == nil {
				continue
			}

			if member.Type() == "method_definition" {
				memberSymbols := v.extractMethod(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
				for _, ms := range memberSymbols {
					symbol.Children = append(symbol.Children, ms)
				}
				symbols = append(symbols, memberSymbols...)
			}
		}
	}

	return symbols
}

// extractMethod extracts a method from a class
func (v *VueExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := v.BuildNamePath(parentPath, name)

	symbol := v.CreateSymbol(node, sourceCode, SymbolTypeMethod, name, namePath, filePath, projectID, parentID)
	symbol.DocString = v.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	return symbols
}

// extractFunction extracts a function declaration
func (v *VueExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := v.BuildNamePath(parentPath, name)

	symbol := v.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.DocString = v.ExtractDocString(node, sourceCode)

	return symbol
}

// extractVariableDeclaration extracts variable declarations
func (v *VueExtractor) extractVariableDeclaration(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child == nil || child.Type() != "variable_declarator" {
			continue
		}

		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		name := GetNodeContent(nameNode, sourceCode)
		namePath := v.BuildNamePath(parentPath, name)

		// Determine if it's a const
		symbolType := SymbolTypeVariable
		if node.Type() == "lexical_declaration" {
			// Check if it's const or let
			for j := 0; j < int(node.ChildCount()); j++ {
				c := node.Child(j)
				if c != nil && c.Type() == "const" {
					symbolType = SymbolTypeConstant
					break
				}
			}
		}

		symbol := v.CreateSymbol(child, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
		symbols = append(symbols, symbol)
	}

	return symbols
}
