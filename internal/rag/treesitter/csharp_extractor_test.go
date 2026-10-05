package treesitter

import "testing"

func findSym(symbols []*CodeSymbol, path string) *CodeSymbol {
	for _, s := range symbols {
		if s.NamePath == path {
			return s
		}
	}
	return nil
}

func expectSym(t *testing.T, symbols []*CodeSymbol, path string, st SymbolType) *CodeSymbol {
	t.Helper()
	s := findSym(symbols, path)
	if s == nil {
		t.Errorf("missing symbol %q; have: %v", path, symPaths(symbols))
		return nil
	}
	if s.SymbolType != st {
		t.Errorf("symbol %q type = %s, want %s", path, s.SymbolType, st)
	}
	return s
}

func symPaths(symbols []*CodeSymbol) []string {
	var out []string
	for _, s := range symbols {
		out = append(out, string(s.SymbolType)+":"+s.NamePath)
	}
	return out
}

const csharpFixture = `using System;
using static System.Math;
using Json = System.Text.Json.JsonSerializer;

namespace Acme.Shop
{
    /// <summary>A product.</summary>
    /// <remarks>Immutable.</remarks>
    public class Product<T> : BaseEntity, IPriced
    {
        private int _stock = 1, _reserved;
        public const int MaxItems = 10;
        public event EventHandler Changed;
        public event Action Saved { add { } remove { } }
        public string Name { get; set; }
        public int this[int i] => i;

        public Product(string name)
        {
            Name = name;
            Init();
            var o = new Widget();
            this.Reset();
            logger?.Flush();
            Helper.Run<int>();
        }

        ~Product() { }

        public static Product<T> operator +(Product<T> a, Product<T> b) { return a; }

        public List<Order> Fetch(Customer c) => c.Orders();

        public delegate void Callback(int code);

        public class Inner { public void Go() { } }
    }

    public record Point(int X, int Y);
    public record struct Pair(int A, int B);
    public enum Status { Active, Inactive }
    public interface IPriced { decimal Price(); }
    public struct Money { public decimal Amount; }
}
`

func TestCSharpSymbols(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, _ := parseAndExtract(t, walker, parser, LanguageCSharp, "a.cs", csharpFixture)

	expectSym(t, symbols, "/Acme/Shop", SymbolTypeNamespace)
	prod := expectSym(t, symbols, "/Acme/Shop/Product", SymbolTypeClass)
	if prod != nil && prod.DocString == "" {
		t.Error("expected /// doc string on Product")
	} else if prod != nil && !contains(prod.DocString, "A product.") || prod != nil && !contains(prod.DocString, "Immutable.") {
		t.Errorf("doc string not collected fully: %q", prod.DocString)
	}
	base := "/Acme/Shop/Product"
	expectSym(t, symbols, base+"/_stock", SymbolTypeField)
	expectSym(t, symbols, base+"/_reserved", SymbolTypeField)
	expectSym(t, symbols, base+"/MaxItems", SymbolTypeConstant)
	expectSym(t, symbols, base+"/Changed", SymbolTypeField)
	expectSym(t, symbols, base+"/Saved", SymbolTypeProperty)
	expectSym(t, symbols, base+"/Name", SymbolTypeProperty)
	expectSym(t, symbols, base+"/this[]", SymbolTypeProperty)
	ctor := expectSym(t, symbols, base+"/Product", SymbolTypeConstructor)
	if ctor != nil && ctor.Signature == "" {
		t.Error("expected constructor signature")
	}
	expectSym(t, symbols, base+"/~Product", SymbolTypeMethod)
	expectSym(t, symbols, base+"/operator +", SymbolTypeMethod)
	fetch := expectSym(t, symbols, base+"/Fetch", SymbolTypeMethod)
	if fetch != nil && fetch.Signature != "List<Order> Fetch(Customer c)" {
		t.Errorf("unexpected signature %q", fetch.Signature)
	}
	expectSym(t, symbols, base+"/Callback", SymbolTypeTypeAlias)
	expectSym(t, symbols, base+"/Inner", SymbolTypeClass)
	expectSym(t, symbols, base+"/Inner/Go", SymbolTypeMethod)
	if prod != nil && len(prod.Children) == 0 {
		t.Error("Product should have children")
	}

	expectSym(t, symbols, "/Acme/Shop/Point", SymbolTypeClass)
	expectSym(t, symbols, "/Acme/Shop/Pair", SymbolTypeStruct)
	expectSym(t, symbols, "/Acme/Shop/Status", SymbolTypeEnum)
	expectSym(t, symbols, "/Acme/Shop/Status/Active", SymbolTypeEnumMember)
	expectSym(t, symbols, "/Acme/Shop/Status/Inactive", SymbolTypeEnumMember)
	expectSym(t, symbols, "/Acme/Shop/IPriced", SymbolTypeInterface)
	expectSym(t, symbols, "/Acme/Shop/IPriced/Price", SymbolTypeMethod)
	expectSym(t, symbols, "/Acme/Shop/Money", SymbolTypeStruct)
	expectSym(t, symbols, "/Acme/Shop/Money/Amount", SymbolTypeField)
}

func TestCSharpFileScopedNamespace(t *testing.T) {
	src := `using System;

namespace Acme.Core;

public class Service
{
    public void Run() { }
}

public enum Mode { A }
`
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, _ := parseAndExtract(t, walker, parser, LanguageCSharp, "b.cs", src)
	ns := expectSym(t, symbols, "/Acme/Core", SymbolTypeNamespace)
	svc := expectSym(t, symbols, "/Acme/Core/Service", SymbolTypeClass)
	expectSym(t, symbols, "/Acme/Core/Service/Run", SymbolTypeMethod)
	expectSym(t, symbols, "/Acme/Core/Mode/A", SymbolTypeEnumMember)
	if ns != nil && svc != nil && (svc.ParentID == nil || *svc.ParentID != ns.ID) {
		t.Error("Service should be a child of the file-scoped namespace")
	}
}

func TestCSharpEdges(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, edges := parseAndExtract(t, walker, parser, LanguageCSharp, "a.cs", csharpFixture)

	for _, p := range []string{"System", "System.Math", "System.Text.Json.JsonSerializer"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	if hasImport(edges, "Json") {
		t.Error("alias name must not be an import")
	}
	for _, call := range []string{"Init", "Widget", "Reset", "Flush", "Run", "Orders"} {
		if !hasCall(edges, call) {
			t.Errorf("missing call %q: %+v", call, edgesByType(edges, EdgeCalls))
		}
	}
	for _, ty := range []string{"BaseEntity", "IPriced", "EventHandler", "List", "Order", "Customer", "Widget", "Product"} {
		if !hasEdge(edges, EdgeTypeRef, ty) {
			t.Errorf("missing type_ref %q", ty)
		}
	}
	if hasEdge(edges, EdgeTypeRef, "var") {
		t.Error("var must not be a type_ref")
	}
	// Calls inside the constructor are attributed to it.
	var ctorID string
	for _, s := range symbols {
		if s.SymbolType == SymbolTypeConstructor {
			ctorID = s.ID
		}
	}
	found := false
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "Init" && e.SrcSymbolID == ctorID && ctorID != "" {
			found = true
		}
	}
	if !found {
		t.Error("Init call should be attributed to the constructor")
	}
}
