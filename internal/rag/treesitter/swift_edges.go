package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for Swift. It emits:
//   - imports:  import declarations (DstPath = dotted path as written,
//     e.g. "Foundation" or "Foo.Bar").
//   - calls:    call expressions (bare identifier or the member name of a
//     navigation expression such as `store.save(x)` -> "save").
//   - type_ref: every type_identifier that is a reference (inheritance,
//     conformances, parameters, return types, property types, extended types),
//     not a declaration name.
//
// calls/type_ref are attributed to the enclosing function/method/initializer.
func (s *SwiftExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		startLine, _, startByte, _ := GetNodeLocation(node)
		switch node.Type() {
		case "import_declaration":
			if id := FindChildByType(node, "identifier"); id != nil {
				if path := GetNodeContent(id, sourceCode); path != "" {
					edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: path, StartLine: startLine})
				}
			}
		case "call_expression":
			if node.NamedChildCount() == 0 {
				continue
			}
			if name := swiftCalleeName(node.NamedChild(0), sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "type_identifier":
			if isDeclarationName(node) {
				continue
			}
			name := GetNodeContent(node, sourceCode)
			if name == "" || name == "Self" {
				continue
			}
			edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeTypeRef, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
		}
	}
	return edges, nil
}

// swiftCalleeName resolves the called name from the callee child of a call_expression.
func swiftCalleeName(callee *sitter.Node, sourceCode []byte) string {
	if callee == nil {
		return ""
	}
	switch callee.Type() {
	case "simple_identifier":
		return GetNodeContent(callee, sourceCode)
	case "navigation_expression":
		if suf := callee.ChildByFieldName("suffix"); suf != nil {
			if id := suf.ChildByFieldName("suffix"); id != nil {
				return GetNodeContent(id, sourceCode)
			}
			if id := FindChildByType(suf, "simple_identifier"); id != nil {
				return GetNodeContent(id, sourceCode)
			}
		}
	}
	return ""
}
