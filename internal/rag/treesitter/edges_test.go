package treesitter

import (
	"context"
	"testing"
)

// parseAndExtract is a small helper that parses src for lang and returns its
// symbols and edges via the shared walker.
func parseAndExtract(t *testing.T, walker *ASTWalker, parser *Parser, lang Language, filePath, src string) ([]*CodeSymbol, []*CodeEdge) {
	t.Helper()
	tree, err := parser.Parse(context.Background(), []byte(src), lang)
	if err != nil {
		t.Fatalf("parse %s: %v", lang, err)
	}
	defer tree.Close()
	symbols, err := walker.ExtractSymbols(tree, []byte(src), lang, filePath, "proj")
	if err != nil {
		t.Fatalf("extract symbols %s: %v", lang, err)
	}
	edges, err := walker.ExtractEdges(tree, []byte(src), lang, filePath, "proj", symbols)
	if err != nil {
		t.Fatalf("extract edges %s: %v", lang, err)
	}
	return symbols, edges
}

func edgesByType(edges []*CodeEdge, et EdgeType) []*CodeEdge {
	var out []*CodeEdge
	for _, e := range edges {
		if e.EdgeType == et {
			out = append(out, e)
		}
	}
	return out
}

func hasImport(edges []*CodeEdge, path string) bool {
	for _, e := range edgesByType(edges, EdgeImports) {
		if e.DstPath == path {
			return true
		}
	}
	return false
}

func hasCall(edges []*CodeEdge, name string) bool {
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == name {
			return true
		}
	}
	return false
}

func TestGoEdges(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()

	src := `package main

import (
	"fmt"
	"strings"
)

func Greet(name string) string {
	return fmt.Sprintf("hi %s", strings.ToUpper(name))
}

func main() {
	Greet("world")
}
`
	symbols, edges := parseAndExtract(t, walker, parser, LanguageGo, "main.go", src)

	if !hasImport(edges, "fmt") || !hasImport(edges, "strings") {
		t.Fatalf("expected fmt and strings imports, got %+v", edgesByType(edges, EdgeImports))
	}
	// Qualified calls resolve to the trailing selector.
	if !hasCall(edges, "Sprintf") || !hasCall(edges, "ToUpper") {
		t.Fatalf("expected Sprintf and ToUpper calls, got %+v", edgesByType(edges, EdgeCalls))
	}
	// Bare call to Greet from main.
	if !hasCall(edges, "Greet") {
		t.Fatalf("expected Greet call, got %+v", edgesByType(edges, EdgeCalls))
	}
	// The Greet call must be attributed to the enclosing main function.
	var mainID string
	for _, s := range symbols {
		if s.Name == "main" && s.SymbolType == SymbolTypeFunction {
			mainID = s.ID
		}
	}
	if mainID == "" {
		t.Fatal("main symbol not found")
	}
	var attributed bool
	for _, e := range edgesByType(edges, EdgeCalls) {
		if e.DstName == "Greet" && e.SrcSymbolID == mainID {
			attributed = true
		}
	}
	if !attributed {
		t.Fatal("Greet call not attributed to enclosing main function")
	}
}

func TestTypeScriptEdges(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()

	src := `import { foo } from "./foo";
import bar from "../lib/bar";

export function run(): void {
  foo();
  bar.doThing();
}
`
	_, edges := parseAndExtract(t, walker, parser, LanguageTypeScript, "src/run.ts", src)

	if !hasImport(edges, "./foo") || !hasImport(edges, "../lib/bar") {
		t.Fatalf("expected relative imports, got %+v", edgesByType(edges, EdgeImports))
	}
	if !hasCall(edges, "foo") || !hasCall(edges, "doThing") {
		t.Fatalf("expected foo and doThing calls, got %+v", edgesByType(edges, EdgeCalls))
	}
}

func TestJavaScriptRequireEdges(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	parser := NewParser()
	defer parser.Close()

	src := `const path = require("path");
const local = require("./local");

function go() {
  path.join("a", "b");
}
`
	_, edges := parseAndExtract(t, walker, parser, LanguageJavaScript, "index.js", src)

	if !hasImport(edges, "path") || !hasImport(edges, "./local") {
		t.Fatalf("expected require() import edges, got %+v", edgesByType(edges, EdgeImports))
	}
	if !hasCall(edges, "join") {
		t.Fatalf("expected join call, got %+v", edgesByType(edges, EdgeCalls))
	}
}

func TestEdgeCapableLanguages(t *testing.T) {
	walker := NewASTWalker(DefaultWalkerConfig())
	for _, lang := range []Language{LanguageGo, LanguageTypeScript, LanguageJavaScript} {
		if !walker.SupportsEdges(lang) {
			t.Errorf("expected %s to support edges", lang)
		}
	}
	// A symbol-only language (no EdgeExtractor) must report no edge support.
	if walker.SupportsEdges(LanguageTOML) {
		t.Error("TOML should not support edges")
	}
}
