package treesitter

import "testing"

func hasEdge(edges []*CodeEdge, et EdgeType, name string) bool {
	for _, e := range edgesByType(edges, et) {
		if e.DstName == name {
			return true
		}
	}
	return false
}

func TestRustEdges(t *testing.T) {
	src := `use std::collections::HashMap;
extern crate serde;
mod util;
mod inline { }

struct Store { items: HashMap<String, Item> }

impl Display for Store {
    fn show(&self, c: Config) -> Result<Item, Error> {
        let v = helper(1);
        let w = util::build();
        self.items.insert(1, 2);
        println!("x");
        v
    }
}
`
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, edges := parseAndExtract(t, walker, parser, LanguageRust, "a.rs", src)

	for _, p := range []string{"std::collections::HashMap", "serde", "util"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	if hasImport(edges, "inline") {
		t.Error("inline mod must not be an import")
	}
	for _, c := range []string{"helper", "build", "insert", "println"} {
		if !hasCall(edges, c) {
			t.Errorf("missing call %q", c)
		}
	}
	for _, ty := range []string{"HashMap", "Item", "Display", "Config", "Result", "Error", "String"} {
		if !hasEdge(edges, EdgeTypeRef, ty) {
			t.Errorf("missing type_ref %q", ty)
		}
	}
	var show string
	for _, s := range symbols {
		if s.Name == "show" {
			show = s.ID
		}
	}
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "helper" && (show == "" || e.SrcSymbolID != show) {
			t.Errorf("helper call not attributed to show: %q vs %q", e.SrcSymbolID, show)
		}
	}
}

func TestJavaEdges(t *testing.T) {
	src := `package a;
import java.util.List;
import java.io.*;

public class Svc extends Base implements Runnable {
    private Repo repo;
    public Result run(Config c) {
        Item i = new Item();
        repo.save(i);
        helper();
        return null;
    }
}
`
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()
	symbols, edges := parseAndExtract(t, walker, parser, LanguageJava, "A.java", src)

	for _, p := range []string{"java.util.List", "java.io.*"} {
		if !hasImport(edges, p) {
			t.Errorf("missing import %q: %+v", p, edgesByType(edges, EdgeImports))
		}
	}
	for _, c := range []string{"save", "helper", "Item"} {
		if !hasCall(edges, c) {
			t.Errorf("missing call %q", c)
		}
	}
	for _, ty := range []string{"Base", "Runnable", "Repo", "Result", "Config"} {
		if !hasEdge(edges, EdgeTypeRef, ty) {
			t.Errorf("missing type_ref %q", ty)
		}
	}
	var run string
	for _, s := range symbols {
		if s.Name == "run" {
			run = s.ID
		}
	}
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "save" && (run == "" || e.SrcSymbolID != run) {
			t.Errorf("save call not attributed to run")
		}
	}
}

func TestRustJavaSupportEdges(t *testing.T) {
	w := NewASTWalker(DefaultWalkerConfig())
	for _, l := range []Language{LanguageRust, LanguageJava} {
		if !w.SupportsEdges(l) {
			t.Errorf("%s should support edges", l)
		}
	}
}
