package treesitter

import "testing"

func TestSwiftEdges(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, edges := parseAndExtract(t, walker, parser, LanguageSwift, "a.swift", swiftFixture)

	for _, p := range []string{"Foundation", "Foo.Bar"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	for _, c := range []string{"print", "Square", "scale"} {
		if !hasCall(edges, c) {
			t.Errorf("missing call %q: %+v", c, edgesByType(edges, EdgeCalls))
		}
	}
	for _, ty := range []string{"Sendable", "Shape", "Double", "String", "Int", "CustomStringConvertible", "Square"} {
		if !hasEdge(edges, EdgeTypeRef, ty) {
			t.Errorf("missing type_ref %q", ty)
		}
	}
	// Declaration names are not references.
	if hasEdge(edges, EdgeTypeRef, "Counter") || hasEdge(edges, EdgeTypeRef, "Base") {
		t.Error("declaration names must not be type_ref edges")
	}
	// Attribution: scale(by:) call inside Square.double.
	var double string
	for _, s := range symbols {
		if s.NamePath == "/Square/double" {
			double = s.ID
		}
	}
	found := false
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "scale" && e.SrcSymbolID == double && double != "" {
			found = true
		}
	}
	if !found {
		t.Error("scale call not attributed to Square.double")
	}
}

func TestSwiftNavigationCall(t *testing.T) {
	src := `import UIKit

final class Vm {
    func run(store: Store) {
        store.items.save(1).flush()
    }
}
`
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	_, edges := parseAndExtract(t, walker, parser, LanguageSwift, "b.swift", src)
	for _, c := range []string{"save", "flush"} {
		if !hasCall(edges, c) {
			t.Errorf("missing call %q: %+v", c, edgesByType(edges, EdgeCalls))
		}
	}
	if !hasEdge(edges, EdgeTypeRef, "Store") {
		t.Error("missing type_ref Store")
	}
}
