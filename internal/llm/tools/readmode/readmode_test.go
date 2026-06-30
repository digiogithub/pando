package readmode

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

const goFixture = `package sample

import (
	"fmt"
	"strings"
)

// Greeter greets people.
type Greeter struct {
	Name string
}

// Hello returns a greeting.
func (g *Greeter) Hello(loud bool) string {
	if loud {
		return strings.ToUpper("hi " + g.Name)
	}
	return fmt.Sprintf("hi %s", g.Name)
}

func Add(a, b int) int {
	return a + b
}
`

func TestNormalize(t *testing.T) {
	cases := map[string]Mode{
		"":           ModeFull,
		"full":       ModeFull,
		"FULL":       ModeFull,
		"signatures": ModeSignatures,
		"sig":        ModeSignatures,
		"map":        ModeMap,
		"auto":       ModeAuto,
		"bogus":      ModeFull,
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestResolveAuto(t *testing.T) {
	// Instruction file → full regardless of size.
	if got := Resolve(ModeAuto, ResolveInput{Path: "CLAUDE.md", SizeBytes: 999999}); got != ModeFull {
		t.Errorf("instruction file: got %q want full", got)
	}
	// Non-code (markdown) → full.
	if got := Resolve(ModeAuto, ResolveInput{Path: "doc.md", SizeBytes: 50000}); got != ModeFull {
		t.Errorf("markdown: got %q want full", got)
	}
	// Diagnostics active → full.
	if got := Resolve(ModeAuto, ResolveInput{Path: "x.go", SizeBytes: 50000, DiagnosticsActive: true}); got != ModeFull {
		t.Errorf("diagnostics: got %q want full", got)
	}
	// Small code → full.
	if got := Resolve(ModeAuto, ResolveInput{Path: "x.go", SizeBytes: 100}); got != ModeFull {
		t.Errorf("small: got %q want full", got)
	}
	// Medium code → map.
	if got := Resolve(ModeAuto, ResolveInput{Path: "x.go", SizeBytes: 5000}); got != ModeMap {
		t.Errorf("medium: got %q want map", got)
	}
	// Large code → signatures.
	if got := Resolve(ModeAuto, ResolveInput{Path: "x.go", SizeBytes: 50000}); got != ModeSignatures {
		t.Errorf("large: got %q want signatures", got)
	}
	// Explicit mode passes through unchanged.
	if got := Resolve(ModeSignatures, ResolveInput{Path: "CLAUDE.md", SizeBytes: 10}); got != ModeSignatures {
		t.Errorf("explicit: got %q want signatures", got)
	}
}

func TestRenderSignaturesGo(t *testing.T) {
	symbols, lang, ok := ParseSymbols(context.Background(), "sample.go", []byte(goFixture))
	if !ok {
		t.Fatal("ParseSymbols failed for Go fixture")
	}
	if lang != treesitter.LanguageGo {
		t.Fatalf("lang=%q want go", lang)
	}
	out := RenderSignatures(symbols, lang, 0)
	if out == "" {
		t.Fatal("empty signatures render")
	}
	for _, want := range []string{"Greeter", "Hello", "Add"} {
		if !strings.Contains(out, want) {
			t.Errorf("signatures missing %q:\n%s", want, out)
		}
	}
	// Must carry line numbers (Add is defined late in the fixture).
	if !strings.Contains(out, "L") {
		t.Errorf("signatures missing line numbers:\n%s", out)
	}
	// Must be smaller than the raw source (the whole point).
	if len(out) >= len(goFixture) {
		t.Errorf("signatures render not smaller: %d >= %d", len(out), len(goFixture))
	}
}

func TestRenderSignaturesLineOffset(t *testing.T) {
	symbols, lang, ok := ParseSymbols(context.Background(), "sample.go", []byte(goFixture))
	if !ok {
		t.Fatal("ParseSymbols failed")
	}
	base := RenderSignatures(symbols, lang, 0)
	shifted := RenderSignatures(symbols, lang, 100)
	if base == shifted {
		t.Fatal("expected line offset to change rendered line numbers")
	}
}

func TestRenderMapGo(t *testing.T) {
	symbols, lang, ok := ParseSymbols(context.Background(), "sample.go", []byte(goFixture))
	if !ok {
		t.Fatal("ParseSymbols failed")
	}
	out := RenderMap(symbols, lang, goFixture, 0)
	if !strings.Contains(out, "imports:") {
		t.Errorf("map missing imports section:\n%s", out)
	}
	if !strings.Contains(out, "fmt") || !strings.Contains(out, "strings") {
		t.Errorf("map missing import targets:\n%s", out)
	}
	// Go methods are top-level declarations, so map includes them alongside types.
	if !strings.Contains(out, "Add") || !strings.Contains(out, "Greeter") {
		t.Errorf("map missing top-level symbols:\n%s", out)
	}
}

const tsFixture = `import { Foo } from "./foo";

export class Service {
  private name: string;

  constructor(name: string) {
    this.name = name;
  }

  greet(): string {
    return "hi " + this.name;
  }
}

export function helper(x: number): number {
  return x * 2;
}
`

func TestRenderMapTypeScriptOmitsNested(t *testing.T) {
	symbols, lang, ok := ParseSymbols(context.Background(), "service.ts", []byte(tsFixture))
	if !ok {
		t.Fatal("ParseSymbols failed for TS fixture")
	}
	mapOut := RenderMap(symbols, lang, tsFixture, 0)
	sigOut := RenderSignatures(symbols, lang, 0)

	// The class is a top-level symbol; its method should appear in signatures but
	// be omitted from the structural map (nested under the class).
	if !strings.Contains(mapOut, "Service") {
		t.Errorf("map missing class Service:\n%s", mapOut)
	}
	if strings.Contains(mapOut, "greet") {
		t.Errorf("map should omit nested method greet:\n%s", mapOut)
	}
	if !strings.Contains(sigOut, "greet") {
		t.Errorf("signatures should include nested method greet:\n%s", sigOut)
	}
	if !strings.Contains(mapOut, "./foo") {
		t.Errorf("map missing TS import:\n%s", mapOut)
	}
}

func TestParseSymbolsUnsupported(t *testing.T) {
	if _, _, ok := ParseSymbols(context.Background(), "data.unknownext", []byte("x")); ok {
		t.Error("expected unsupported extension to fail parse")
	}
}

func TestDiffWindows(t *testing.T) {
	oldC := "line1\nline2\nline3"
	newC := "line1\nCHANGED\nline3"
	d := DiffWindows(oldC, newC, 0)
	if !strings.Contains(d, "- 2: line2") {
		t.Errorf("diff missing removed line:\n%s", d)
	}
	if !strings.Contains(d, "+ 2: CHANGED") {
		t.Errorf("diff missing added line:\n%s", d)
	}
	// Identical content → no change.
	if got := DiffWindows(oldC, oldC, 0); !strings.Contains(got, "no textual change") {
		t.Errorf("expected no-change marker, got %q", got)
	}
}

func TestExtractImports(t *testing.T) {
	imps := extractImports(goFixture, treesitter.LanguageGo)
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %v", imps)
	}
}
