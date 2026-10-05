// Package treesitter provides C# language symbol extraction.
package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// CSharpExtractor extracts symbols from C# source code.
//
// Namespaces become SymbolTypeNamespace symbols whose segments are part of
// the name path of everything they contain (namespace A.B { class C {} } ->
// "/A/B/C"). Both block-scoped and file-scoped namespaces are supported.
type CSharpExtractor struct {
	BaseExtractor
}

// NewCSharpExtractor creates a new C# extractor
func NewCSharpExtractor(config WalkerConfig) *CSharpExtractor {
	return &CSharpExtractor{
		BaseExtractor: NewBaseExtractor(LanguageCSharp, config),
	}
}

// GetSymbolTypes returns the types of symbols the C# extractor can find
func (c *CSharpExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeNamespace,
		SymbolTypeClass,
		SymbolTypeStruct,
		SymbolTypeInterface,
		SymbolTypeEnum,
		SymbolTypeEnumMember,
		SymbolTypeMethod,
		SymbolTypeConstructor,
		SymbolTypeProperty,
		SymbolTypeField,
		SymbolTypeConstant,
		SymbolTypeTypeAlias,
	}
}

// ExtractSymbols extracts all symbols from C# source code
func (c *CSharpExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	return c.extractList(tree.RootNode(), sourceCode, filePath, projectID, "", nil, nil), nil
}

// extractList walks the named children of a container (compilation unit or
// declaration_list). A file-scoped namespace applies to the siblings that
// follow it, so the scope is tracked while iterating.
func (c *CSharpExtractor) extractList(container *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string, parentSym *CodeSymbol) []*CodeSymbol {
	var symbols []*CodeSymbol
	curPath, curID, curSym := parentPath, parentID, parentSym
	for i := 0; i < int(container.NamedChildCount()); i++ {
		child := container.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Type() == "file_scoped_namespace_declaration" {
			if ns := c.namespaceSymbol(child, src, filePath, projectID, parentPath, parentID); ns != nil {
				symbols = append(symbols, ns)
				curPath, curID, curSym = ns.NamePath, &ns.ID, ns
			}
			continue
		}
		got := c.extractNode(child, src, filePath, projectID, curPath, curID)
		if curSym != nil && curSym != parentSym {
			curSym.Children = append(curSym.Children, directChildren(got, curSym.ID)...)
		}
		symbols = append(symbols, got...)
	}
	return symbols
}

func (c *CSharpExtractor) namespaceSymbol(node *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	name := GetNodeContent(nameNode, src)
	namePath := parentPath
	for _, seg := range strings.Split(name, ".") {
		namePath = c.BuildNamePath(namePath, strings.TrimSpace(seg))
	}
	sym := c.CreateSymbol(node, src, SymbolTypeNamespace, name, namePath, filePath, projectID, parentID)
	sym.DocString = c.docString(node, src)
	return sym
}

// extractNode extracts symbols from a single declaration node
func (c *CSharpExtractor) extractNode(node *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string) []*CodeSymbol {
	switch node.Type() {
	case "namespace_declaration":
		ns := c.namespaceSymbol(node, src, filePath, projectID, parentPath, parentID)
		if ns == nil {
			return nil
		}
		out := []*CodeSymbol{ns}
		if body := node.ChildByFieldName("body"); body != nil {
			members := c.extractList(body, src, filePath, projectID, ns.NamePath, &ns.ID, ns)
			ns.Children = append(ns.Children, directChildren(members, ns.ID)...)
			out = append(out, members...)
		}
		return out

	case "class_declaration", "record_declaration":
		return c.extractType(node, src, filePath, projectID, parentPath, parentID, SymbolTypeClass)
	case "struct_declaration", "record_struct_declaration":
		return c.extractType(node, src, filePath, projectID, parentPath, parentID, SymbolTypeStruct)
	case "interface_declaration":
		return c.extractType(node, src, filePath, projectID, parentPath, parentID, SymbolTypeInterface)
	case "enum_declaration":
		return c.extractEnum(node, src, filePath, projectID, parentPath, parentID)

	case "method_declaration":
		return c.single(c.namedMember(node, src, filePath, projectID, parentPath, parentID, SymbolTypeMethod))
	case "constructor_declaration":
		return c.single(c.namedMember(node, src, filePath, projectID, parentPath, parentID, SymbolTypeConstructor))
	case "destructor_declaration":
		if nameNode := node.ChildByFieldName("name"); nameNode != nil {
			name := "~" + GetNodeContent(nameNode, src)
			return c.single(c.makeSymbol(node, src, filePath, projectID, parentPath, parentID, SymbolTypeMethod, name))
		}
	case "property_declaration":
		return c.single(c.namedMember(node, src, filePath, projectID, parentPath, parentID, SymbolTypeProperty))
	case "event_declaration":
		return c.single(c.namedMember(node, src, filePath, projectID, parentPath, parentID, SymbolTypeProperty))
	case "indexer_declaration":
		return c.single(c.makeSymbol(node, src, filePath, projectID, parentPath, parentID, SymbolTypeProperty, "this[]"))
	case "operator_declaration", "conversion_operator_declaration":
		return c.single(c.makeSymbol(node, src, filePath, projectID, parentPath, parentID, SymbolTypeMethod, c.operatorName(node, src)))
	case "delegate_declaration":
		return c.single(c.namedMember(node, src, filePath, projectID, parentPath, parentID, SymbolTypeTypeAlias))
	case "field_declaration", "event_field_declaration":
		return c.extractFields(node, src, filePath, projectID, parentPath, parentID)
	}
	return nil
}

func (c *CSharpExtractor) single(s *CodeSymbol) []*CodeSymbol {
	if s == nil {
		return nil
	}
	return []*CodeSymbol{s}
}

// directChildren returns the symbols of list whose parent is the given ID.
func directChildren(list []*CodeSymbol, id string) []*CodeSymbol {
	var out []*CodeSymbol
	for _, s := range list {
		if s.ParentID != nil && *s.ParentID == id {
			out = append(out, s)
		}
	}
	return out
}

func (c *CSharpExtractor) makeSymbol(node *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string, st SymbolType, name string) *CodeSymbol {
	sym := c.CreateSymbol(node, src, st, name, c.BuildNamePath(parentPath, name), filePath, projectID, parentID)
	sym.DocString = c.docString(node, src)
	return sym
}

// namedMember builds a symbol whose name is the `name` field of node.
func (c *CSharpExtractor) namedMember(node *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string, st SymbolType) *CodeSymbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	sym := c.makeSymbol(node, src, filePath, projectID, parentPath, parentID, st, GetNodeContent(nameNode, src))
	if st == SymbolTypeMethod || st == SymbolTypeConstructor || st == SymbolTypeTypeAlias {
		sym.Signature = c.signature(node, src)
	}
	return sym
}

func (c *CSharpExtractor) operatorName(node *sitter.Node, src []byte) string {
	if op := node.ChildByFieldName("operator"); op != nil {
		return "operator " + GetNodeContent(op, src)
	}
	if t := node.ChildByFieldName("type"); t != nil {
		return "operator " + GetNodeContent(t, src)
	}
	return "operator"
}

// extractType handles class/struct/record/interface declarations and their members.
func (c *CSharpExtractor) extractType(node *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string, st SymbolType) []*CodeSymbol {
	// `record struct` is a record_declaration carrying a `struct` keyword.
	if node.Type() == "record_declaration" {
		for i := 0; i < int(node.ChildCount()); i++ {
			if ch := node.Child(i); ch != nil && !ch.IsNamed() && ch.Type() == "struct" {
				st = SymbolTypeStruct
				break
			}
		}
	}
	sym := c.namedMember(node, src, filePath, projectID, parentPath, parentID, st)
	if sym == nil {
		return nil
	}
	out := []*CodeSymbol{sym}
	if body := node.ChildByFieldName("body"); body != nil {
		members := c.extractList(body, src, filePath, projectID, sym.NamePath, &sym.ID, sym)
		sym.Children = append(sym.Children, directChildren(members, sym.ID)...)
		out = append(out, members...)
	}
	return out
}

// extractEnum handles enum declarations and their members.
func (c *CSharpExtractor) extractEnum(node *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string) []*CodeSymbol {
	sym := c.namedMember(node, src, filePath, projectID, parentPath, parentID, SymbolTypeEnum)
	if sym == nil {
		return nil
	}
	out := []*CodeSymbol{sym}
	if body := node.ChildByFieldName("body"); body != nil {
		for i := 0; i < int(body.NamedChildCount()); i++ {
			m := body.NamedChild(i)
			if m == nil || m.Type() != "enum_member_declaration" {
				continue
			}
			if member := c.namedMember(m, src, filePath, projectID, sym.NamePath, &sym.ID, SymbolTypeEnumMember); member != nil {
				sym.Children = append(sym.Children, member)
				out = append(out, member)
			}
		}
	}
	return out
}

// extractFields handles field and event-field declarations (one symbol per declarator).
func (c *CSharpExtractor) extractFields(node *sitter.Node, src []byte, filePath, projectID, parentPath string, parentID *string) []*CodeSymbol {
	st := SymbolTypeField
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch != nil && ch.Type() == "modifier" && GetNodeContent(ch, src) == "const" {
			st = SymbolTypeConstant
		}
	}
	var out []*CodeSymbol
	for i := 0; i < int(node.NamedChildCount()); i++ {
		decl := node.NamedChild(i)
		if decl == nil || decl.Type() != "variable_declaration" {
			continue
		}
		for j := 0; j < int(decl.NamedChildCount()); j++ {
			vd := decl.NamedChild(j)
			if vd == nil || vd.Type() != "variable_declarator" {
				continue
			}
			nameNode := vd.ChildByFieldName("name")
			if nameNode == nil {
				continue
			}
			sym := c.makeSymbol(vd, src, filePath, projectID, parentPath, parentID, st, GetNodeContent(nameNode, src))
			if len(out) == 0 {
				sym.DocString = c.docString(node, src)
			} else {
				sym.DocString = ""
			}
			out = append(out, sym)
		}
	}
	return out
}

// signature returns the text from the return type (or name) to the end of the parameter list.
func (c *CSharpExtractor) signature(node *sitter.Node, src []byte) string {
	params := node.ChildByFieldName("parameters")
	if params == nil {
		return ""
	}
	start := node.StartByte()
	if t := node.ChildByFieldName("returns"); t != nil {
		start = t.StartByte()
	} else if t := node.ChildByFieldName("type"); t != nil {
		start = t.StartByte()
	} else if n := node.ChildByFieldName("name"); n != nil {
		start = n.StartByte()
	}
	end := params.EndByte()
	if int(end) > len(src) || end < start {
		return ""
	}
	return string(src[start:end])
}

// docString collects the contiguous run of comments directly above node
// (`///` XML doc lines or block comments). Falls back to nothing when doc
// extraction is disabled.
func (c *CSharpExtractor) docString(node *sitter.Node, src []byte) string {
	if !c.config.ExtractDocStrings {
		return ""
	}
	var parts []string
	expectEnd := node.StartPoint().Row
	for prev := node.PrevNamedSibling(); prev != nil && prev.Type() == "comment"; prev = prev.PrevNamedSibling() {
		if prev.EndPoint().Row+1 < expectEnd {
			break
		}
		parts = append([]string{GetNodeContent(prev, src)}, parts...)
		expectEnd = prev.StartPoint().Row
	}
	return strings.Join(parts, "\n")
}
