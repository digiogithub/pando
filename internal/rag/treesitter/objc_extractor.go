// Package treesitter provides Objective-C language symbol extraction.
package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// ObjCExtractor extracts symbols from Objective-C source code using the
// tree-sitter-objc grammar (a superset of the C grammar). C-level constructs
// (functions, structs, enums, typedefs, macros) are delegated to CExtractor and
// re-tagged as Objective-C.
type ObjCExtractor struct {
	BaseExtractor
	c *CExtractor
}

// NewObjCExtractor creates a new Objective-C extractor
func NewObjCExtractor(config WalkerConfig) *ObjCExtractor {
	return &ObjCExtractor{
		BaseExtractor: NewBaseExtractor(LanguageObjectiveC, config),
		c:             NewCExtractor(config),
	}
}

// GetSymbolTypes returns the types of symbols the Objective-C extractor can find
func (o *ObjCExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeClass,
		SymbolTypeInterface,
		SymbolTypeMethod,
		SymbolTypeProperty,
		SymbolTypeFunction,
		SymbolTypeStruct,
		SymbolTypeEnum,
		SymbolTypeEnumMember,
		SymbolTypeTypeAlias,
		SymbolTypeVariable,
		SymbolTypeConstant,
	}
}

// ExtractSymbols extracts all symbols from Objective-C source code
func (o *ObjCExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()
	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}
		symbols = append(symbols, o.extractNode(child, sourceCode, filePath, projectID, "", nil)...)
	}
	return symbols, nil
}

func (o *ObjCExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	switch node.Type() {
	case "class_interface", "class_implementation":
		return o.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID, SymbolTypeClass)
	case "protocol_declaration":
		return o.extractClass(node, sourceCode, filePath, projectID, parentPath, parentID, SymbolTypeInterface)
	case "method_declaration", "method_definition":
		if s := o.extractMethod(node, sourceCode, filePath, projectID, parentPath, parentID); s != nil {
			return []*CodeSymbol{s}
		}
	case "property_declaration":
		if s := o.extractProperty(node, sourceCode, filePath, projectID, parentPath, parentID); s != nil {
			return []*CodeSymbol{s}
		}
	case "implementation_definition":
		var out []*CodeSymbol
		for i := 0; i < int(node.NamedChildCount()); i++ {
			if c := node.NamedChild(i); c != nil {
				out = append(out, o.extractNode(c, sourceCode, filePath, projectID, parentPath, parentID)...)
			}
		}
		return out
	case "type_definition":
		return o.extractTypedef(node, sourceCode, filePath, projectID, parentPath, parentID)
	case "function_definition", "declaration", "struct_specifier", "enum_specifier", "preproc_def":
		syms := o.c.extractNode(node, sourceCode, filePath, projectID, parentPath, parentID)
		for _, s := range syms {
			s.Language = LanguageObjectiveC
		}
		return syms
	}
	return nil
}

// extractClass handles class_interface, class_implementation (including
// categories) and protocol_declaration, then their members.
func (o *ObjCExtractor) extractClass(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string, st SymbolType) []*CodeSymbol {
	var nameNode *sitter.Node
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if c := node.NamedChild(i); c != nil && c.Type() == "identifier" {
			nameNode = c
			break
		}
	}
	if nameNode == nil {
		return nil
	}
	name := GetNodeContent(nameNode, sourceCode)
	if cat := node.ChildByFieldName("category"); cat != nil {
		name += "(" + GetNodeContent(cat, sourceCode) + ")"
	} else if node.Type() == "class_implementation" || node.Type() == "class_interface" {
		// Anonymous class extension `@interface Foo ()` / category on impl: an
		// identifier directly after the name that is not the superclass.
		for i := 0; i < int(node.NamedChildCount()); i++ {
			c := node.NamedChild(i)
			if c == nil || c.Type() != "identifier" || c.StartByte() == nameNode.StartByte() {
				continue
			}
			if sc := node.ChildByFieldName("superclass"); sc != nil && sc.StartByte() == c.StartByte() {
				continue
			}
			name += "(" + GetNodeContent(c, sourceCode) + ")"
			break
		}
	}
	namePath := o.BuildNamePath(parentPath, name)
	symbol := o.CreateSymbol(node, sourceCode, st, name, namePath, filePath, projectID, parentID)
	symbol.DocString = o.ExtractDocString(node, sourceCode)
	if sc := node.ChildByFieldName("superclass"); sc != nil {
		symbol.Signature = "@interface " + name + " : " + GetNodeContent(sc, sourceCode)
	}
	symbols := []*CodeSymbol{symbol}
	for i := 0; i < int(node.NamedChildCount()); i++ {
		member := node.NamedChild(i)
		if member == nil {
			continue
		}
		ms := o.extractNode(member, sourceCode, filePath, projectID, namePath, &symbol.ID)
		symbol.Children = append(symbol.Children, ms...)
		symbols = append(symbols, ms...)
	}
	return symbols
}

// objcSelector builds the full selector (e.g. "initWithFoo:bar:") of a
// method_declaration / method_definition. Selector parts are direct identifier
// children; each part followed by a method_parameter carries a colon.
func objcSelector(node *sitter.Node, sourceCode []byte) string {
	var sb strings.Builder
	n := int(node.NamedChildCount())
	for i := 0; i < n; i++ {
		c := node.NamedChild(i)
		if c == nil || c.Type() != "identifier" {
			continue
		}
		sb.WriteString(GetNodeContent(c, sourceCode))
		if i+1 < n {
			if next := node.NamedChild(i + 1); next != nil && next.Type() == "method_parameter" {
				sb.WriteString(":")
			}
		}
	}
	return sb.String()
}

func (o *ObjCExtractor) extractMethod(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	name := objcSelector(node, sourceCode)
	if name == "" {
		return nil
	}
	namePath := o.BuildNamePath(parentPath, name)
	st := SymbolTypeMethod
	if strings.HasPrefix(name, "init") {
		st = SymbolTypeConstructor
	}
	symbol := o.CreateSymbol(node, sourceCode, st, name, namePath, filePath, projectID, parentID)
	symbol.DocString = o.ExtractDocString(node, sourceCode)
	// Signature: everything up to the body (or the whole prototype).
	end := node.EndByte()
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if c := node.NamedChild(i); c != nil && c.Type() == "compound_statement" {
			end = c.StartByte()
			break
		}
	}
	if int(end) <= len(sourceCode) && node.StartByte() < end {
		symbol.Signature = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(sourceCode[node.StartByte():end])), ";"))
	}
	return symbol
}

func (o *ObjCExtractor) extractProperty(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) *CodeSymbol {
	var name string
	for i := 0; i < int(node.NamedChildCount()) && name == ""; i++ {
		sd := node.NamedChild(i)
		if sd == nil || sd.Type() != "struct_declaration" {
			continue
		}
		for j := 0; j < int(sd.NamedChildCount()) && name == ""; j++ {
			if d := sd.NamedChild(j); d != nil && d.Type() == "struct_declarator" {
				name = o.c.getDeclaratorName(unwrapDeclarator(d), sourceCode)
			}
		}
	}
	if name == "" {
		return nil
	}
	symbol := o.CreateSymbol(node, sourceCode, SymbolTypeProperty, name, o.BuildNamePath(parentPath, name), filePath, projectID, parentID)
	symbol.Signature = strings.TrimSuffix(strings.TrimSpace(GetNodeContent(node, sourceCode)), ";")
	return symbol
}

// unwrapDeclarator returns the inner declarator of a struct_declarator wrapper.
func unwrapDeclarator(n *sitter.Node) *sitter.Node {
	if n.Type() == "struct_declarator" && n.NamedChildCount() > 0 {
		return n.NamedChild(0)
	}
	return n
}

// extractTypedef emits a type alias for every declarator of a typedef. Unlike
// C, the Objective-C grammar exposes the aliased names as type_identifier.
func (o *ObjCExtractor) extractTypedef(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	var out []*CodeSymbol
	for i := 0; i < int(node.ChildCount()); i++ {
		if node.FieldNameForChild(i) != "declarator" {
			continue
		}
		d := node.Child(i)
		for d != nil && d.Type() != "type_identifier" && d.Type() != "identifier" {
			d = d.ChildByFieldName("declarator")
		}
		if d == nil {
			continue
		}
		name := GetNodeContent(d, sourceCode)
		s := o.CreateSymbol(node, sourceCode, SymbolTypeTypeAlias, name, o.BuildNamePath(parentPath, name), filePath, projectID, parentID)
		s.DocString = o.ExtractDocString(node, sourceCode)
		out = append(out, s)
	}
	return out
}
