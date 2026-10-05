package treesitter

import "testing"

const cppFixture = `#include <vector>
#include "shop/product.h"

namespace shop {

/** A product. */
template <typename T>
class Product : public Base<T> {
public:
    Product(int id);
    ~Product();
    int price() const;
    void inl() { helper(); this->price(); util::run(); obj.go(); }
    int id_;
    static const int kMax = 10;
    Config* cfg_;
    enum class Kind { Small, Large };
};

struct Point { int x; int y; };
union Bits { int i; float f; };
enum class Color { Red, Green };
using Items = std::vector<Product<int>>;
typedef unsigned long ulong_t;

namespace detail { void hidden(); }

}  // namespace shop

int shop::Product::price() const { return id_; }
shop::Product::Product(int id) : id_(id) {}
shop::Product::~Product() {}

template <typename T>
T identity(T t) { return t; }

void run() {
    shop::Product p(1);
    Product* q = new Product(2);
    std::sort(a, b);
    auto n = make<Widget>();
}
`

func TestCPPSymbols(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, _ := parseAndExtract(t, walker, parser, LanguageCPP, "a.cpp", cppFixture)

	expectSym(t, symbols, "/shop", SymbolTypeNamespace)
	prod := expectSym(t, symbols, "/shop/Product", SymbolTypeClass)
	if prod != nil && prod.DocString == "" {
		t.Error("expected doc comment on templated class")
	}
	b := "/shop/Product"
	expectSym(t, symbols, b+"/Product", SymbolTypeConstructor)
	expectSym(t, symbols, b+"/~Product", SymbolTypeMethod)
	expectSym(t, symbols, b+"/price", SymbolTypeMethod)
	expectSym(t, symbols, b+"/inl", SymbolTypeMethod)
	expectSym(t, symbols, b+"/id_", SymbolTypeField)
	expectSym(t, symbols, b+"/kMax", SymbolTypeConstant)
	expectSym(t, symbols, b+"/cfg_", SymbolTypeField)
	expectSym(t, symbols, b+"/Kind", SymbolTypeEnum)
	expectSym(t, symbols, b+"/Kind/Small", SymbolTypeEnumMember)

	expectSym(t, symbols, "/shop/Point", SymbolTypeStruct)
	expectSym(t, symbols, "/shop/Point/x", SymbolTypeField)
	expectSym(t, symbols, "/shop/Bits", SymbolTypeStruct)
	expectSym(t, symbols, "/shop/Color", SymbolTypeEnum)
	expectSym(t, symbols, "/shop/Color/Green", SymbolTypeEnumMember)
	expectSym(t, symbols, "/shop/Items", SymbolTypeTypeAlias)
	expectSym(t, symbols, "/shop/ulong_t", SymbolTypeTypeAlias)
	expectSym(t, symbols, "/shop/detail", SymbolTypeNamespace)
	expectSym(t, symbols, "/shop/detail/hidden", SymbolTypeFunction)

	// Out-of-line definitions are attributed to their class.
	var outOfLine int
	for _, s := range symbols {
		if s.NamePath == "/shop/Product/price" || s.NamePath == "/shop/Product/Product" || s.NamePath == "/shop/Product/~Product" {
			outOfLine++
		}
	}
	if outOfLine < 3 {
		t.Errorf("expected in-class and out-of-line members, have: %v", symPaths(symbols))
	}
	expectSym(t, symbols, "/identity", SymbolTypeFunction)
	expectSym(t, symbols, "/run", SymbolTypeFunction)
}

func TestCPPQualifiedDefinitionParent(t *testing.T) {
	src := `class Foo { public: int bar(int x) const; Foo(); };
int Foo::bar(int x) const { return x; }
Foo::Foo() {}
`
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, _ := parseAndExtract(t, walker, parser, LanguageCPP, "b.cpp", src)
	foo := expectSym(t, symbols, "/Foo", SymbolTypeClass)
	var defs []*CodeSymbol
	for _, s := range symbols {
		if s.NamePath == "/Foo/bar" && s.Signature != "" && s.EndLine >= 2 && s.StartLine == 2 {
			defs = append(defs, s)
		}
	}
	if len(defs) != 1 {
		t.Fatalf("expected one out-of-line Foo::bar, have: %v", symPaths(symbols))
	}
	if foo != nil && (defs[0].ParentID == nil || *defs[0].ParentID != foo.ID) {
		t.Error("Foo::bar should have Foo as parent")
	}
	if defs[0].Signature != "int Foo::bar(int x)" {
		t.Errorf("unexpected signature %q", defs[0].Signature)
	}
	var ctors int
	for _, s := range symbols {
		if s.SymbolType == SymbolTypeConstructor {
			ctors++
		}
	}
	if ctors != 2 {
		t.Errorf("expected 2 constructors (prototype + definition), got %d", ctors)
	}
}

func TestCPPEdges(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, edges := parseAndExtract(t, walker, parser, LanguageCPP, "a.cpp", cppFixture)

	for _, p := range []string{"vector", "shop/product.h"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	for _, c := range []string{"helper", "price", "run", "go", "sort", "make", "Product"} {
		if !hasCall(edges, c) {
			t.Errorf("missing call %q: %+v", c, edgesByType(edges, EdgeCalls))
		}
	}
	for _, ty := range []string{"Base", "Config", "Product"} {
		if !hasEdge(edges, EdgeTypeRef, ty) {
			t.Errorf("missing type_ref %q", ty)
		}
	}
	// Declared names are not references; template parameter T is not either.
	for _, e := range edgesByType(edges, EdgeTypeRef) {
		if e.DstName == "Point" || e.DstName == "Bits" || e.DstName == "Items" || e.DstName == "ulong_t" {
			t.Errorf("declared name %q must not be a type_ref", e.DstName)
		}
	}
	var inlID string
	for _, s := range symbols {
		if s.Name == "inl" {
			inlID = s.ID
		}
	}
	found := false
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "helper" && e.SrcSymbolID == inlID && inlID != "" {
			found = true
		}
	}
	if !found {
		t.Error("helper call should be attributed to Product::inl")
	}
}

// C keeps using the C extractor and must not pick up C++ behaviour.
func TestCStillUsesCExtractor(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	ext, ok := walker.GetExtractor(LanguageC)
	if !ok {
		t.Fatal("no C extractor")
	}
	if _, isC := ext.(*CExtractor); !isC {
		t.Errorf("C extractor is %T", ext)
	}
}
