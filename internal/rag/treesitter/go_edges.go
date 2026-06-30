// Package treesitter provides Go code property-graph edge extraction (lean-ctx Phase 4).
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for Go. It emits:
//   - imports: one per import spec (file-level), DstPath = the import path string.
//   - calls:   one per call expression, DstName = the callee identifier (the
//     trailing selector for qualified calls), attributed to the enclosing
//     function/method symbol when one contains the call.
func (g *GoExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		switch node.Type() {
		case "import_spec":
			pathNode := node.ChildByFieldName("path")
			if pathNode == nil {
				continue
			}
			path := unquote(GetNodeContent(pathNode, sourceCode))
			if path == "" {
				continue
			}
			startLine, _, _, _ := GetNodeLocation(node)
			edges = append(edges, &CodeEdge{
				ProjectID: projectID,
				FilePath:  filePath,
				EdgeType:  EdgeImports,
				DstPath:   path,
				StartLine: startLine,
			})

		case "call_expression":
			name := goCalleeName(node, sourceCode)
			if name == "" {
				continue
			}
			startLine, _, startByte, _ := GetNodeLocation(node)
			edges = append(edges, &CodeEdge{
				ProjectID:   projectID,
				FilePath:    filePath,
				EdgeType:    EdgeCalls,
				SrcSymbolID: enclosingSymbolID(symbols, startByte),
				DstName:     name,
				StartLine:   startLine,
			})
		}
	}
	return edges, nil
}

// goCalleeName returns the called identifier of a Go call_expression: for a
// qualified call like pkg.Func or recv.Method it returns the trailing selector
// (Func/Method); for a bare identifier call it returns that identifier. Type
// conversions and complex callees (e.g. f()()) yield "" and are skipped.
func goCalleeName(call *sitter.Node, sourceCode []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return GetNodeContent(fn, sourceCode)
	case "selector_expression":
		field := fn.ChildByFieldName("field")
		if field != nil {
			return GetNodeContent(field, sourceCode)
		}
	}
	return ""
}
