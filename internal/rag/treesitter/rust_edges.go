package treesitter

import (
	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for Rust. It emits:
//   - imports:  use declarations (DstPath = the use tree as written, e.g.
//     "std::collections::HashMap"), `extern crate x` (x) and out-of-line
//     `mod x;` declarations (x).
//   - calls:    call expressions (bare identifier, last segment of a scoped
//     path, method name of a field expression) and macro invocations.
//   - type_ref: every type_identifier that is a reference (fields, params,
//     return types, impl targets/traits, generics), not a declaration name.
//
// calls/type_ref are attributed to the enclosing function/method when any.
func (r *RustExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		startLine, _, startByte, _ := GetNodeLocation(node)
		switch node.Type() {
		case "use_declaration":
			if arg := node.ChildByFieldName("argument"); arg != nil {
				if path := GetNodeContent(arg, sourceCode); path != "" {
					edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: path, StartLine: startLine})
				}
			}
		case "extern_crate_declaration":
			if name := node.ChildByFieldName("name"); name != nil {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: GetNodeContent(name, sourceCode), StartLine: startLine})
			}
		case "mod_item":
			// Only out-of-line modules (`mod foo;`) refer to another file.
			if node.ChildByFieldName("body") == nil {
				if name := node.ChildByFieldName("name"); name != nil {
					edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: GetNodeContent(name, sourceCode), StartLine: startLine})
				}
			}
		case "call_expression":
			if name := rustCalleeName(node.ChildByFieldName("function"), sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "macro_invocation":
			if name := rustCalleeName(node.ChildByFieldName("macro"), sourceCode); name != "" {
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

// rustCalleeName resolves the called name from a call's function node (or a
// macro_invocation's macro node).
func rustCalleeName(fn *sitter.Node, sourceCode []byte) string {
	if fn == nil {
		return ""
	}
	switch fn.Type() {
	case "identifier":
		return GetNodeContent(fn, sourceCode)
	case "scoped_identifier":
		if n := fn.ChildByFieldName("name"); n != nil {
			return GetNodeContent(n, sourceCode)
		}
	case "field_expression":
		if f := fn.ChildByFieldName("field"); f != nil {
			return GetNodeContent(f, sourceCode)
		}
	case "generic_function":
		return rustCalleeName(fn.ChildByFieldName("function"), sourceCode)
	}
	return ""
}

// isDeclarationName reports whether node is the `name` field of its parent
// (a declaration such as struct_item or a type parameter) rather than a
// reference. A scoped_type_identifier's name (fmt::Result) is a real reference.
func isDeclarationName(node *sitter.Node) bool {
	parent := node.Parent()
	if parent == nil || parent.Type() == "scoped_type_identifier" {
		return false
	}
	n := parent.ChildByFieldName("name")
	return n != nil && n.StartByte() == node.StartByte() && n.EndByte() == node.EndByte()
}
