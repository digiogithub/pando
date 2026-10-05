package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for Objective-C. It emits:
//   - imports:  #import / #include (path without quotes or angle brackets) and
//     `@import Module;`.
//   - calls:    message sends (full selector such as "initWithFoo:bar:") and
//     plain C calls, attributed to the enclosing method/function.
//   - type_ref: class names used as message receivers, superclasses, protocol
//     lists, @class forward declarations and every type_identifier.
func (o *ObjCExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	add := func(et EdgeType, line, pos int, name, path string) {
		e := &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: et, DstName: name, DstPath: path, StartLine: line}
		if et != EdgeImports {
			e.SrcSymbolID = enclosingSymbolID(symbols, pos)
		}
		edges = append(edges, e)
	}
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		startLine, _, startByte, _ := GetNodeLocation(node)
		switch node.Type() {
		case "preproc_include":
			if p := node.ChildByFieldName("path"); p != nil {
				path := strings.Trim(GetNodeContent(p, sourceCode), "<>\"")
				if path != "" {
					add(EdgeImports, startLine, startByte, "", path)
				}
			}
		case "module_import":
			if p := node.ChildByFieldName("path"); p != nil {
				add(EdgeImports, startLine, startByte, "", GetNodeContent(p, sourceCode))
			}
		case "message_expression":
			if sel := objcMessageSelector(node, sourceCode); sel != "" {
				add(EdgeCalls, startLine, startByte, sel, "")
			}
			if r := node.ChildByFieldName("receiver"); r != nil && r.Type() == "identifier" {
				if n := GetNodeContent(r, sourceCode); n != "" && n[0] >= 'A' && n[0] <= 'Z' {
					add(EdgeTypeRef, startLine, startByte, n, "")
				}
			}
		case "call_expression":
			if f := node.ChildByFieldName("function"); f != nil {
				switch f.Type() {
				case "identifier":
					add(EdgeCalls, startLine, startByte, GetNodeContent(f, sourceCode), "")
				case "field_expression":
					if fld := f.ChildByFieldName("field"); fld != nil {
						add(EdgeCalls, startLine, startByte, GetNodeContent(fld, sourceCode), "")
					}
				}
			}
		case "class_interface":
			if sc := node.ChildByFieldName("superclass"); sc != nil {
				add(EdgeTypeRef, startLine, startByte, GetNodeContent(sc, sourceCode), "")
			}
		case "protocol_reference_list", "class_declaration":
			for i := 0; i < int(node.NamedChildCount()); i++ {
				if c := node.NamedChild(i); c != nil && c.Type() == "identifier" {
					add(EdgeTypeRef, startLine, startByte, GetNodeContent(c, sourceCode), "")
				}
			}
		case "type_identifier":
			if objcIsDefiningName(node) {
				continue
			}
			if n := GetNodeContent(node, sourceCode); n != "" {
				add(EdgeTypeRef, startLine, startByte, n, "")
			}
		}
	}
	return edges, nil
}

// objcMessageSelector reconstructs the selector of a message_expression from
// its repeated `method` fields; keyword parts get a trailing colon when the
// message carries arguments.
func objcMessageSelector(node *sitter.Node, sourceCode []byte) string {
	var parts []string
	hasArgs := false
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c == nil || !c.IsNamed() {
			continue
		}
		switch node.FieldNameForChild(i) {
		case "method":
			parts = append(parts, GetNodeContent(c, sourceCode))
		case "receiver":
		default:
			hasArgs = true
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if !hasArgs {
		return parts[0]
	}
	return strings.Join(parts, ":") + ":"
}

// objcIsDefiningName reports whether a type_identifier is the name being
// defined by its parent (struct/enum/union tag or typedef name) rather than a
// reference to a type.
func objcIsDefiningName(node *sitter.Node) bool {
	p := node.Parent()
	if p == nil {
		return false
	}
	switch p.Type() {
	case "struct_specifier", "enum_specifier", "union_specifier":
		n := p.ChildByFieldName("name")
		return n != nil && n.StartByte() == node.StartByte()
	case "type_definition":
		d := p.ChildByFieldName("declarator")
		return d != nil && d.StartByte() == node.StartByte()
	}
	return false
}
