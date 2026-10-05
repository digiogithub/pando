package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for C#. It emits:
//   - imports:  using directives (DstPath = the namespace/type as written;
//     for `using Alias = X.Y;` the aliased target X.Y).
//   - calls:    invocation expressions (method name: bare, member access,
//     generic or conditional-access) and object creations (`new Foo()` -> Foo),
//     attributed to the enclosing member.
//   - type_ref: identifiers in type position (declared types, return types,
//     parameters, base lists, generic arguments, casts, constraints, ...).
//     Simple names only; qualified types contribute their last segment.
func (c *CSharpExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		startLine, _, startByte, _ := GetNodeLocation(node)
		switch node.Type() {
		case "using_directive":
			path := csUsingTarget(node, sourceCode)
			if path != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: path, StartLine: startLine})
			}
		case "invocation_expression":
			if name := csCalleeName(node.ChildByFieldName("function"), sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "object_creation_expression":
			if name := csTypeName(node.ChildByFieldName("type"), sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "identifier", "generic_name", "qualified_name":
			if !csIsTypePosition(node) {
				continue
			}
			name := csTypeName(node, sourceCode)
			if name == "" || name == "var" {
				continue
			}
			edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeTypeRef, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
		}
	}
	return edges, nil
}

// csUsingTarget returns the imported namespace/type of a using directive.
func csUsingTarget(node *sitter.Node, src []byte) string {
	var target *sitter.Node
	aliasName := node.ChildByFieldName("name")
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch == nil {
			continue
		}
		if aliasName != nil && csharpSameNode(ch, aliasName) {
			continue // alias name, not the target
		}
		switch ch.Type() {
		case "identifier", "qualified_name", "generic_name", "alias_qualified_name":
			target = ch
		}
	}
	if target == nil {
		return ""
	}
	return GetNodeContent(target, src)
}

// csCalleeName resolves the invoked method name of a call's function node.
func csCalleeName(fn *sitter.Node, src []byte) string {
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier", "generic_name":
		return csTypeName(fn, src)
	case "member_access_expression", "member_binding_expression":
		return csTypeName(fn.ChildByFieldName("name"), src)
	case "conditional_access_expression":
		// a?.B() : the binding is the last named child.
		if n := int(fn.NamedChildCount()); n > 0 {
			return csCalleeName(fn.NamedChild(n-1), src)
		}
	}
	return ""
}

// csTypeName returns the simple name of identifier / generic_name /
// qualified_name nodes; "" for anything else (predefined types, arrays...).
func csTypeName(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier":
		return GetNodeContent(n, src)
	case "generic_name":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if ch := n.NamedChild(i); ch != nil && ch.Type() == "identifier" {
				return GetNodeContent(ch, src)
			}
		}
	case "qualified_name":
		return csTypeName(n.ChildByFieldName("name"), src)
	}
	return ""
}

// csIsTypePosition reports whether n sits where the grammar expects a type:
// the `type`/`returns` field of its parent, or inside a base list / type
// argument list.
func csIsTypePosition(n *sitter.Node) bool {
	p := n.Parent()
	if p == nil {
		return false
	}
	switch p.Type() {
	case "base_list", "type_argument_list", "primary_constructor_base_type":
		return true
	case "generic_name", "qualified_name", "alias_qualified_name", "using_directive", "namespace_declaration", "file_scoped_namespace_declaration":
		return false
	}
	for _, f := range []string{"type", "returns"} {
		if t := p.ChildByFieldName(f); t != nil && csharpSameNode(t, n) {
			return true
		}
	}
	return false
}

// csharpSameNode compares two nodes by type and byte range.
func csharpSameNode(a, b *sitter.Node) bool {
	return a != nil && b != nil && a.Type() == b.Type() && a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}
