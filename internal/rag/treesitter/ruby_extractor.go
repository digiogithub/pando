// Package treesitter provides Ruby language symbol extraction.
package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// RubyExtractor extracts symbols from Ruby source code
type RubyExtractor struct {
	BaseExtractor
}

// NewRubyExtractor creates a new Ruby extractor
func NewRubyExtractor(config WalkerConfig) *RubyExtractor {
	return &RubyExtractor{
		BaseExtractor: NewBaseExtractor(LanguageRuby, config),
	}
}

// GetSymbolTypes returns the types of symbols the Ruby extractor can find
func (r *RubyExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeModule,
		SymbolTypeClass,
		SymbolTypeMethod,
		SymbolTypeFunction,
		SymbolTypeConstructor,
		SymbolTypeConstant,
		SymbolTypeProperty,
	}
}

// ExtractSymbols extracts all symbols from Ruby source code
func (r *RubyExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	var symbols []*CodeSymbol
	root := tree.RootNode()
	for i := 0; i < int(root.NamedChildCount()); i++ {
		if child := root.NamedChild(i); child != nil {
			symbols = append(symbols, r.extractNode(child, sourceCode, filePath, projectID, "", nil, false)...)
		}
	}
	return symbols, nil
}

// extractNode extracts symbols from a node. inType is true when the node sits
// directly inside a class/module body (methods become SymbolTypeMethod).
func (r *RubyExtractor) extractNode(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string, inType bool) []*CodeSymbol {
	switch node.Type() {
	case "module":
		return r.extractContainer(node, sourceCode, filePath, projectID, parentPath, parentID, SymbolTypeModule)
	case "class":
		return r.extractContainer(node, sourceCode, filePath, projectID, parentPath, parentID, SymbolTypeClass)
	case "singleton_class":
		// `class << self`: members are class-level methods of the enclosing type.
		var out []*CodeSymbol
		if body := node.ChildByFieldName("body"); body != nil {
			for i := 0; i < int(body.NamedChildCount()); i++ {
				c := body.NamedChild(i)
				if c == nil {
					continue
				}
				if c.Type() == "method" {
					if name := r.fieldText(c, "name", sourceCode); name != "" {
						full := "self." + name
						out = append(out, r.methodSymbol(c, sourceCode, filePath, projectID, parentPath, parentID, SymbolTypeMethod, full, "def "+full))
					}
					continue
				}
				out = append(out, r.extractNode(c, sourceCode, filePath, projectID, parentPath, parentID, true)...)
			}
		}
		return out
	case "method":
		name := r.fieldText(node, "name", sourceCode)
		if name == "" {
			return nil
		}
		st := SymbolTypeFunction
		if inType {
			st = SymbolTypeMethod
			if name == "initialize" {
				st = SymbolTypeConstructor
			}
		}
		return []*CodeSymbol{r.methodSymbol(node, sourceCode, filePath, projectID, parentPath, parentID, st, name, "def "+name)}
	case "singleton_method":
		name := r.fieldText(node, "name", sourceCode)
		obj := r.fieldText(node, "object", sourceCode)
		if name == "" {
			return nil
		}
		full := name
		if obj != "" {
			full = obj + "." + name
		}
		return []*CodeSymbol{r.methodSymbol(node, sourceCode, filePath, projectID, parentPath, parentID, SymbolTypeMethod, full, "def "+full)}
	case "assignment":
		left := node.ChildByFieldName("left")
		if left == nil || (left.Type() != "constant" && left.Type() != "scope_resolution") {
			return nil
		}
		name := GetNodeContent(left, sourceCode)
		return []*CodeSymbol{r.CreateSymbol(node, sourceCode, SymbolTypeConstant, name, r.BuildNamePath(parentPath, name), filePath, projectID, parentID)}
	case "call":
		return r.extractAttr(node, sourceCode, filePath, projectID, parentPath, parentID)
	}
	return nil
}

func (r *RubyExtractor) fieldText(node *sitter.Node, field string, sourceCode []byte) string {
	if n := node.ChildByFieldName(field); n != nil {
		return GetNodeContent(n, sourceCode)
	}
	return ""
}

func (r *RubyExtractor) methodSymbol(node *sitter.Node, sourceCode []byte, filePath, projectID, parentPath string, parentID *string, st SymbolType, name, sig string) *CodeSymbol {
	s := r.CreateSymbol(node, sourceCode, st, name, r.BuildNamePath(parentPath, name), filePath, projectID, parentID)
	s.DocString = r.ExtractDocString(node, sourceCode)
	if p := node.ChildByFieldName("parameters"); p != nil {
		sig += GetNodeContent(p, sourceCode)
	}
	s.Signature = sig
	return s
}

func (r *RubyExtractor) extractContainer(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string, st SymbolType) []*CodeSymbol {
	name := r.fieldText(node, "name", sourceCode)
	if name == "" {
		return nil
	}
	namePath := r.BuildNamePath(parentPath, name)
	symbol := r.CreateSymbol(node, sourceCode, st, name, namePath, filePath, projectID, parentID)
	symbol.DocString = r.ExtractDocString(node, sourceCode)
	if sc := node.ChildByFieldName("superclass"); sc != nil {
		symbol.Signature = "class " + name + " " + strings.TrimSpace(GetNodeContent(sc, sourceCode))
	}
	symbols := []*CodeSymbol{symbol}
	ms := r.extractBody(node.ChildByFieldName("body"), sourceCode, filePath, projectID, namePath, &symbol.ID, true)
	symbol.Children = append(symbol.Children, ms...)
	return append(symbols, ms...)
}

func (r *RubyExtractor) extractBody(body *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string, inType bool) []*CodeSymbol {
	if body == nil {
		return nil
	}
	var out []*CodeSymbol
	for i := 0; i < int(body.NamedChildCount()); i++ {
		if c := body.NamedChild(i); c != nil {
			out = append(out, r.extractNode(c, sourceCode, filePath, projectID, parentPath, parentID, inType)...)
		}
	}
	return out
}

// extractAttr turns attr_accessor/attr_reader/attr_writer calls into properties.
func (r *RubyExtractor) extractAttr(node *sitter.Node, sourceCode []byte, filePath string, projectID string, parentPath string, parentID *string) []*CodeSymbol {
	if node.ChildByFieldName("receiver") != nil {
		return nil
	}
	m := r.fieldText(node, "method", sourceCode)
	if m != "attr_accessor" && m != "attr_reader" && m != "attr_writer" {
		return nil
	}
	args := node.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var out []*CodeSymbol
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil {
			continue
		}
		var name string
		switch a.Type() {
		case "simple_symbol":
			name = strings.TrimPrefix(GetNodeContent(a, sourceCode), ":")
		case "string":
			name = unquote(GetNodeContent(a, sourceCode))
		}
		if name == "" {
			continue
		}
		s := r.CreateSymbol(a, sourceCode, SymbolTypeProperty, name, r.BuildNamePath(parentPath, name), filePath, projectID, parentID)
		s.Signature = m + " :" + name
		out = append(out, s)
	}
	return out
}
