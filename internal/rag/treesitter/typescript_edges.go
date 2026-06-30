// Package treesitter provides TypeScript/JavaScript property-graph edge extraction (lean-ctx Phase 4).
package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for TypeScript (ESM/CommonJS-ish).
func (t *TypeScriptExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	return extractJSFamilyEdges(tree, sourceCode, filePath, projectID, symbols)
}

// ExtractEdges implements EdgeExtractor for JavaScript. TS and JS share the same
// import_statement / call_expression / member_expression node shapes, so they
// reuse one implementation.
func (j *JavaScriptExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	return extractJSFamilyEdges(tree, sourceCode, filePath, projectID, symbols)
}

// extractJSFamilyEdges emits imports + calls edges for the JS/TS grammar family:
//   - imports: ESM `import ... from "x"` (the source string) and CommonJS
//     `require("x")` calls, DstPath = the module specifier.
//   - calls:   call expressions, DstName = the callee identifier (the trailing
//     member for method calls), attributed to the enclosing function/method.
func extractJSFamilyEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		switch node.Type() {
		case "import_statement":
			src := node.ChildByFieldName("source")
			path := jsStringValue(src, sourceCode)
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
			// CommonJS require("x") → treat as an import edge.
			if path := jsRequirePath(node, sourceCode); path != "" {
				startLine, _, _, _ := GetNodeLocation(node)
				edges = append(edges, &CodeEdge{
					ProjectID: projectID,
					FilePath:  filePath,
					EdgeType:  EdgeImports,
					DstPath:   path,
					StartLine: startLine,
				})
				continue
			}
			name := jsCalleeName(node, sourceCode)
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

// jsStringValue returns the inner value of a JS/TS string node, preferring the
// string_fragment child and falling back to unquoting the raw text.
func jsStringValue(node *sitter.Node, sourceCode []byte) string {
	if node == nil {
		return ""
	}
	if frag := FindNamedChildByType(node, "string_fragment"); frag != nil {
		return GetNodeContent(frag, sourceCode)
	}
	return unquote(GetNodeContent(node, sourceCode))
}

// jsRequirePath returns the module specifier of a `require("x")` call, or "" if
// the call is not a single-string-argument require.
func jsRequirePath(call *sitter.Node, sourceCode []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "identifier" || GetNodeContent(fn, sourceCode) != "require" {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	arg := args.NamedChild(0)
	if arg == nil || arg.Type() != "string" {
		return ""
	}
	return jsStringValue(arg, sourceCode)
}

// jsCalleeName returns the called identifier of a JS/TS call_expression: the
// trailing member for method calls (obj.method → method) or the bare identifier.
func jsCalleeName(call *sitter.Node, sourceCode []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return GetNodeContent(fn, sourceCode)
	case "member_expression":
		prop := fn.ChildByFieldName("property")
		if prop != nil {
			return GetNodeContent(prop, sourceCode)
		}
	}
	return ""
}
