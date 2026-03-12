// Package treesitter provides Rust language symbol extraction.
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// RustExtractor extracts symbols from Rust source code
type RustExtractor struct {
	BaseExtractor
}

// NewRustExtractor creates a new Rust extractor
func NewRustExtractor(config WalkerConfig) *RustExtractor {
	return &RustExtractor{
		BaseExtractor: NewBaseExtractor(LanguageRust, config),
	}
}

// GetSymbolTypes returns the types of symbols the Rust extractor can find
func (r *RustExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeStruct,
		SymbolTypeEnum,
		SymbolTypeTrait,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeConstant,
		SymbolTypeTypeAlias,
		SymbolTypeModule,
	}
}

// ExtractSymbols extracts all symbols from Rust source code
func (r *RustExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}

		childSymbols := r.extractNode(child, sourceCode, filePath, projectID, "", nil)
		symbols = append(symbols, childSymbols...)
	}

	return symbols, nil
}

// extractNode extracts symbols from a node
func (r *RustExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "struct_item":
		symbols = append(symbols, r.extractStruct(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "enum_item":
		symbols = append(symbols, r.extractEnum(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "trait_item":
		if symbol := r.extractTrait(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "impl_item":
		symbols = append(symbols, r.extractImpl(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "function_item":
		if symbol := r.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "const_item":
		if symbol := r.extractConst(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "static_item":
		if symbol := r.extractStatic(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "type_item":
		if symbol := r.extractTypeAlias(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "mod_item":
		symbols = append(symbols, r.extractModule(node, sourceCode, filePath, projectID, parentPath, parentID)...)
	}

	return symbols
}

// extractStruct extracts a struct declaration
func (r *RustExtractor) extractStruct(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeStruct, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract struct fields
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			field := body.NamedChild(i)
			if field == nil || field.Type() != "field_declaration" {
				continue
			}

			fieldNameNode := field.ChildByFieldName("name")
			if fieldNameNode != nil {
				fieldName := GetNodeContent(fieldNameNode, sourceCode)
				fieldPath := r.BuildNamePath(namePath, fieldName)
				fieldSymbol := r.CreateSymbol(field, sourceCode, SymbolTypeField, fieldName, fieldPath, filePath, projectID, &symbol.ID)
				symbol.Children = append(symbol.Children, fieldSymbol)
				symbols = append(symbols, fieldSymbol)
			}
		}
	}

	return symbols
}

// extractEnum extracts an enum declaration
func (r *RustExtractor) extractEnum(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeEnum, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)
	symbols = append(symbols, symbol)

	// Extract enum variants
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			variant := body.NamedChild(i)
			if variant == nil || variant.Type() != "enum_variant" {
				continue
			}

			variantNameNode := variant.ChildByFieldName("name")
			if variantNameNode != nil {
				variantName := GetNodeContent(variantNameNode, sourceCode)
				variantPath := r.BuildNamePath(namePath, variantName)
				variantSymbol := r.CreateSymbol(variant, sourceCode, SymbolTypeEnumMember, variantName, variantPath, filePath, projectID, &symbol.ID)
				symbol.Children = append(symbol.Children, variantSymbol)
				symbols = append(symbols, variantSymbol)
			}
		}
	}

	return symbols
}

// extractTrait extracts a trait declaration
func (r *RustExtractor) extractTrait(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeTrait, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)

	return symbol
}

// extractImpl extracts an impl block
func (r *RustExtractor) extractImpl(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	// Get the type being implemented
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return symbols
	}

	typeName := GetNodeContent(typeNode, sourceCode)
	implPath := r.BuildNamePath(parentPath, typeName)

	// Extract methods from impl body
	body := node.ChildByFieldName("body")
	if body == nil {
		return symbols
	}

	for i := 0; i < int(body.NamedChildCount()); i++ {
		child := body.NamedChild(i)
		if child == nil || child.Type() != "function_item" {
			continue
		}

		if methodSymbol := r.extractFunction(child, sourceCode, filePath, projectID, implPath, parentID); methodSymbol != nil {
			methodSymbol.SymbolType = SymbolTypeMethod
			methodSymbol.Metadata = map[string]interface{}{
				"impl_type": typeName,
			}
			symbols = append(symbols, methodSymbol)
		}
	}

	return symbols
}

// extractFunction extracts a function declaration
func (r *RustExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeFunction, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)
	symbol.Signature = r.extractFunctionSignature(node, sourceCode)

	return symbol
}

// extractConst extracts a const declaration
func (r *RustExtractor) extractConst(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeConstant, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)

	return symbol
}

// extractStatic extracts a static declaration
func (r *RustExtractor) extractStatic(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeVariable, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)

	return symbol
}

// extractTypeAlias extracts a type alias
func (r *RustExtractor) extractTypeAlias(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeTypeAlias, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)

	return symbol
}

// extractModule extracts a module declaration
func (r *RustExtractor) extractModule(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return symbols
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := r.BuildNamePath(parentPath, name)

	symbol := r.CreateSymbol(node, sourceCode, SymbolTypeModule, name, namePath, filePath, projectID, parentID)
	symbols = append(symbols, symbol)

	// Extract module body items
	body := node.ChildByFieldName("body")
	if body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			child := body.NamedChild(i)
			if child != nil {
				childSymbols := r.extractNode(child, sourceCode, filePath, projectID, namePath, &symbol.ID)
				symbol.Children = append(symbol.Children, childSymbols...)
				symbols = append(symbols, childSymbols...)
			}
		}
	}

	return symbols
}

// extractFunctionSignature extracts the function signature
func (r *RustExtractor) extractFunctionSignature(node *sitter.Node, sourceCode []byte) string {
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
