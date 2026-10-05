package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// ExtractEdges implements EdgeExtractor for Ruby. It emits:
//   - imports:  require / require_relative / load / autoload calls (DstPath =
//     the string argument).
//   - calls:    method calls (method name), attributed to the enclosing method.
//   - type_ref: superclass constants, include/extend/prepend arguments and
//     constant receivers (`Util.go`).
func (r *RubyExtractor) ExtractEdges(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string, symbols []*CodeSymbol) ([]*CodeEdge, error) {
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
		case "superclass":
			for i := 0; i < int(node.NamedChildCount()); i++ {
				if n := rubyConstName(node.NamedChild(i), sourceCode); n != "" {
					add(EdgeTypeRef, startLine, startByte, n, "")
				}
			}
		case "call":
			m := ""
			if mn := node.ChildByFieldName("method"); mn != nil {
				m = GetNodeContent(mn, sourceCode)
			}
			if m == "" {
				continue
			}
			recv := node.ChildByFieldName("receiver")
			args := node.ChildByFieldName("arguments")
			if recv == nil {
				switch m {
				case "require", "require_relative", "load", "autoload":
					if p := rubyFirstStringArg(args, sourceCode); p != "" {
						add(EdgeImports, startLine, startByte, "", p)
					}
					continue
				case "include", "extend", "prepend":
					if args != nil {
						for i := 0; i < int(args.NamedChildCount()); i++ {
							if n := rubyConstName(args.NamedChild(i), sourceCode); n != "" {
								add(EdgeTypeRef, startLine, startByte, n, "")
							}
						}
					}
					continue
				case "attr_accessor", "attr_reader", "attr_writer":
					continue
				}
			} else if n := rubyConstName(recv, sourceCode); n != "" {
				add(EdgeTypeRef, startLine, startByte, n, "")
			}
			add(EdgeCalls, startLine, startByte, m, "")
		}
	}
	return edges, nil
}

// rubyConstName returns the simple name of a constant / scope_resolution node
// (last segment), or "" for any other node.
func rubyConstName(n *sitter.Node, sourceCode []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "constant":
		return GetNodeContent(n, sourceCode)
	case "scope_resolution":
		if nm := n.ChildByFieldName("name"); nm != nil {
			return GetNodeContent(nm, sourceCode)
		}
		txt := GetNodeContent(n, sourceCode)
		return txt[strings.LastIndex(txt, "::")+2:]
	}
	return ""
}

// rubyFirstStringArg returns the literal text of the first plain string
// argument, ignoring interpolated strings.
func rubyFirstStringArg(args *sitter.Node, sourceCode []byte) string {
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil || a.Type() != "string" {
			continue
		}
		for j := 0; j < int(a.NamedChildCount()); j++ {
			if c := a.NamedChild(j); c != nil && c.Type() == "interpolation" {
				return ""
			}
		}
		return unquote(GetNodeContent(a, sourceCode))
	}
	return ""
}
