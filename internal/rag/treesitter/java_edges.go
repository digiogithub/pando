package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for Java. It emits:
//   - imports:  import declarations (DstPath = qualified name, with ".*" for
//     wildcard imports).
//   - calls:    method invocations (method name) and object creations
//     (`new Foo()` → Foo), attributed to the enclosing method/constructor.
//   - type_ref: every type_identifier (fields, params, return types,
//     extends/implements, generics, casts, ...).
func (j *JavaExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
	if tree == nil {
		return nil, nil
	}
	var edges []*CodeEdge
	iter := NewNodeIterator(tree.RootNode())
	for node := iter.Next(); node != nil; node = iter.Next() {
		startLine, _, startByte, _ := GetNodeLocation(node)
		switch node.Type() {
		case "import_declaration":
			var path string
			wildcard := false
			for i := 0; i < int(node.NamedChildCount()); i++ {
				c := node.NamedChild(i)
				if c == nil {
					continue
				}
				switch c.Type() {
				case "scoped_identifier", "identifier":
					path = GetNodeContent(c, sourceCode)
				case "asterisk":
					wildcard = true
				}
			}
			if path == "" {
				continue
			}
			if wildcard {
				path += ".*"
			}
			edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeImports, DstPath: path, StartLine: startLine})
		case "method_invocation":
			if n := node.ChildByFieldName("name"); n != nil {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: GetNodeContent(n, sourceCode), StartLine: startLine})
			}
		case "object_creation_expression":
			if name := javaTypeName(node.ChildByFieldName("type"), sourceCode); name != "" {
				edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeCalls, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
			}
		case "type_identifier":
			name := GetNodeContent(node, sourceCode)
			if name == "" || name == "var" {
				continue
			}
			edges = append(edges, &CodeEdge{ProjectID: projectID, FilePath: filePath, EdgeType: EdgeTypeRef, SrcSymbolID: enclosingSymbolID(symbols, startByte), DstName: name, StartLine: startLine})
		}
	}
	return edges, nil
}

// javaTypeName returns the simple name of a type node (type_identifier,
// scoped_type_identifier → last segment, generic_type → its base type).
func javaTypeName(n *sitter.Node, sourceCode []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "type_identifier":
		return GetNodeContent(n, sourceCode)
	case "scoped_type_identifier":
		txt := GetNodeContent(n, sourceCode)
		if i := strings.LastIndex(txt, "."); i >= 0 {
			return txt[i+1:]
		}
		return txt
	case "generic_type":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c != nil && (c.Type() == "type_identifier" || c.Type() == "scoped_type_identifier") {
				return javaTypeName(c, sourceCode)
			}
		}
	}
	return ""
}
