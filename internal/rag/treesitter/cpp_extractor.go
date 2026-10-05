// Package treesitter provides C++ language symbol extraction.
package treesitter

import (
	"strings"

	sitter "github.com/madeindigio/go-tree-sitter"
)

// CPPExtractor extracts symbols from C++ source code.
//
// Namespaces become part of the name path (namespace a { class B {} } ->
// "/a/B"). Out-of-line definitions such as `int Foo::bar() {}` are attributed
// to their class (name path "/Foo/bar", parent = the class symbol when it is
// defined in the same file). C is handled by CExtractor and is unaffected.
type CPPExtractor struct {
	BaseExtractor
}

// NewCPPExtractor creates a new C++ extractor
func NewCPPExtractor(config WalkerConfig) *CPPExtractor {
	return &CPPExtractor{
		BaseExtractor: NewBaseExtractor(LanguageCPP, config),
	}
}

// GetSymbolTypes returns the types of symbols the C++ extractor can find
func (c *CPPExtractor) GetSymbolTypes() []SymbolType {
	return []SymbolType{
		SymbolTypeNamespace,
		SymbolTypeClass,
		SymbolTypeStruct,
		SymbolTypeEnum,
		SymbolTypeEnumMember,
		SymbolTypeFunction,
		SymbolTypeMethod,
		SymbolTypeConstructor,
		SymbolTypeField,
		SymbolTypeVariable,
		SymbolTypeConstant,
		SymbolTypeTypeAlias,
	}
}

// cppScope carries the per-file extraction state.
type cppScope struct {
	src       []byte
	filePath  string
	projectID string
	byPath    map[string]*CodeSymbol // name path -> type/namespace symbol
}

// cppCtx is the lexical context of a node being extracted.
type cppCtx struct {
	path      string
	parentID  *string
	className string // enclosing class name, "" outside classes
}

// ExtractSymbols extracts all symbols from C++ source code
func (c *CPPExtractor) ExtractSymbols(tree *sitter.Tree, sourceCode []byte, filePath string, projectID string) ([]*CodeSymbol, error) {
	sc := &cppScope{src: sourceCode, filePath: filePath, projectID: projectID, byPath: map[string]*CodeSymbol{}}
	return c.extractChildren(tree.RootNode(), sc, cppCtx{}), nil
}

func (c *CPPExtractor) extractChildren(container *sitter.Node, sc *cppScope, ctx cppCtx) []*CodeSymbol {
	var out []*CodeSymbol
	for i := 0; i < int(container.NamedChildCount()); i++ {
		if ch := container.NamedChild(i); ch != nil {
			out = append(out, c.extractNode(ch, sc, ctx)...)
		}
	}
	return out
}

func (c *CPPExtractor) extractNode(node *sitter.Node, sc *cppScope, ctx cppCtx) []*CodeSymbol {
	switch node.Type() {
	case "namespace_definition":
		return c.extractNamespace(node, sc, ctx)
	case "class_specifier":
		return c.extractClass(node, sc, ctx, SymbolTypeClass)
	case "struct_specifier", "union_specifier":
		return c.extractClass(node, sc, ctx, SymbolTypeStruct)
	case "enum_specifier":
		return c.extractEnum(node, sc, ctx)
	case "template_declaration":
		return c.extractTemplate(node, sc, ctx)
	case "function_definition":
		return c.single(c.extractFunction(node, sc, ctx))
	case "declaration", "field_declaration":
		return c.extractDeclaration(node, sc, ctx)
	case "alias_declaration":
		if n := node.ChildByFieldName("name"); n != nil {
			return c.single(c.makeSymbol(node, sc, ctx, SymbolTypeTypeAlias, GetNodeContent(n, sc.src)))
		}
	case "type_definition":
		return c.extractTypedef(node, sc, ctx)
	case "preproc_if", "preproc_ifdef", "preproc_else", "preproc_elif", "preproc_elifdef", "declaration_list", "linkage_specification":
		// Transparent wrappers: members keep the enclosing context.
		target := node
		if node.Type() == "linkage_specification" {
			if b := node.ChildByFieldName("body"); b != nil {
				target = b
			}
		}
		return c.extractChildren(target, sc, ctx)
	}
	return nil
}

func (c *CPPExtractor) single(s *CodeSymbol) []*CodeSymbol {
	if s == nil {
		return nil
	}
	return []*CodeSymbol{s}
}

func (c *CPPExtractor) makeSymbol(node *sitter.Node, sc *cppScope, ctx cppCtx, st SymbolType, name string) *CodeSymbol {
	sym := c.CreateSymbol(node, sc.src, st, name, c.BuildNamePath(ctx.path, name), sc.filePath, sc.projectID, ctx.parentID)
	sym.DocString = c.ExtractDocString(node, sc.src)
	return sym
}

func (c *CPPExtractor) extractNamespace(node *sitter.Node, sc *cppScope, ctx cppCtx) []*CodeSymbol {
	body := node.ChildByFieldName("body")
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		// Anonymous namespace: members stay in the enclosing scope.
		if body != nil {
			return c.extractChildren(body, sc, ctx)
		}
		return nil
	}
	name := GetNodeContent(nameNode, sc.src)
	// `namespace a::b {}` is a single name node containing "::".
	namePath := ctx.path
	for _, seg := range strings.Split(name, "::") {
		namePath = c.BuildNamePath(namePath, seg)
	}
	sym := c.CreateSymbol(node, sc.src, SymbolTypeNamespace, name, namePath, sc.filePath, sc.projectID, ctx.parentID)
	sym.DocString = c.ExtractDocString(node, sc.src)
	sc.byPath[namePath] = sym
	out := []*CodeSymbol{sym}
	if body != nil {
		members := c.extractChildren(body, sc, cppCtx{path: namePath, parentID: &sym.ID})
		sym.Children = append(sym.Children, cppDirectChildren(members, sym.ID)...)
		out = append(out, members...)
	}
	return out
}

func cppDirectChildren(list []*CodeSymbol, id string) []*CodeSymbol {
	var out []*CodeSymbol
	for _, s := range list {
		if s.ParentID != nil && *s.ParentID == id {
			out = append(out, s)
		}
	}
	return out
}

// extractClass handles class/struct/union specifiers that have a body.
func (c *CPPExtractor) extractClass(node *sitter.Node, sc *cppScope, ctx cppCtx, st SymbolType) []*CodeSymbol {
	body := node.ChildByFieldName("body")
	nameNode := node.ChildByFieldName("name")
	if body == nil {
		return nil // forward declaration / elaborated type use
	}
	if nameNode == nil {
		// Anonymous struct/union: surface its members in the enclosing scope.
		return c.extractChildren(body, sc, ctx)
	}
	name := cppSimpleName(GetNodeContent(nameNode, sc.src))
	sym := c.makeSymbol(node, sc, ctx, st, name)
	sc.byPath[sym.NamePath] = sym
	out := []*CodeSymbol{sym}
	members := c.extractChildren(body, sc, cppCtx{path: sym.NamePath, parentID: &sym.ID, className: name})
	sym.Children = append(sym.Children, cppDirectChildren(members, sym.ID)...)
	return append(out, members...)
}

// extractEnum handles enum / enum class specifiers and their enumerators.
func (c *CPPExtractor) extractEnum(node *sitter.Node, sc *cppScope, ctx cppCtx) []*CodeSymbol {
	body := node.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	nameNode := node.ChildByFieldName("name")
	memberCtx := ctx
	var out []*CodeSymbol
	var sym *CodeSymbol
	if nameNode != nil {
		sym = c.makeSymbol(node, sc, ctx, SymbolTypeEnum, GetNodeContent(nameNode, sc.src))
		out = append(out, sym)
		memberCtx = cppCtx{path: sym.NamePath, parentID: &sym.ID}
	}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		e := body.NamedChild(i)
		if e == nil || e.Type() != "enumerator" {
			continue
		}
		if n := e.ChildByFieldName("name"); n != nil {
			m := c.makeSymbol(e, sc, memberCtx, SymbolTypeEnumMember, GetNodeContent(n, sc.src))
			if sym != nil {
				sym.Children = append(sym.Children, m)
			}
			out = append(out, m)
		}
	}
	return out
}

// extractTemplate unwraps template_declaration and extracts the templated entity.
func (c *CPPExtractor) extractTemplate(node *sitter.Node, sc *cppScope, ctx cppCtx) []*CodeSymbol {
	var out []*CodeSymbol
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch == nil || ch.Type() == "template_parameter_list" {
			continue
		}
		got := c.extractNode(ch, sc, ctx)
		if len(got) > 0 && got[0].DocString == "" {
			got[0].DocString = c.ExtractDocString(node, sc.src)
		}
		out = append(out, got...)
	}
	return out
}

// extractFunction builds a symbol for a function_definition.
func (c *CPPExtractor) extractFunction(node *sitter.Node, sc *cppScope, ctx cppCtx) *CodeSymbol {
	decl := node.ChildByFieldName("declarator")
	if decl == nil {
		return nil
	}
	scope, name, isFunc := cppDeclarator(decl, sc.src)
	if name == "" || !isFunc {
		return nil
	}
	return c.functionSymbol(node, sc, ctx, scope, name)
}

// functionSymbol creates Function/Method/Constructor symbols, attributing
// qualified definitions (Foo::bar) to their class.
func (c *CPPExtractor) functionSymbol(node *sitter.Node, sc *cppScope, ctx cppCtx, scope []string, name string) *CodeSymbol {
	path := ctx.path
	parentID := ctx.parentID
	className := ctx.className
	st := SymbolTypeFunction
	if len(scope) > 0 {
		for _, s := range scope {
			path = c.BuildNamePath(path, s)
		}
		className = scope[len(scope)-1]
		if owner, ok := sc.byPath[path]; ok {
			parentID = &owner.ID
		}
		st = SymbolTypeMethod
	} else if ctx.className != "" {
		st = SymbolTypeMethod
	}
	if className != "" && name == className {
		st = SymbolTypeConstructor
	}
	sym := c.CreateSymbol(node, sc.src, st, name, c.BuildNamePath(path, name), sc.filePath, sc.projectID, parentID)
	sym.DocString = c.ExtractDocString(node, sc.src)
	sym.Signature = c.signature(node, sc.src)
	return sym
}

// extractDeclaration handles `declaration` (namespace scope, ctor/dtor
// prototypes) and `field_declaration` (class members).
func (c *CPPExtractor) extractDeclaration(node *sitter.Node, sc *cppScope, ctx cppCtx) []*CodeSymbol {
	var out []*CodeSymbol

	// A class/struct/enum defined inline: `struct P { ... } p;`
	if t := node.ChildByFieldName("type"); t != nil {
		switch t.Type() {
		case "class_specifier", "struct_specifier", "union_specifier", "enum_specifier":
			out = append(out, c.extractNode(t, sc, ctx)...)
		}
	}

	isStaticConst := false
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch == nil {
			continue
		}
		txt := ""
		switch ch.Type() {
		case "storage_class_specifier", "type_qualifier":
			txt = GetNodeContent(ch, sc.src)
		}
		if txt == "constexpr" || txt == "const" {
			isStaticConst = true
		}
	}

	for _, decl := range fieldChildren(node, "declarator") {
		scope, name, isFunc := cppDeclarator(decl, sc.src)
		if name == "" {
			continue
		}
		if isFunc {
			out = append(out, c.functionSymbol(node, sc, ctx, scope, name))
			continue
		}
		st := SymbolTypeVariable
		if ctx.className != "" {
			st = SymbolTypeField
		}
		if isStaticConst && (node.ChildByFieldName("default_value") != nil || decl.Type() == "init_declarator") {
			st = SymbolTypeConstant
		}
		out = append(out, c.makeSymbol(node, sc, ctx, st, name))
	}
	return out
}

// fieldChildren returns every child of parent stored under the given field
// (a declaration may carry several declarators).
func fieldChildren(parent *sitter.Node, field string) []*sitter.Node {
	var out []*sitter.Node
	for i := 0; i < int(parent.ChildCount()); i++ {
		if parent.FieldNameForChild(i) == field {
			if ch := parent.Child(i); ch != nil {
				out = append(out, ch)
			}
		}
	}
	return out
}

// extractTypedef handles `typedef <type> Name;` including typedef struct {...} Name;
func (c *CPPExtractor) extractTypedef(node *sitter.Node, sc *cppScope, ctx cppCtx) []*CodeSymbol {
	var out []*CodeSymbol
	if t := node.ChildByFieldName("type"); t != nil {
		switch t.Type() {
		case "class_specifier", "struct_specifier", "union_specifier", "enum_specifier":
			out = append(out, c.extractNode(t, sc, ctx)...)
		}
	}
	for _, d := range fieldChildren(node, "declarator") {
		_, name, _ := cppDeclarator(d, sc.src)
		if name == "" && d.Type() == "type_identifier" {
			name = GetNodeContent(d, sc.src)
		}
		if name != "" {
			out = append(out, c.makeSymbol(node, sc, ctx, SymbolTypeTypeAlias, name))
		}
	}
	return out
}

// signature returns the text from the return type (or declarator) to the end
// of the parameter list.
func (c *CPPExtractor) signature(node *sitter.Node, src []byte) string {
	decl := node.ChildByFieldName("declarator")
	if decl == nil {
		return ""
	}
	fd := cppFindFunctionDeclarator(decl)
	if fd == nil {
		return ""
	}
	params := fd.ChildByFieldName("parameters")
	if params == nil {
		return ""
	}
	start := decl.StartByte()
	if t := node.ChildByFieldName("type"); t != nil {
		start = t.StartByte()
	}
	end := params.EndByte()
	if int(end) > len(src) || end < start {
		return ""
	}
	return string(src[start:end])
}

func cppFindFunctionDeclarator(n *sitter.Node) *sitter.Node {
	for n != nil {
		if n.Type() == "function_declarator" {
			return n
		}
		if d := n.ChildByFieldName("declarator"); d != nil {
			n = d
			continue
		}
		var next *sitter.Node
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if ch := n.NamedChild(i); ch != nil && strings.HasSuffix(ch.Type(), "declarator") {
				next = ch
				break
			}
		}
		n = next
	}
	return nil
}

// cppDeclarator resolves a declarator to (qualifier scope, name, isFunction).
// Pointer/reference/array wrappers are looked through.
func cppDeclarator(n *sitter.Node, src []byte) (scope []string, name string, isFunc bool) {
	if n == nil {
		return nil, "", false
	}
	switch n.Type() {
	case "function_declarator":
		s, nm, _ := cppDeclarator(n.ChildByFieldName("declarator"), src)
		return s, nm, true
	case "pointer_declarator", "array_declarator", "init_declarator", "parenthesized_declarator":
		if d := n.ChildByFieldName("declarator"); d != nil {
			return cppDeclarator(d, src)
		}
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if ch := n.NamedChild(i); ch != nil && strings.HasSuffix(ch.Type(), "declarator") {
				return cppDeclarator(ch, src)
			}
		}
	case "reference_declarator":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			if ch := n.NamedChild(i); ch != nil {
				if s, nm, f := cppDeclarator(ch, src); nm != "" {
					return s, nm, f
				}
			}
		}
	case "identifier", "field_identifier", "type_identifier", "operator_name", "destructor_name":
		return nil, GetNodeContent(n, src), false
	case "template_function", "template_method":
		if nm := n.ChildByFieldName("name"); nm != nil {
			return nil, GetNodeContent(nm, src), false
		}
	case "qualified_identifier":
		var sc []string
		if s := n.ChildByFieldName("scope"); s != nil {
			for _, seg := range strings.Split(cppSimpleScope(GetNodeContent(s, src)), "::") {
				if seg != "" {
					sc = append(sc, seg)
				}
			}
		}
		inner := n.ChildByFieldName("name")
		if inner == nil {
			return sc, "", false
		}
		is, nm, f := cppDeclarator(inner, src)
		return append(sc, is...), nm, f
	}
	return nil, "", false
}

// cppSimpleScope strips template argument lists from a scope text
// (Foo<T>::Bar -> Foo::Bar).
func cppSimpleScope(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && r != ' ' {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// cppSimpleName strips template arguments of a class name (Foo<int> -> Foo).
func cppSimpleName(s string) string {
	if i := strings.Index(s, "<"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
