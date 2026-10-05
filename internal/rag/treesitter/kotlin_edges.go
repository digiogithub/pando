package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for Kotlin. It emits:
//   - imports:  import_header (DstPath = dotted identifier as written, with
//     ".*" for wildcard imports; aliases keep the original path).
//   - calls:    call_expression (simple_identifier callee, or the last
//     navigation_suffix identifier of a navigation_expression) and
//     constructor_invocation in delegation specifiers (superclass call).
//   - type_ref: every type_identifier that is a reference (properties,
//     parameters, return types, receivers, supertypes, generics), not a
//     declaration name, type parameter or import alias.
//
// calls/type_ref are attributed to the enclosing symbol when any.
func (k *KotlinExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		startLine, _, startByte, _ := GetNodeLocation(node)
		switch node.Type() {
		case "import_header":
			id := FindChildByType(node, "identifier")
			if id == nil {
				continue
			}
			path := GetNodeContent(id, sourceCode)
			if path == "" {
				continue
			}
			if FindChildByType(node, "wildcard_import") != nil {
				path += ".*"
			}
			edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: path, StartLine: startLine})
		case "call_expression":
			if name := kotlinCalleeName(node, sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "constructor_invocation":
			if ut := FindChildByType(node, "user_type"); ut != nil {
				if name := kotlinUserTypeName(ut, sourceCode); name != "" {
					edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
				}
			}
		case "type_identifier":
			if kotlinIsDeclarationName(node) {
				continue
			}
			name := GetNodeContent(node, sourceCode)
			if name == "" {
				continue
			}
			edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeTypeRef, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
		}
	}
	return edges, nil
}

// kotlinCalleeName resolves the called name of a call_expression. The grammar
// has no fields: the callee is the first named child.
func kotlinCalleeName(call *sitter.Node, sourceCode []byte) string {
	if call.NamedChildCount() == 0 {
		return ""
	}
	callee := call.NamedChild(0)
	if callee == nil {
		return ""
	}
	switch callee.Type() {
	case "simple_identifier":
		return GetNodeContent(callee, sourceCode)
	case "navigation_expression":
		// The member name is the simple_identifier of the last navigation_suffix.
		n := int(callee.NamedChildCount())
		for i := n - 1; i >= 0; i-- {
			c := callee.NamedChild(i)
			if c != nil && c.Type() == "navigation_suffix" {
				if id := FindChildByType(c, "simple_identifier"); id != nil {
					return GetNodeContent(id, sourceCode)
				}
				return ""
			}
		}
	}
	return ""
}

// kotlinUserTypeName returns the last type_identifier of a user_type
// (`a.b.Foo<T>` -> Foo).
func kotlinUserTypeName(ut *sitter.Node, sourceCode []byte) string {
	var last *sitter.Node
	for i := 0; i < int(ut.NamedChildCount()); i++ {
		if c := ut.NamedChild(i); c != nil && c.Type() == "type_identifier" {
			last = c
		}
	}
	if last == nil {
		return ""
	}
	return GetNodeContent(last, sourceCode)
}

// kotlinIsDeclarationName reports whether a type_identifier names a declaration
// (class/object/alias/type parameter) or an import alias rather than
// referencing a type. Since the grammar has no fields, this is decided from the
// parent node type: references always live under user_type.
func kotlinIsDeclarationName(node *sitter.Node) bool {
	parent := node.Parent()
	return parent == nil || parent.Type() != "user_type"
}
