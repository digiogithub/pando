package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for C++. It emits:
//   - imports:  #include directives (DstPath = the header without the
//     <> / "" delimiters, e.g. "vector", "foo/bar.h").
//   - calls:    call expressions (bare identifier, member name of a field
//     expression, last segment of a qualified name, template function name)
//     and `new Foo(...)` (Foo), attributed to the enclosing function/method.
//   - type_ref: type_identifier references (members, parameters, return
//     types, base classes, template arguments, ...), excluding the names being
//     declared (class/struct/enum/alias names, template parameters).
func (c *CPPExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		startLine, _, startByte, _ := GetNodeLocation(node)
		switch node.Type() {
		case "preproc_include":
			if p := node.ChildByFieldName("path"); p != nil {
				path := cppIncludePath(p, sourceCode)
				if path != "" {
					edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: path, StartLine: startLine})
				}
			}
		case "call_expression":
			if name := cppCalleeName(node.ChildByFieldName("function"), sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "new_expression":
			if name := cppTypeName(node.ChildByFieldName("type"), sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "type_identifier":
			if cppIsDeclaredName(node) {
				continue
			}
			edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeTypeRef, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: GetNodeContent(node, sourceCode), StartLine: startLine})
		}
	}
	return edges, nil
}

// cppIncludePath returns the header path of a preproc_include path node.
func cppIncludePath(p *sitter.Node, src []byte) string {
	txt := GetNodeContent(p, src)
	txt = strings.TrimSpace(txt)
	if len(txt) >= 2 {
		switch {
		case txt[0] == '<' && txt[len(txt)-1] == '>':
			return txt[1 : len(txt)-1]
		case txt[0] == '"' && txt[len(txt)-1] == '"':
			return txt[1 : len(txt)-1]
		}
	}
	return txt
}

// cppCalleeName resolves the called name from a call_expression function node.
func cppCalleeName(fn *sitter.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "field_identifier", "type_identifier":
		return GetNodeContent(fn, src)
	case "field_expression":
		return cppCalleeName(fn.ChildByFieldName("field"), src)
	case "qualified_identifier":
		return cppCalleeName(fn.ChildByFieldName("name"), src)
	case "template_function", "template_method":
		return cppCalleeName(fn.ChildByFieldName("name"), src)
	case "destructor_name":
		return GetNodeContent(fn, src)
	}
	return ""
}

// cppTypeName returns the simple name of a type node (new expressions).
func cppTypeName(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "type_identifier":
		return GetNodeContent(n, src)
	case "qualified_identifier":
		return cppTypeName(n.ChildByFieldName("name"), src)
	case "template_type":
		return cppTypeName(n.ChildByFieldName("name"), src)
	}
	return ""
}

// cppIsDeclaredName reports whether a type_identifier is a name being
// declared (not a reference to a type).
func cppIsDeclaredName(n *sitter.Node) bool {
	p := n.Parent()
	if p == nil {
		return false
	}
	switch p.Type() {
	case "class_specifier", "struct_specifier", "union_specifier", "enum_specifier", "alias_declaration", "concept_definition":
		if nm := p.ChildByFieldName("name"); nm != nil && cppSameNode(nm, n) {
			return true
		}
	case "type_definition":
		for _, d := range fieldChildren(p, "declarator") {
			if cppSameNode(d, n) {
				return true
			}
		}
	case "type_parameter_declaration", "optional_type_parameter_declaration", "variadic_type_parameter_declaration":
		return true
	}
	return false
}

// cppSameNode compares two nodes by type and byte range.
func cppSameNode(a, b *sitter.Node) bool {
	return a != nil && b != nil && a.Type() == b.Type() && a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}
