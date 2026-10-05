// Package treesitter provides Swift language symbol extraction.
package treesitter

import (
	"strings"

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

// extractNode extracts symbols from a node. In this grammar struct, enum,
// class, actor and extension are all `class_declaration` nodes distinguished by
// the `declaration_kind` field.
func (s *SwiftExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	switch node.Type() {
	case "class_declaration":
		symbols = append(symbols, s.extractTypeDecl(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "protocol_declaration":
		symbols = append(symbols, s.extractProtocol(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "function_declaration", "protocol_function_declaration":
		if symbol := s.extractFunction(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}

	case "init_declaration":
		symbols = append(symbols, s.extractNamedMember(node, SymbolTypeConstructor, "init", sourceCode, filePath, projectID, parentPath, parentID))

	case "deinit_declaration":
		symbols = append(symbols, s.extractNamedMember(node, SymbolTypeMethod, "deinit", sourceCode, filePath, projectID, parentPath, parentID))

	case "subscript_declaration":
		symbols = append(symbols, s.extractNamedMember(node, SymbolTypeMethod, "subscript", sourceCode, filePath, projectID, parentPath, parentID))

	case "property_declaration":
		symbols = append(symbols, s.extractProperty(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "protocol_property_declaration":
		symbols = append(symbols, s.extractProperty(node, sourceCode, filePath, projectID, parentPath, parentID)...)

	case "typealias_declaration", "associatedtype_declaration":
		if symbol := s.extractTypealias(node, sourceCode, filePath, projectID, parentPath, parentID); symbol != nil {
			symbols = append(symbols, symbol)
		}
	}

	return symbols
}

// swiftDeclKind returns the declaration keyword (class/struct/enum/actor/extension).
func swiftDeclKind(node *sitter.Node, sourceCode []byte) string {
	if k := node.ChildByFieldName("declaration_kind"); k != nil {
		return GetNodeContent(k, sourceCode)
	}
	return "class"
}

// extractTypeDecl dispatches a class_declaration by its declaration_kind.
func (s *SwiftExtractor) extractTypeDecl(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	kind := swiftDeclKind(node, sourceCode)
	if kind == "extension" {
		return s.extractExtension(node, sourceCode, filePath, projectID, parentPath, parentID)
	}

	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbolType := SymbolTypeClass
	switch kind {
	case "struct":
		symbolType = SymbolTypeStruct
	case "enum":
		symbolType = SymbolTypeEnum
	}

	symbol := s.CreateSymbol(node, sourceCode, symbolType, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	symbol.Metadata = map[string]interface{}{"kind": kind}
	if inh := swiftInheritedTypes(node, sourceCode); len(inh) > 0 {
		symbol.Metadata["inherits"] = inh
	}

	symbols := []*CodeSymbol{symbol}
	if body := node.ChildByFieldName("body"); body != nil {
		symbols = append(symbols, s.extractMembers(body, sourceCode, filePath, projectID, namePath, symbol)...)
	}
	return symbols
}

// extractMembers walks a type body attaching members to parent.
func (s *SwiftExtractor) extractMembers(body *sitter.Node, sourceCode []byte, filePath string, projectID string, namePath string, parent *CodeSymbol) []*CodeSymbol {
	var symbols []*CodeSymbol
	for i := 0; i < int(body.NamedChildCount()); i++ {
		member := body.NamedChild(i)
		if member == nil {
			continue
		}
		var memberSymbols []*CodeSymbol
		if member.Type() == "enum_entry" {
			memberSymbols = s.extractEnumEntry(member, sourceCode, filePath, projectID, namePath, &parent.ID)
		} else {
			memberSymbols = s.extractNode(member, sourceCode, filePath, projectID, namePath, &parent.ID)
		}
		parent.Children = append(parent.Children, memberSymbols...)
		symbols = append(symbols, memberSymbols...)
	}
	return symbols
}

// extractEnumEntry extracts the cases of one `case a, b(Int)` entry.
func (s *SwiftExtractor) extractEnumEntry(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol
	for i := 0; i < int(node.ChildCount()); i++ {
		if node.FieldNameForChild(i) != "name" {
			continue
		}
		c := node.Child(i)
		if c == nil {
			continue
		}
		caseName := GetNodeContent(c, sourceCode)
		casePath := s.BuildNamePath(parentPath, caseName)
		symbols = append(symbols, s.CreateSymbol(node, sourceCode, SymbolTypeEnumMember, caseName, casePath, filePath, projectID, parentID))
	}
	return symbols
}

// swiftInheritedTypes returns the type names in inheritance_specifier children.
func swiftInheritedTypes(node *sitter.Node, sourceCode []byte) []string {
	var out []string
	for i := 0; i < int(node.NamedChildCount()); i++ {
		c := node.NamedChild(i)
		if c != nil && c.Type() == "inheritance_specifier" {
			if t := strings.TrimSpace(GetNodeContent(c, sourceCode)); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// extractNamedMember builds a method-like symbol with a fixed name (init, deinit, subscript).
func (s *SwiftExtractor) extractNamedMember(node *sitter.Node, st SymbolType, name string, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	namePath := s.BuildNamePath(parentPath, name)
	symbol := s.CreateSymbol(node, sourceCode, st, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	return symbol
}

// extractProtocol extracts a protocol declaration and its members.
func (s *SwiftExtractor) extractProtocol(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbol := s.CreateSymbol(node, sourceCode, SymbolTypeInterface, name, namePath, filePath, projectID, parentID)
	symbol.DocString = s.ExtractDocString(node, sourceCode)
	symbol.Metadata = map[string]interface{}{"kind": "protocol"}
	if inh := swiftInheritedTypes(node, sourceCode); len(inh) > 0 {
		symbol.Metadata["inherits"] = inh
	}

	symbols := []*CodeSymbol{symbol}
	if body := node.ChildByFieldName("body"); body != nil {
		symbols = append(symbols, s.extractMembers(body, sourceCode, filePath, projectID, namePath, symbol)...)
	}
	return symbols
}

// extractFunction extracts a function (or protocol function requirement).
func (s *SwiftExtractor) extractFunction(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}

	name := GetNodeContent(nameNode, sourceCode)
	namePath := s.BuildNamePath(parentPath, name)

	symbolType := SymbolTypeFunction
	if parentPath != "" {
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

// extractExtension extracts the members of an extension. No symbol is created
// for the extended type itself; members are attributed under the extended type's
// name path and tagged with metadata extension_of (and conforms_to).
func (s *SwiftExtractor) extractExtension(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var symbols []*CodeSymbol

	typeNode := node.ChildByFieldName("name")
	if typeNode == nil {
		return symbols
	}
	typeName := GetNodeContent(typeNode, sourceCode)
	if id := FindChildByType(typeNode, "type_identifier"); id != nil {
		typeName = GetNodeContent(id, sourceCode)
	}
	extPath := s.BuildNamePath(parentPath, typeName)
	conforms := swiftInheritedTypes(node, sourceCode)

	body := node.ChildByFieldName("body")
	if body == nil {
		return symbols
	}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		member := body.NamedChild(i)
		if member == nil {
			continue
		}
		var memberSymbols []*CodeSymbol
		if member.Type() == "enum_entry" {
			memberSymbols = s.extractEnumEntry(member, sourceCode, filePath, projectID, extPath, parentID)
		} else {
			memberSymbols = s.extractNode(member, sourceCode, filePath, projectID, extPath, parentID)
		}
		for _, sym := range memberSymbols {
			if sym.Metadata == nil {
				sym.Metadata = map[string]interface{}{}
			}
			if _, ok := sym.Metadata["extension_of"]; !ok {
				sym.Metadata["extension_of"] = typeName
				if len(conforms) > 0 {
					sym.Metadata["conforms_to"] = conforms
				}
			}
		}
		symbols = append(symbols, memberSymbols...)
	}
	return symbols
}
